package proxyhandler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/scheduler"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/store"
)

// ChannelHealthProbeExecutor is a thin boot-wired probe that hits live upstream
// for ModelProbeScheduler / admin probe-now paths (#170).
//
// It prefers a lightweight GET /v1/models check. Platforms without a models
// catalog endpoint may return non-2xx; those outcomes are recorded as failure
// (not credential expiry). See docs/analysis/probe-boot-wiring.md residual.
type ChannelHealthProbeExecutor struct {
	cfg *config.Config

	// transport is an optional override for tests (fake upstream).
	// When nil, production uses platform.DoWithProxy / default client.
	transport http.RoundTripper

	// db is an optional override for tests. When nil, production uses store.GetDB().
	db *store.DB
}

// NewChannelHealthProbeExecutor builds the production probe executor.
func NewChannelHealthProbeExecutor(cfg *config.Config) *ChannelHealthProbeExecutor {
	return &ChannelHealthProbeExecutor{cfg: cfg}
}

// SetTransport injects a RoundTripper (tests).
func (p *ChannelHealthProbeExecutor) SetTransport(rt http.RoundTripper) {
	if p == nil {
		return
	}
	p.transport = rt
}

// SetDB injects a database handle (tests). Production leaves this nil.
func (p *ChannelHealthProbeExecutor) SetDB(db *store.DB) {
	if p == nil {
		return
	}
	p.db = db
}

// ProbeChannel implements scheduler.ChannelHealthProbe.
func (p *ChannelHealthProbeExecutor) ProbeChannel(ctx context.Context, target scheduler.ProbeTarget) (scheduler.ProbeOutcome, error) {
	if p == nil {
		return scheduler.ProbeOutcome{Status: "inconclusive", ErrorText: "probe executor nil"}, nil
	}
	if target.ChannelID <= 0 {
		return scheduler.ProbeOutcome{Status: "skipped", ErrorText: "invalid channel id"}, nil
	}

	dbw := p.db
	if dbw == nil {
		dbw = store.GetDB()
	}
	if dbw == nil {
		return scheduler.ProbeOutcome{}, fmt.Errorf("database not initialized")
	}

	loaded, err := loadProbeChannel(ctx, dbw, target.ChannelID)
	if err != nil {
		// Missing channel is inconclusive (no health mutation).
		return scheduler.ProbeOutcome{Status: "inconclusive", ErrorText: err.Error()}, nil
	}
	if loaded.TokenValue == "" {
		return scheduler.ProbeOutcome{
			Status:    "inconclusive",
			ErrorText: "no usable token on channel/account",
		}, nil
	}

	model := strings.TrimSpace(target.ModelName)
	if model == "" {
		model = loaded.SourceModel
	}

	// Prefer lightweight models listing. Fall back to a tiny chat completion
	// when the operator/target model is known and models endpoint is absent
	// is documented as residual — we still try models first for cost.
	return p.executeModelsProbe(ctx, loaded, model)
}

type probeChannelTarget struct {
	ChannelID   int64
	AccountID   int64
	SiteID      int64
	SourceModel string
	SiteURL     string
	TokenValue  string
	Account     store.Account
	Site        store.Site
}

