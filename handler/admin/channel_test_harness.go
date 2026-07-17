package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/store"
)

const (
	channelTestDefaultPrompt    = "ping"
	channelTestDefaultTimeoutMs = 15_000
	channelTestMinTimeoutMs     = 1_000
	channelTestMaxTimeoutMs     = 60_000
	channelTestMaxBodyBytes     = 2 << 10 // ~2 KiB safe summary
	channelTestMaxPromptRunes   = 256
	channelTestDefaultModel     = "gpt-4o-mini"
	channelTestModeChat         = "chat"
	channelTestModeModels       = "models"
)

// secretLikeRE redacts common secret-looking tokens from operator-visible bodies/errors.
var secretLikeRE = regexp.MustCompile(`(?i)(sk-[a-z0-9_\-]{8,}|Bearer\s+[A-Za-z0-9\-._~+/]+=*|api[_-]?key["']?\s*[:=]\s*["']?[A-Za-z0-9\-._]{8,})`)

// RegisterChannelTestRoutes registers admin-only forced-channel probe endpoints.
//
//	POST /api/admin/test-channel
//	POST /api/debug/channel-probe   (alias)
func RegisterChannelTestRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	h := &channelTestHandler{db: db, cfg: cfg}
	r.Post("/api/admin/test-channel", h.testChannel)
	r.Post("/api/debug/channel-probe", h.testChannel)
}

type channelTestHandler struct {
	db  *sqlx.DB
	cfg *config.Config

	// transport is an optional override for tests (stub upstream).
	// When nil, production uses platform.DoWithProxy / default client.
	transport http.RoundTripper
}

type channelTestRequest struct {
	ChannelID *int64 `json:"channelId"`
	SiteID    *int64 `json:"siteId"`
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	Mode      string `json:"mode"`
	TimeoutMs *int64 `json:"timeoutMs"`
}

type channelTestTarget struct {
	ChannelID   int64
	RouteID     int64
	AccountID   int64
	SiteID      int64
	SourceModel string
	SiteName    string
	SiteURL     string
	Platform    string
	TokenValue  string
	Account     store.Account
	Site        store.Site
}

type harnessAccountRow struct {
	ID            int64   `db:"id"`
	SiteID        int64   `db:"site_id"`
	AccessToken   string  `db:"access_token"`
	APIToken      *string `db:"api_token"`
	OAuthProvider *string `db:"oauth_provider"`
	ExtraConfig   *string `db:"extra_config"`
	Status        string  `db:"status"`
}

type harnessSiteRow struct {
	ID             int64   `db:"id"`
	Name           string  `db:"name"`
	URL            string  `db:"url"`
	Platform       string  `db:"platform"`
	ProxyURL       *string `db:"proxy_url"`
	UseSystemProxy bool    `db:"use_system_proxy"`
	CustomHeaders  *string `db:"custom_headers"`
	Status         string  `db:"status"`
}

type harnessChannelRow struct {
	ID          int64   `db:"id"`
	RouteID     int64   `db:"route_id"`
	AccountID   int64   `db:"account_id"`
	TokenID     *int64  `db:"token_id"`
	SourceModel *string `db:"source_model"`
	Enabled     bool    `db:"enabled"`
}

type harnessTokenRow struct {
	ID      int64  `db:"id"`
	Token   string `db:"token"`
	Enabled bool   `db:"enabled"`
}

// POST /api/admin/test-channel
// POST /api/debug/channel-probe
func (h *channelTestHandler) testChannel(w http.ResponseWriter, r *http.Request) {
	var body channelTestRequest
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	h.runChannelTest(w, r, body)
}

