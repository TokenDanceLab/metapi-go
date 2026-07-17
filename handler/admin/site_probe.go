package admin

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// siteProbeOptions controls forced site model probing.
type siteProbeOptions struct {
	Scope              string
	ModelName          string
	LatencyThresholdMs int
}

// siteProbeModelResult is one model outcome from a forced site probe pass.
type siteProbeModelResult struct {
	ModelName       string `json:"modelName"`
	Available       bool   `json:"available"`
	Status          string `json:"status"` // supported | unsupported | skipped
	LatencyMs       *int64 `json:"latencyMs,omitempty"`
	Reason          string `json:"reason,omitempty"`
	LatencyExceeded bool   `json:"latencyExceeded,omitempty"`
	AccountID       int64  `json:"accountId,omitempty"`
	ChannelID       int64  `json:"channelId,omitempty"`
	Error           string `json:"error,omitempty"`
	DisabledByProbe bool   `json:"disabledByProbe,omitempty"`
}

// siteProbeSummary aggregates a forced site probe pass.
type siteProbeSummary struct {
	Success     bool                   `json:"success"`
	TotalModels int                    `json:"totalModels"`
	Available   int                    `json:"available"`
	Unavailable int                    `json:"unavailable"`
	Probed      int                    `json:"probed"`
	Unsupported int                    `json:"unsupported"`
	Skipped     int                    `json:"skipped"`
	Results     []siteProbeModelResult `json:"results"`
	EmptyReason string                 `json:"emptyReason,omitempty"`
	Scope       string                 `json:"scope,omitempty"`
	StartedAt   string                 `json:"startedAt,omitempty"`
	CompletedAt string                 `json:"completedAt,omitempty"`
}

// siteProbeProgressEvent is emitted during SSE probe streaming.
type siteProbeProgressEvent struct {
	Type string
	Data map[string]any
}

type siteProbeTarget struct {
	ModelName string
	ChannelID int64
	AccountID int64
	SiteID    int64
	SiteURL   string
	SiteName  string
	Platform  string
	Token     string
	Account   store.Account
	Site      store.Site
}

const (
	siteProbeDefaultTimeoutMs = 15_000
	siteProbeMinTimeoutMs     = 1_000
	siteProbeMaxTimeoutMs     = 60_000
)