func loadProbeChannel(ctx context.Context, dbw *store.DB, channelID int64) (*probeChannelTarget, error) {
	var ch struct {
		ID          int64   `db:"id"`
		AccountID   int64   `db:"account_id"`
		TokenID     *int64  `db:"token_id"`
		SourceModel *string `db:"source_model"`
		Enabled     bool    `db:"enabled"`
	}
	err := dbw.GetContext(ctx, &ch, dbw.Rebind(`
		SELECT id, account_id, token_id, source_model, enabled
		FROM route_channels WHERE id = ?`), channelID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("channel not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load channel: %w", err)
	}

	var acc store.Account
	if err := dbw.GetContext(ctx, &acc, dbw.Rebind(`
		SELECT id, site_id, username, access_token, api_token, balance, balance_used,
		       quota, unit_cost, value_score, status, is_pinned, sort_order,
		       checkin_enabled, last_checkin_at, last_balance_refresh, oauth_provider,
		       oauth_account_key, oauth_project_id, extra_config, created_at, updated_at
		FROM accounts WHERE id = ?`), ch.AccountID); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("account not found")
		}
		return nil, fmt.Errorf("load account: %w", err)
	}

	var site store.Site
	if err := dbw.GetContext(ctx, &site, dbw.Rebind(`
		SELECT id, name, url, external_checkin_url, platform, proxy_url, use_system_proxy,
		       custom_headers, status, is_pinned, sort_order, global_weight, api_key,
		       max_concurrency,
		       post_refresh_probe_enabled, post_refresh_probe_model, post_refresh_probe_scope,
		       post_refresh_probe_latency_threshold_ms, created_at, updated_at
		FROM sites WHERE id = ?`), acc.SiteID); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("site not found")
		}
		return nil, fmt.Errorf("load site: %w", err)
	}

	var token *store.AccountToken
	if ch.TokenID != nil && *ch.TokenID > 0 {
		var tr store.AccountToken
		if err := dbw.GetContext(ctx, &tr, dbw.Rebind(`
			SELECT id, account_id, name, token, token_group, value_status, source,
			       enabled, is_default, created_at, updated_at
			FROM account_tokens WHERE id = ?`), *ch.TokenID); err == nil {
			token = &tr
		}
	}

	sourceModel := ""
	if ch.SourceModel != nil {
		sourceModel = strings.TrimSpace(*ch.SourceModel)
	}

	return &probeChannelTarget{
		ChannelID:   ch.ID,
		AccountID:   acc.ID,
		SiteID:      site.ID,
		SourceModel: sourceModel,
		SiteURL:     site.URL,
		TokenValue:  resolveProbeToken(ch.TokenID, token, acc),
		Account:     acc,
		Site:        site,
	}, nil
}

// resolveProbeToken mirrors harness/routing token resolution without importing
// selector internals. Falls back to access_token for session-style accounts.
func resolveProbeToken(tokenID *int64, token *store.AccountToken, acc store.Account) string {
	if tokenID != nil && *tokenID > 0 {
		if token == nil || strings.TrimSpace(token.Token) == "" || !token.Enabled {
			return ""
		}
		return strings.TrimSpace(token.Token)
	}
	if acc.OAuthProvider != nil && strings.TrimSpace(*acc.OAuthProvider) != "" {
		return strings.TrimSpace(acc.AccessToken)
	}
	if acc.APIToken != nil && strings.TrimSpace(*acc.APIToken) != "" {
		return strings.TrimSpace(*acc.APIToken)
	}
	return strings.TrimSpace(acc.AccessToken)
}