// runChannelTest is the shared forced-channel probe implementation used by
// /api/admin/test-channel and the /api/test/{proxy,chat} aliases (#185).
func (h *channelTestHandler) runChannelTest(w http.ResponseWriter, r *http.Request, body channelTestRequest) {
	mode := strings.ToLower(strings.TrimSpace(body.Mode))
	if mode == "" {
		mode = channelTestModeChat
	}
	if mode != channelTestModeChat && mode != channelTestModeModels {
		writeError(w, http.StatusBadRequest, "Invalid mode. Expected chat or models.")
		return
	}

	if (body.ChannelID == nil || *body.ChannelID <= 0) && (body.SiteID == nil || *body.SiteID <= 0) {
		writeError(w, http.StatusBadRequest, "channelId or siteId is required")
		return
	}

	model := strings.TrimSpace(body.Model)
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		prompt = channelTestDefaultPrompt
	}
	if utf8.RuneCountInString(prompt) > channelTestMaxPromptRunes {
		// Bound operator input; never store prompt corpora.
		runes := []rune(prompt)
		prompt = string(runes[:channelTestMaxPromptRunes])
	}

	timeoutMs := int64(channelTestDefaultTimeoutMs)
	if body.TimeoutMs != nil && *body.TimeoutMs > 0 {
		timeoutMs = *body.TimeoutMs
	}
	if timeoutMs < channelTestMinTimeoutMs {
		timeoutMs = channelTestMinTimeoutMs
	}
	if timeoutMs > channelTestMaxTimeoutMs {
		timeoutMs = channelTestMaxTimeoutMs
	}

	target, err := h.resolveTarget(r.Context(), body.ChannelID, body.SiteID, model)
	if err != nil {
		if isHarnessNotFound(err) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if model == "" {
		if target.SourceModel != "" {
			model = target.SourceModel
		} else {
			model = channelTestDefaultModel
		}
	}

	if target.TokenValue == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":       false,
			"statusCode":    0,
			"latencyMs":     0,
			"truncatedBody": "",
			"error":         "No usable token on channel/account (api token / oauth access token required)",
			"channelId":     target.ChannelID,
			"siteId":        target.SiteID,
			"accountId":     target.AccountID,
			"model":         model,
			"mode":          mode,
			"bodyTruncated": false,
		})
		return
	}

	result := h.executeProbe(r.Context(), target, mode, model, prompt, timeoutMs)
	writeJSON(w, http.StatusOK, result)
}

func isHarnessNotFound(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

func (h *channelTestHandler) resolveTarget(ctx context.Context, channelID, siteID *int64, modelHint string) (*channelTestTarget, error) {
	if channelID != nil && *channelID > 0 {
		return h.loadByChannelID(ctx, *channelID)
	}
	return h.loadBySiteID(ctx, *siteID, modelHint)
}

func (h *channelTestHandler) loadByChannelID(ctx context.Context, channelID int64) (*channelTestTarget, error) {
	var ch harnessChannelRow
	err := h.db.GetContext(ctx, &ch, h.db.Rebind(`
		SELECT id, route_id, account_id, token_id, source_model, enabled
		FROM route_channels WHERE id = ?`), channelID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("channel not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load channel: %w", err)
	}
	return h.assembleTarget(ctx, ch)
}

func (h *channelTestHandler) loadBySiteID(ctx context.Context, siteID int64, modelHint string) (*channelTestTarget, error) {
	// Prefer enabled channel whose source_model matches modelHint, else any enabled for the site.
	var ch harnessChannelRow
	modelHint = strings.TrimSpace(modelHint)
	var err error
	if modelHint != "" {
		err = h.db.GetContext(ctx, &ch, h.db.Rebind(`
			SELECT rc.id, rc.route_id, rc.account_id, rc.token_id, rc.source_model, rc.enabled
			FROM route_channels rc
			JOIN accounts a ON a.id = rc.account_id
			WHERE a.site_id = ? AND rc.enabled = ?
			  AND LOWER(COALESCE(rc.source_model, '')) = LOWER(?)
			ORDER BY rc.priority DESC, rc.id ASC
			LIMIT 1`), siteID, true, modelHint)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("load channel by site/model: %w", err)
		}
	}
	if ch.ID == 0 {
		err = h.db.GetContext(ctx, &ch, h.db.Rebind(`
			SELECT rc.id, rc.route_id, rc.account_id, rc.token_id, rc.source_model, rc.enabled
			FROM route_channels rc
			JOIN accounts a ON a.id = rc.account_id
			WHERE a.site_id = ? AND rc.enabled = ?
			ORDER BY rc.priority DESC, rc.id ASC
			LIMIT 1`), siteID, true)
		if err == sql.ErrNoRows {
			// Fall back to any channel on site even if disabled.
			err = h.db.GetContext(ctx, &ch, h.db.Rebind(`
				SELECT rc.id, rc.route_id, rc.account_id, rc.token_id, rc.source_model, rc.enabled
				FROM route_channels rc
				JOIN accounts a ON a.id = rc.account_id
				WHERE a.site_id = ?
				ORDER BY rc.priority DESC, rc.id ASC
				LIMIT 1`), siteID)
		}
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no channel found for site")
		}
		if err != nil {
			return nil, fmt.Errorf("load channel by site: %w", err)
		}
	}
	return h.assembleTarget(ctx, ch)
}