// runSiteProbe loads models for a site, forces lightweight chat probes through the
// #119 harness transport path, updates model_availability, and optionally
// auto-disables unsupported models for the site.
func (h *sitesHandler) runSiteProbe(
	ctx context.Context,
	siteID int64,
	opts siteProbeOptions,
	onProgress func(siteProbeProgressEvent),
) (*siteProbeSummary, error) {
	if siteID <= 0 {
		return nil, fmt.Errorf("Invalid site id")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var site store.Site
	if err := h.db.GetContext(ctx, &site, h.db.Rebind("SELECT * FROM sites WHERE id = ?"), siteID); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("site not found")
		}
		return nil, fmt.Errorf("load site: %w", err)
	}

	scope := strings.ToLower(strings.TrimSpace(opts.Scope))
	if scope != "single" {
		// Treat empty / "all" / unknown as all for operator convenience.
		if scope == "" || scope == "all" {
			scope = "all"
		} else {
			scope = "all"
		}
	}
	modelName := strings.TrimSpace(opts.ModelName)
	if scope == "single" && modelName == "" {
		// Fall back to site post-refresh probe model when present.
		modelName = strings.TrimSpace(site.PostRefreshProbeModel)
	}

	latencyThreshold := opts.LatencyThresholdMs
	if latencyThreshold <= 0 && site.PostRefreshProbeLatencyThresholdMs > 0 {
		latencyThreshold = int(site.PostRefreshProbeLatencyThresholdMs)
	}

	targets, emptyReason, err := h.loadSiteProbeTargets(ctx, site, scope, modelName)
	if err != nil {
		return nil, err
	}

	startedAt := time.Now().UTC().Format(time.RFC3339)
	summary := &siteProbeSummary{
		Success:     true,
		TotalModels: len(targets),
		Results:     make([]siteProbeModelResult, 0, len(targets)),
		Scope:       scope,
		StartedAt:   startedAt,
		EmptyReason: emptyReason,
	}

	if onProgress != nil {
		onProgress(siteProbeProgressEvent{
			Type: "start",
			Data: map[string]any{
				"scope":       scope,
				"modelsCount": len(targets),
				"startedAt":   startedAt,
				"siteId":      siteID,
			},
		})
	}

	if len(targets) == 0 {
		summary.CompletedAt = time.Now().UTC().Format(time.RFC3339)
		if summary.EmptyReason == "" {
			summary.EmptyReason = "no probe targets (accounts/models)"
		}
		return summary, nil
	}

	probe := h.newSiteProbeHarness()
	disabledModels := map[string]bool{}

	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return summary, err
		}

		result := h.probeOneSiteModel(ctx, probe, target, latencyThreshold)
		summary.Results = append(summary.Results, result)
		summary.Probed++

		switch result.Status {
		case "supported":
			summary.Available++
		case "unsupported":
			summary.Unavailable++
			summary.Unsupported++
		default:
			summary.Skipped++
		}

		if onProgress != nil {
			onProgress(siteProbeProgressEvent{
				Type: "model",
				Data: map[string]any{
					"modelName":       result.ModelName,
					"status":          result.Status,
					"latencyMs":       result.LatencyMs,
					"reason":          result.Reason,
					"latencyExceeded": result.LatencyExceeded,
					"available":       result.Available,
					"accountId":       result.AccountID,
					"channelId":       result.ChannelID,
				},
			})
		}

		// Persist account-level availability when we have an account target.
		if target.AccountID > 0 && result.Status != "skipped" {
			if err := upsertModelAvailability(h.db, target.AccountID, result.ModelName, result.Available, result.LatencyMs); err != nil {
				slog.Warn("site probe: model_availability upsert failed",
					"site_id", siteID,
					"account_id", target.AccountID,
					"model", result.ModelName,
					"error", err,
				)
			}
		}

		// Auto-disable unsupported models for the site (matches Sites.tsx complete copy).
		if result.Status == "unsupported" && !disabledModels[result.ModelName] {
			if err := ensureSiteDisabledModel(h.db, siteID, result.ModelName); err != nil {
				slog.Warn("site probe: disable model failed",
					"site_id", siteID,
					"model", result.ModelName,
					"error", err,
				)
			} else {
				disabledModels[result.ModelName] = true
				result.DisabledByProbe = true
				summary.Results[len(summary.Results)-1] = result
				if onProgress != nil {
					onProgress(siteProbeProgressEvent{
						Type: "action",
						Data: map[string]any{
							"action":    "disabled",
							"modelName": result.ModelName,
						},
					})
				}
			}
		}

		// Best-effort runtime health stamp when we have a real channel id.
		if target.ChannelID > 0 {
			applySiteProbeHealth(ctx, target, result)
		}
	}

	summary.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	return summary, nil
}