func (p *ChannelHealthProbeExecutor) executeModelsProbe(ctx context.Context, target *probeChannelTarget, model string) (scheduler.ProbeOutcome, error) {
	upstreamPath := "/v1/models"
	upstreamURL := proxy.BuildUpstreamURL(target.SiteURL, upstreamPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		return scheduler.ProbeOutcome{Status: "inconclusive", ErrorText: "build request: " + err.Error()}, nil
	}
	req.Header.Set("Authorization", "Bearer "+target.TokenValue)
	req.Header.Set("User-Agent", "metapi-go-model-probe/1.0")

	proxyCfg := service.BuildPlatformProxyConfig(p.cfg, &target.Account, &target.Site)
	if proxyCfg != nil {
		for k, v := range proxyCfg.CustomHeaders {
			if proxy.IsMetapiControlHeader(k) {
				continue
			}
			if strings.EqualFold(k, "Authorization") {
				continue
			}
			req.Header.Set(k, v)
		}
	}

	started := time.Now()
	resp, err := p.doRequest(ctx, req, proxyCfg)
	latencyMs := float64(time.Since(started).Milliseconds())
	if err != nil {
		// Transport / timeout: treat as failure so recovery can cool flaky channels.
		msg := err.Error()
		if strings.Contains(msg, "context deadline exceeded") {
			msg = "timeout waiting for upstream"
		}
		return scheduler.ProbeOutcome{
			Status:     "failure",
			LatencyMs:  latencyMs,
			ErrorText:  msg,
			HTTPStatus: 0,
		}, nil
	}
	defer resp.Body.Close()
	// Drain a small amount so keep-alive connections can be reused; discard body.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return scheduler.ProbeOutcome{
			Status:     "success",
			LatencyMs:  latencyMs,
			HTTPStatus: resp.StatusCode,
		}, nil
	}

	// Non-2xx models listing: some gateways lack /v1/models. Retry once with a
	// tiny chat completion when we have a model name (better signal, still cheap).
	if model != "" && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed) {
		return p.executeChatProbe(ctx, target, model)
	}

	return scheduler.ProbeOutcome{
		Status:     "failure",
		LatencyMs:  latencyMs,
		HTTPStatus: resp.StatusCode,
		ErrorText:  fmt.Sprintf("upstream status %d", resp.StatusCode),
	}, nil
}

func (p *ChannelHealthProbeExecutor) executeChatProbe(ctx context.Context, target *probeChannelTarget, model string) (scheduler.ProbeOutcome, error) {
	upstreamPath := "/v1/chat/completions"
	upstreamURL := proxy.BuildUpstreamURL(target.SiteURL, upstreamPath)
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
		"stream":     false,
	}
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return scheduler.ProbeOutcome{Status: "inconclusive", ErrorText: "build chat request: " + err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+target.TokenValue)
	req.Header.Set("User-Agent", "metapi-go-model-probe/1.0")

	proxyCfg := service.BuildPlatformProxyConfig(p.cfg, &target.Account, &target.Site)
	if proxyCfg != nil {
		for k, v := range proxyCfg.CustomHeaders {
			if proxy.IsMetapiControlHeader(k) {
				continue
			}
			if strings.EqualFold(k, "Authorization") {
				continue
			}
			req.Header.Set(k, v)
		}
	}

	started := time.Now()
	resp, err := p.doRequest(ctx, req, proxyCfg)
	latencyMs := float64(time.Since(started).Milliseconds())
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "context deadline exceeded") {
			msg = "timeout waiting for upstream"
		}
		return scheduler.ProbeOutcome{
			Status:     "failure",
			LatencyMs:  latencyMs,
			ErrorText:  msg,
			HTTPStatus: 0,
		}, nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return scheduler.ProbeOutcome{
			Status:     "success",
			LatencyMs:  latencyMs,
			HTTPStatus: resp.StatusCode,
		}, nil
	}
	return scheduler.ProbeOutcome{
		Status:     "failure",
		LatencyMs:  latencyMs,
		HTTPStatus: resp.StatusCode,
		ErrorText:  fmt.Sprintf("upstream status %d", resp.StatusCode),
	}, nil
}

func (p *ChannelHealthProbeExecutor) doRequest(ctx context.Context, req *http.Request, proxyCfg *platform.ProxyConfig) (*http.Response, error) {
	if p.transport != nil {
		return p.transport.RoundTrip(req.WithContext(ctx))
	}
	if proxyCfg != nil && (proxyCfg.ProxyURL != "" || proxyCfg.InsecureSkipTLS) {
		return platform.DoWithProxy(ctx, req, proxyCfg)
	}
	// Context owns the deadline; refuse cross-origin redirects so a public
	// site URL cannot 302 into metadata/loopback (shared platform policy).
	client := &http.Client{
		Timeout:       0,
		CheckRedirect: platform.RejectCrossOriginRedirect,
	}
	return client.Do(req.WithContext(ctx))
}