func (h *channelTestHandler) assembleTarget(ctx context.Context, ch harnessChannelRow) (*channelTestTarget, error) {
	var acc harnessAccountRow
	if err := h.db.GetContext(ctx, &acc, h.db.Rebind(`
		SELECT id, site_id, access_token, api_token, oauth_provider, extra_config, status
		FROM accounts WHERE id = ?`), ch.AccountID); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("account not found")
		}
		return nil, fmt.Errorf("load account: %w", err)
	}

	var site harnessSiteRow
	if err := h.db.GetContext(ctx, &site, h.db.Rebind(`
		SELECT id, name, url, platform, proxy_url, use_system_proxy, custom_headers, status
		FROM sites WHERE id = ?`), acc.SiteID); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("site not found")
		}
		return nil, fmt.Errorf("load site: %w", err)
	}

	var token *harnessTokenRow
	if ch.TokenID != nil && *ch.TokenID > 0 {
		var tr harnessTokenRow
		if err := h.db.GetContext(ctx, &tr, h.db.Rebind(`
			SELECT id, token, enabled FROM account_tokens WHERE id = ?`), *ch.TokenID); err == nil {
			token = &tr
		}
	}

	sourceModel := ""
	if ch.SourceModel != nil {
		sourceModel = strings.TrimSpace(*ch.SourceModel)
	}

	account := store.Account{
		ID:            acc.ID,
		SiteID:        acc.SiteID,
		AccessToken:   acc.AccessToken,
		APIToken:      acc.APIToken,
		OAuthProvider: acc.OAuthProvider,
		ExtraConfig:   acc.ExtraConfig,
		Status:        acc.Status,
	}
	siteModel := store.Site{
		ID:             site.ID,
		Name:           site.Name,
		URL:            site.URL,
		Platform:       site.Platform,
		ProxyURL:       site.ProxyURL,
		UseSystemProxy: site.UseSystemProxy,
		CustomHeaders:  site.CustomHeaders,
		Status:         site.Status,
	}

	return &channelTestTarget{
		ChannelID:   ch.ID,
		RouteID:     ch.RouteID,
		AccountID:   acc.ID,
		SiteID:      site.ID,
		SourceModel: sourceModel,
		SiteName:    site.Name,
		SiteURL:     site.URL,
		Platform:    site.Platform,
		TokenValue:  resolveHarnessToken(ch.TokenID, token, acc),
		Account:     account,
		Site:        siteModel,
	}, nil
}

// resolveHarnessToken mirrors routing.ChannelSelector.resolveChannelTokenValue
// without importing selector internals. Harness additionally falls back to
// access_token for session-style accounts so operators can still probe.
func resolveHarnessToken(tokenID *int64, token *harnessTokenRow, acc harnessAccountRow) string {
	if tokenID != nil && *tokenID > 0 {
		if token == nil || token.Token == "" || !token.Enabled {
			return ""
		}
		return token.Token
	}
	if acc.OAuthProvider != nil && strings.TrimSpace(*acc.OAuthProvider) != "" {
		return strings.TrimSpace(acc.AccessToken)
	}
	if acc.APIToken != nil && strings.TrimSpace(*acc.APIToken) != "" {
		return strings.TrimSpace(*acc.APIToken)
	}
	return strings.TrimSpace(acc.AccessToken)
}