func (h *sitesHandler) newSiteProbeHarness() *channelTestHandler {
	cfg := h.cfg
	if cfg == nil {
		// config.Get panics if unset; protect unit tests that never load config.
		func() {
			defer func() { _ = recover() }()
			cfg = config.Get()
		}()
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &channelTestHandler{
		db:        h.db,
		cfg:       cfg,
		transport: h.transport,
	}
}

func (h *sitesHandler) loadSiteProbeTargets(
	ctx context.Context,
	site store.Site,
	scope, modelName string,
) ([]siteProbeTarget, string, error) {
	// Prefer enabled route_channels with non-empty source_model for this site.
	type channelRow struct {
		ID          int64   `db:"id"`
		AccountID   int64   `db:"account_id"`
		SourceModel *string `db:"source_model"`
		Enabled     bool    `db:"enabled"`
		TokenID     *int64  `db:"token_id"`
	}
	var channels []channelRow
	err := h.db.SelectContext(ctx, &channels, h.db.Rebind(`
		SELECT rc.id, rc.account_id, rc.source_model, rc.enabled, rc.token_id
		FROM route_channels rc
		INNER JOIN accounts a ON a.id = rc.account_id
		WHERE a.site_id = ?
		ORDER BY rc.enabled DESC, rc.priority DESC, rc.id ASC
	`), site.ID)
	if err != nil {
		return nil, "", fmt.Errorf("load site channels: %w", err)
	}

	// Also collect model names from availability tables (even if no channel yet).
	modelSet := map[string]struct{}{}
	if scope == "single" && modelName != "" {
		modelSet[modelName] = struct{}{}
	} else {
		var availModels []string
		_ = h.db.SelectContext(ctx, &availModels, h.db.Rebind(`
			SELECT DISTINCT ma.model_name
			FROM model_availability ma
			INNER JOIN accounts a ON a.id = ma.account_id
			WHERE a.site_id = ? AND COALESCE(ma.model_name, '') <> ''
		`), site.ID)
		for _, m := range availModels {
			m = strings.TrimSpace(m)
			if m != "" {
				modelSet[m] = struct{}{}
			}
		}
		var tokenModels []string
		_ = h.db.SelectContext(ctx, &tokenModels, h.db.Rebind(`
			SELECT DISTINCT tma.model_name
			FROM token_model_availability tma
			INNER JOIN account_tokens at ON at.id = tma.token_id
			INNER JOIN accounts a ON a.id = at.account_id
			WHERE a.site_id = ? AND COALESCE(tma.model_name, '') <> ''
		`), site.ID)
		for _, m := range tokenModels {
			m = strings.TrimSpace(m)
			if m != "" {
				modelSet[m] = struct{}{}
			}
		}
		for _, ch := range channels {
			if ch.SourceModel == nil {
				continue
			}
			m := strings.TrimSpace(*ch.SourceModel)
			if m != "" {
				modelSet[m] = struct{}{}
			}
		}
	}

	if len(modelSet) == 0 {
		return nil, "no models found for site (availability/channels empty)", nil
	}

	// Load active accounts for the site once.
	type accountRow struct {
		ID            int64   `db:"id"`
		SiteID        int64   `db:"site_id"`
		AccessToken   string  `db:"access_token"`
		APIToken      *string `db:"api_token"`
		OAuthProvider *string `db:"oauth_provider"`
		ExtraConfig   *string `db:"extra_config"`
		Status        string  `db:"status"`
	}
	var accounts []accountRow
	if err := h.db.SelectContext(ctx, &accounts, h.db.Rebind(`
		SELECT id, site_id, access_token, api_token, oauth_provider, extra_config, status
		FROM accounts
		WHERE site_id = ?
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, id ASC
	`), site.ID); err != nil {
		return nil, "", fmt.Errorf("load site accounts: %w", err)
	}
	if len(accounts) == 0 {
		return nil, "no accounts for site", nil
	}

	// Map channels by model (prefer enabled).
	channelByModel := map[string]channelRow{}
	for _, ch := range channels {
		if ch.SourceModel == nil {
			continue
		}
		m := strings.TrimSpace(*ch.SourceModel)
		if m == "" {
			continue
		}
		existing, ok := channelByModel[m]
		if !ok || (!existing.Enabled && ch.Enabled) {
			channelByModel[m] = ch
		}
	}

	// Account lookup.
	accountByID := map[int64]accountRow{}
	var fallbackAccount accountRow
	for i, acc := range accounts {
		accountByID[acc.ID] = acc
		if i == 0 || (fallbackAccount.Status != "active" && acc.Status == "active") {
			fallbackAccount = acc
		}
	}

	// Optional token cache.
	tokenCache := map[int64]string{}

	models := make([]string, 0, len(modelSet))
	for m := range modelSet {
		models = append(models, m)
	}
	sortStringsCI(models)

	targets := make([]siteProbeTarget, 0, len(models))
	for _, model := range models {
		var acc accountRow
		var channelID int64
		var tokenID *int64

		if ch, ok := channelByModel[model]; ok {
			channelID = ch.ID
			tokenID = ch.TokenID
			if a, ok := accountByID[ch.AccountID]; ok {
				acc = a
			}
		}
		if acc.ID == 0 {
			acc = fallbackAccount
		}
		if acc.ID == 0 {
			continue
		}

		tokenValue := ""
		if tokenID != nil && *tokenID > 0 {
			if cached, ok := tokenCache[*tokenID]; ok {
				tokenValue = cached
			} else {
				var tr harnessTokenRow
				if err := h.db.GetContext(ctx, &tr, h.db.Rebind(`
					SELECT id, token, enabled FROM account_tokens WHERE id = ?`), *tokenID); err == nil {
					if tr.Enabled {
						tokenValue = tr.Token
					}
				}
				tokenCache[*tokenID] = tokenValue
			}
		}
		if tokenValue == "" {
			tokenValue = resolveHarnessToken(tokenID, nil, harnessAccountRow{
				ID:            acc.ID,
				SiteID:        acc.SiteID,
				AccessToken:   acc.AccessToken,
				APIToken:      acc.APIToken,
				OAuthProvider: acc.OAuthProvider,
				ExtraConfig:   acc.ExtraConfig,
				Status:        acc.Status,
			})
		}

		targets = append(targets, siteProbeTarget{
			ModelName: model,
			ChannelID: channelID,
			AccountID: acc.ID,
			SiteID:    site.ID,
			SiteURL:   site.URL,
			SiteName:  site.Name,
			Platform:  site.Platform,
			Token:     tokenValue,
			Account: store.Account{
				ID:            acc.ID,
				SiteID:        acc.SiteID,
				AccessToken:   acc.AccessToken,
				APIToken:      acc.APIToken,
				OAuthProvider: acc.OAuthProvider,
				ExtraConfig:   acc.ExtraConfig,
				Status:        acc.Status,
			},
			Site: site,
		})
	}

	if len(targets) == 0 {
		return nil, "no probeable account/model pairs", nil
	}
	return targets, "", nil
}

func (h *sitesHandler) probeOneSiteModel(
	ctx context.Context,
	probe *channelTestHandler,
	target siteProbeTarget,
	latencyThresholdMs int,
) siteProbeModelResult {
	result := siteProbeModelResult{
		ModelName: target.ModelName,
		Status:    "unsupported",
		AccountID: target.AccountID,
		ChannelID: target.ChannelID,
	}

	if target.Token == "" {
		result.Status = "skipped"
		result.Reason = "missing credential / no usable token"
		result.Error = result.Reason
		return result
	}

	timeoutMs := int64(siteProbeDefaultTimeoutMs)
	if timeoutMs < siteProbeMinTimeoutMs {
		timeoutMs = siteProbeMinTimeoutMs
	}
	if timeoutMs > siteProbeMaxTimeoutMs {
		timeoutMs = siteProbeMaxTimeoutMs
	}

	harnessTarget := &channelTestTarget{
		ChannelID:   target.ChannelID,
		AccountID:   target.AccountID,
		SiteID:      target.SiteID,
		SourceModel: target.ModelName,
		SiteName:    target.SiteName,
		SiteURL:     target.SiteURL,
		Platform:    target.Platform,
		TokenValue:  target.Token,
		Account:     target.Account,
		Site:        target.Site,
	}

	out := probe.executeProbe(ctx, harnessTarget, channelTestModeChat, target.ModelName, channelTestDefaultPrompt, timeoutMs)

	var latency int64
	if v, ok := out["latencyMs"].(int64); ok {
		latency = v
	} else if v, ok := out["latencyMs"].(float64); ok {
		latency = int64(v)
	} else if v, ok := out["latencyMs"].(int); ok {
		latency = int64(v)
	}
	result.LatencyMs = &latency

	success, _ := out["success"].(bool)
	errText := ""
	switch v := out["error"].(type) {
	case string:
		errText = v
	case nil:
		errText = ""
	default:
		errText = fmt.Sprint(v)
	}

	if success {
		if latencyThresholdMs > 0 && latency > int64(latencyThresholdMs) {
			result.Status = "unsupported"
			result.Available = false
			result.LatencyExceeded = true
			result.Reason = fmt.Sprintf("响应延迟超过阈值 (%dms > %dms)", latency, latencyThresholdMs)
			result.Error = result.Reason
			return result
		}
		result.Status = "supported"
		result.Available = true
		return result
	}

	result.Status = "unsupported"
	result.Available = false
	if errText == "" {
		if code, ok := out["statusCode"].(int); ok && code > 0 {
			errText = fmt.Sprintf("upstream status %d", code)
		} else if code, ok := out["statusCode"].(float64); ok && code > 0 {
			errText = fmt.Sprintf("upstream status %d", int(code))
		} else {
			errText = "probe failed"
		}
	}
	result.Reason = errText
	result.Error = errText
	return result
}

func upsertModelAvailability(db interface {
	Get(dest any, query string, args ...any) error
	Exec(query string, args ...any) (sql.Result, error)
	Rebind(query string) string
}, accountID int64, modelName string, available bool, latencyMs *int64) error {
	modelName = strings.TrimSpace(modelName)
	if accountID <= 0 || modelName == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var existingID int64
	err := db.Get(&existingID, db.Rebind(`
		SELECT id FROM model_availability WHERE account_id = ? AND model_name = ?`), accountID, modelName)
	if err == nil {
		_, err = db.Exec(db.Rebind(`
			UPDATE model_availability
			SET available = ?, is_manual = ?, latency_ms = ?, checked_at = ?
			WHERE id = ?`), available, false, latencyMs, now, existingID)
		return err
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = db.Exec(db.Rebind(`
		INSERT INTO model_availability (account_id, model_name, available, is_manual, latency_ms, checked_at)
		VALUES (?, ?, ?, ?, ?, ?)`), accountID, modelName, available, false, latencyMs, now)
	return err
}

func ensureSiteDisabledModel(db interface {
	Get(dest any, query string, args ...any) error
	Exec(query string, args ...any) (sql.Result, error)
	Rebind(query string) string
}, siteID int64, modelName string) error {
	modelName = strings.TrimSpace(modelName)
	if siteID <= 0 || modelName == "" {
		return nil
	}
	var existing string
	err := db.Get(&existing, db.Rebind(`
		SELECT model_name FROM site_disabled_models WHERE site_id = ? AND model_name = ?`), siteID, modelName)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		// Some drivers may return empty without ErrNoRows; treat non-nil carefully.
		// Fall through to insert attempt for robustness.
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(db.Rebind(`
		INSERT INTO site_disabled_models (site_id, model_name, created_at) VALUES (?, ?, ?)`), siteID, modelName, now)
	return err
}

// applySiteProbeHealth stamps site runtime probe status only.
// Forced admin probes intentionally do not mutate channel cooldown.
func applySiteProbeHealth(_ context.Context, target siteProbeTarget, result siteProbeModelResult) {
	status := "inconclusive"
	latency := 0.0
	if result.LatencyMs != nil {
		latency = float64(*result.LatencyMs)
	}
	switch result.Status {
	case "supported":
		status = "success"
	case "unsupported":
		status = "failure"
	case "skipped":
		return
	}
	model := target.ModelName
	var channelPtr *int64
	if target.ChannelID > 0 {
		id := target.ChannelID
		channelPtr = &id
	}
	var errPtr *string
	if result.Error != "" {
		e := result.Error
		errPtr = &e
	}
	recordSiteProbeOutcome(target.SiteID, status, latency, &model, channelPtr, errPtr)
}

// recordSiteProbeOutcome is a tiny seam so tests can stub if needed.
var recordSiteProbeOutcome = defaultRecordSiteProbeOutcome