func (h *channelTestHandler) executeProbe(
	ctx context.Context,
	target *channelTestTarget,
	mode, model, prompt string,
	timeoutMs int64,
) map[string]any {
	upstreamPath := "/v1/models"
	method := http.MethodGet
	var bodyBytes []byte
	if mode == channelTestModeChat {
		upstreamPath = "/v1/chat/completions"
		method = http.MethodPost
		payload := map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
			"max_tokens": 8,
			"stream":     false,
		}
		bodyBytes, _ = json.Marshal(payload)
	}

	upstreamURL := proxy.BuildUpstreamURL(target.SiteURL, upstreamPath)
	host := safeHost(upstreamURL)

	timeout := time.Duration(timeoutMs) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, upstreamURL, bodyReader)
	if err != nil {
		return harnessFail(target, mode, model, upstreamPath, host, 0, 0, "", "build request: "+safeError(err))
	}
	if len(bodyBytes) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+target.TokenValue)
	req.Header.Set("User-Agent", "metapi-go-admin-channel-test/1.0")

	proxyCfg := service.BuildPlatformProxyConfig(h.cfg, &target.Account, &target.Site)
	if proxyCfg != nil {
		for k, v := range proxyCfg.CustomHeaders {
			if proxy.IsMetapiControlHeader(k) {
				continue
			}
			// Never let site headers clobber the harness Authorization.
			if strings.EqualFold(k, "Authorization") {
				continue
			}
			req.Header.Set(k, v)
		}
	}

	started := time.Now()
	resp, err := h.doRequest(reqCtx, req, proxyCfg)
	latencyMs := time.Since(started).Milliseconds()
	if err != nil {
		return harnessFail(target, mode, model, upstreamPath, host, 0, latencyMs, "", safeError(err))
	}
	defer resp.Body.Close()

	// Bound body read tightly for operator summary.
	limited := io.LimitReader(resp.Body, channelTestMaxBodyBytes+1)
	raw, readErr := io.ReadAll(limited)
	if readErr != nil {
		return harnessFail(target, mode, model, upstreamPath, host, resp.StatusCode, latencyMs, "", "read body: "+safeError(readErr))
	}
	truncated, wasTruncated := truncateAndRedact(raw, channelTestMaxBodyBytes)

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	out := map[string]any{
		"success":       success,
		"statusCode":    resp.StatusCode,
		"latencyMs":     latencyMs,
		"truncatedBody": truncated,
		"error":         nil,
		"channelId":     target.ChannelID,
		"routeId":       target.RouteID,
		"siteId":        target.SiteID,
		"siteName":      target.SiteName,
		"accountId":     target.AccountID,
		"model":         model,
		"mode":          mode,
		"upstreamPath":  upstreamPath,
		"upstreamHost":  host,
		"bodyTruncated": wasTruncated,
	}
	if !success {
		out["error"] = fmt.Sprintf("upstream status %d", resp.StatusCode)
	}
	return out
}

func harnessFail(target *channelTestTarget, mode, model, path, host string, status int, latencyMs int64, body, errMsg string) map[string]any {
	return map[string]any{
		"success":       false,
		"statusCode":    status,
		"latencyMs":     latencyMs,
		"truncatedBody": body,
		"error":         errMsg,
		"channelId":     target.ChannelID,
		"routeId":       target.RouteID,
		"siteId":        target.SiteID,
		"siteName":      target.SiteName,
		"accountId":     target.AccountID,
		"model":         model,
		"mode":          mode,
		"upstreamPath":  path,
		"upstreamHost":  host,
		"bodyTruncated": false,
	}
}

func (h *channelTestHandler) doRequest(ctx context.Context, req *http.Request, proxyCfg *platform.ProxyConfig) (*http.Response, error) {
	if h.transport != nil {
		return h.transport.RoundTrip(req.WithContext(ctx))
	}
	if proxyCfg != nil && (proxyCfg.ProxyURL != "" || proxyCfg.InsecureSkipTLS) {
		return platform.DoWithProxy(ctx, req, proxyCfg)
	}
	client := &http.Client{Timeout: 0} // context owns deadline
	return client.Do(req.WithContext(ctx))
}

func safeHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

func truncateAndRedact(raw []byte, maxBytes int) (string, bool) {
	truncated := false
	if len(raw) > maxBytes {
		raw = raw[:maxBytes]
		truncated = true
	}
	// Ensure valid UTF-8 after hard cut.
	for !utf8.Valid(raw) && len(raw) > 0 {
		raw = raw[:len(raw)-1]
		truncated = true
	}
	s := string(raw)
	s = secretLikeRE.ReplaceAllString(s, "[redacted]")
	s = strings.ReplaceAll(s, "\r", "")
	if truncated {
		s += "…[truncated]"
	}
	return s, truncated
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = secretLikeRE.ReplaceAllString(msg, "[redacted]")
	if strings.Contains(msg, "context deadline exceeded") {
		return "timeout waiting for upstream"
	}
	if strings.Contains(msg, "context canceled") {
		return "request canceled"
	}
	if utf8.RuneCountInString(msg) > 400 {
		runes := []rune(msg)
		msg = string(runes[:400]) + "…"
	}
	return msg
}
