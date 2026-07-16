package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Codex model discovery / allowlist helpers for OAuth accounts.
//
// Upstream TS used a fixed 12s discovery timeout (modelService.ts) and fixtures
// that only exercised gpt-5.4. Cold-start Codex OAuth discovery often needs a
// longer first attempt plus one soft retry, and allowlists must accept the
// gpt-5.x family (including gpt-5.5) without hardcoding only obsolete models.

const (
	// CodexModelDiscoveryTimeout is the first-attempt budget for Codex cloud
	// model discovery. Raised from the historical 12s upstream default so cold
	// starts (proxy + ChatGPT backend) do not fail spuriously.
	CodexModelDiscoveryTimeout = 30 * time.Second

	// CodexModelDiscoveryRetryTimeout is the budget for the soft-retry attempt
	// after a timeout. Slightly longer than the first attempt.
	CodexModelDiscoveryRetryTimeout = 45 * time.Second

	// CodexModelDiscoveryBackoff is the pause between first timeout and retry.
	CodexModelDiscoveryBackoff = 750 * time.Millisecond

	// CodexModelDiscoveryMaxAttempts is 1 initial + 1 soft retry on timeout.
	CodexModelDiscoveryMaxAttempts = 2

	// CodexQuotaProbeModelFallback is used when no discovered/preferred model
	// is available for the reverse-engineered quota probe.
	CodexQuotaProbeModelFallback = "gpt-5.5"

	// CodexModelsClientVersion is the query param expected by Codex /models.
	CodexModelsClientVersion = "1.0.0"
)

// CodexSeedModels is a non-exhaustive, version-flexible seed of known Codex
// cloud models used for fixtures and allowlist regression tests. It must keep
// current models (gpt-5.5) alongside recent predecessors so allowlists are not
// obsolete-only.
var CodexSeedModels = []string{
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.3-codex",
	"gpt-5.2-codex",
	"gpt-5.2",
	"gpt-5.1-codex-max",
	"gpt-5.1-codex",
	"gpt-5.1",
	"gpt-5.1-codex-mini",
}

// Preferred order for quota probe model selection (newest / most capable first).
var codexQuotaProbePreference = []string{
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.3-codex",
	"gpt-5.2-codex",
	"gpt-5.2",
	"gpt-5.1-codex-max",
	"gpt-5.1-codex",
	"gpt-5.1",
}

var gpt5FamilyPattern = regexp.MustCompile(`(?i)^gpt-5(\.|$|-)`)

// CodexModelDiscoveryErrorCode classifies discovery failures for operators.
type CodexModelDiscoveryErrorCode string

const (
	CodexDiscoveryTimeout      CodexModelDiscoveryErrorCode = "timeout"
	CodexDiscoveryUnauthorized CodexModelDiscoveryErrorCode = "unauthorized"
	CodexDiscoveryEmptyModels  CodexModelDiscoveryErrorCode = "empty_models"
	CodexDiscoveryUnknown      CodexModelDiscoveryErrorCode = "unknown"
)

// CodexModelDiscoveryResult is the outcome of a discovery pass.
type CodexModelDiscoveryResult struct {
	Models      []string                     `json:"models"`
	Status      OauthModelDiscoveryStatus    `json:"status"`
	ErrorCode   CodexModelDiscoveryErrorCode `json:"errorCode,omitempty"`
	ErrorMessage string                      `json:"errorMessage,omitempty"`
	Attempts    int                          `json:"attempts"`
	TimedOut    bool                         `json:"timedOut,omitempty"`
	DurationMs  int64                        `json:"durationMs"`
}

// CodexModelDiscoveryInput holds inputs for cloud model discovery.
type CodexModelDiscoveryInput struct {
	BaseURL     string
	AccessToken string
	AccountID   string
	ProxyURL    *string
	// HTTPClient overrides the default OAuth HTTP client (tests).
	HTTPClient *http.Client
	// Now is optional clock for tests.
	Now func() time.Time
	// Sleep is optional sleeper for backoff (tests).
	Sleep func(context.Context, time.Duration) error
}

// IsCodexGPT5FamilyModel reports whether model is in the gpt-5.x family
// (gpt-5, gpt-5.4, gpt-5.5, gpt-5.2-codex, …). Version-flexible — does not
// hardcode a single minor version.
func IsCodexGPT5FamilyModel(model string) bool {
	name := strings.TrimSpace(model)
	if name == "" {
		return false
	}
	return gpt5FamilyPattern.MatchString(name)
}

// IsCodexModelAllowed reports whether a model is permitted for Codex OAuth
// routing/discovery/quota paths. gpt-5.x family models (including gpt-5.5) are
// always allowed; any other name is allowed only when present in discovered.
func IsCodexModelAllowed(model string, discovered []string) bool {
	name := strings.TrimSpace(model)
	if name == "" {
		return false
	}
	if IsCodexGPT5FamilyModel(name) {
		return true
	}
	target := strings.ToLower(name)
	for _, d := range discovered {
		if strings.ToLower(strings.TrimSpace(d)) == target {
			return true
		}
	}
	return false
}

// NormalizeDiscoveredCodexModels trims, drops empties, and case-insensitively
// dedupes while preserving first-seen original casing.
func NormalizeDiscoveredCodexModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, raw := range models {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}

// MergeCodexDiscoveredWithSeed merges live discovery with seed models so that
// fixtures and first-login paths keep gpt-5.5 (and family) available even when
// upstream returns a partial list. Discovered names win on case collisions.
func MergeCodexDiscoveredWithSeed(discovered []string) []string {
	merged := NormalizeDiscoveredCodexModels(discovered)
	seen := make(map[string]struct{}, len(merged)+len(CodexSeedModels))
	for _, m := range merged {
		seen[strings.ToLower(m)] = struct{}{}
	}
	for _, seed := range CodexSeedModels {
		key := strings.ToLower(seed)
		if _, ok := seen[key]; ok {
			continue
		}
		// Only seed gpt-5.x family; never invent non-family models.
		if !IsCodexGPT5FamilyModel(seed) {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, seed)
	}
	return merged
}

// FilterCodexAllowedModels keeps models that pass IsCodexModelAllowed.
// When discovered is empty, only gpt-5.x family names pass.
func FilterCodexAllowedModels(models []string, discovered []string) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		if IsCodexModelAllowed(m, discovered) {
			out = append(out, strings.TrimSpace(m))
		}
	}
	return NormalizeDiscoveredCodexModels(out)
}

// SelectCodexQuotaProbeModel chooses a model for the Codex quota probe.
// Prefers the newest known preferred model present in discovered; otherwise
// prefers any gpt-5.x from discovered; finally falls back to gpt-5.5 so the
// probe path is not stuck on obsolete-only gpt-5.4.
func SelectCodexQuotaProbeModel(discovered []string) string {
	normalized := NormalizeDiscoveredCodexModels(discovered)
	if len(normalized) > 0 {
		index := make(map[string]string, len(normalized))
		for _, m := range normalized {
			index[strings.ToLower(m)] = m
		}
		for _, pref := range codexQuotaProbePreference {
			if actual, ok := index[strings.ToLower(pref)]; ok {
				return actual
			}
		}
		// Any gpt-5.x from discovery, sorted for stability.
		var family []string
		for _, m := range normalized {
			if IsCodexGPT5FamilyModel(m) {
				family = append(family, m)
			}
		}
		if len(family) > 0 {
			sort.Slice(family, func(i, j int) bool {
				return strings.ToLower(family[i]) > strings.ToLower(family[j])
			})
			return family[0]
		}
		return normalized[0]
	}
	return CodexQuotaProbeModelFallback
}

// ClassifyCodexModelDiscoveryError maps error text to a stable error code.
func ClassifyCodexModelDiscoveryError(err error) CodexModelDiscoveryErrorCode {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "timed out"),
		strings.Contains(msg, "deadline exceeded"),
		strings.Contains(msg, "context deadline"),
		strings.Contains(msg, "请求超时"):
		return CodexDiscoveryTimeout
	case strings.Contains(msg, "http 401"),
		strings.Contains(msg, "unauthorized"),
		strings.Contains(msg, "unauthenticated"):
		return CodexDiscoveryUnauthorized
	case strings.Contains(msg, "未获取到可用模型"),
		strings.Contains(msg, "empty"),
		strings.Contains(msg, "no models"):
		return CodexDiscoveryEmptyModels
	default:
		return CodexDiscoveryUnknown
	}
}

// FormatCodexModelDiscoveryTimeoutStatus returns an operator-facing message
// when first/retry discovery times out.
func FormatCodexModelDiscoveryTimeoutStatus(timeout time.Duration, attempts int) string {
	sec := int(timeout.Round(time.Second) / time.Second)
	if sec <= 0 {
		sec = int(CodexModelDiscoveryTimeout / time.Second)
	}
	return fmt.Sprintf(
		"Codex model discovery timed out after %d attempt(s) (budget %ds). "+
			"Cold-start ChatGPT Codex backends often need >12s; ensure account proxy is reachable, "+
			"retry connection refresh, or raise the discovery budget (default first attempt %ds, soft-retry %ds). "+
			"Status=abnormal until a successful discovery writes healthy + lastDiscoveredModels.",
		attempts,
		sec,
		int(CodexModelDiscoveryTimeout/time.Second),
		int(CodexModelDiscoveryRetryTimeout/time.Second),
	)
}

// BuildCodexModelsEndpoint builds GET .../models?client_version=...
func BuildCodexModelsEndpoint(baseURL string) string {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if normalized == "" {
		normalized = codexUpstreamBaseURL
	}
	return normalized + "/models?client_version=" + url.QueryEscape(CodexModelsClientVersion)
}

// ExtractCodexModelIDs parses Codex /models JSON payloads (array | models|data|items).
func ExtractCodexModelIDs(payload any) []string {
	collection := extractCodexModelCollection(payload)
	var ids []string
	for _, item := range collection {
		switch v := item.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				ids = append(ids, s)
			}
		case map[string]any:
			id := firstNonEmptyString(
				asNonEmptyString(v["id"]),
				asNonEmptyString(v["slug"]),
				asNonEmptyString(v["model"]),
			)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	return NormalizeDiscoveredCodexModels(ids)
}

func extractCodexModelCollection(payload any) []any {
	switch v := payload.(type) {
	case []any:
		return v
	case map[string]any:
		for _, key := range []string{"models", "data", "items"} {
			if arr, ok := v[key].([]any); ok {
				return arr
			}
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// DiscoverCodexModelsFromCloud performs a single GET /models discovery call.
func DiscoverCodexModelsFromCloud(ctx context.Context, input CodexModelDiscoveryInput) ([]string, error) {
	accessToken := strings.TrimSpace(input.AccessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("codex oauth access token missing")
	}
	endpoint := BuildCodexModelsEndpoint(input.BaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", "codex_cli_rs")
	if accountID := strings.TrimSpace(input.AccountID); accountID != "" {
		req.Header.Set("Chatgpt-Account-Id", accountID)
	}

	client := input.HTTPClient
	if client == nil {
		client = newOAuthHTTPClient(nil)
	}
	// Honour proxy via doHTTP when custom client not provided.
	var resp *http.Response
	if input.HTTPClient != nil {
		resp, err = client.Do(req)
	} else {
		resp, err = doHTTP(req, input.ProxyURL, client)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("codex model discovery timeout (%ds): %w",
				int(CodexModelDiscoveryTimeout/time.Second), ctx.Err())
		}
		return nil, fmt.Errorf("codex model discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := readOAuthErrorResponseBody(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := readOAuthJSONResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("codex model discovery response read failed: %w", err)
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("codex model discovery returned invalid payload")
	}
	models := ExtractCodexModelIDs(payload)
	if len(models) == 0 {
		return nil, fmt.Errorf("未获取到可用模型")
	}
	return models, nil
}

// DiscoverCodexModelsWithSoftRetry runs cloud discovery with an increased first
// timeout and one soft-retry/backoff on timeout. Non-timeout errors fail fast.
func DiscoverCodexModelsWithSoftRetry(
	ctx context.Context,
	input CodexModelDiscoveryInput,
	fetch func(context.Context, CodexModelDiscoveryInput) ([]string, error),
) *CodexModelDiscoveryResult {
	if fetch == nil {
		fetch = DiscoverCodexModelsFromCloud
	}
	now := input.Now
	if now == nil {
		now = time.Now
	}
	sleep := input.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}

	started := now()
	timeouts := []time.Duration{CodexModelDiscoveryTimeout, CodexModelDiscoveryRetryTimeout}
	var lastErr error
	attempts := 0
	timedOut := false

	for i, budget := range timeouts {
		if i >= CodexModelDiscoveryMaxAttempts {
			break
		}
		if err := ctx.Err(); err != nil {
			lastErr = err
			break
		}
		attempts++
		attemptCtx, cancel := context.WithTimeout(ctx, budget)
		models, err := fetch(attemptCtx, input)
		cancel()
		if err == nil {
			normalized := NormalizeDiscoveredCodexModels(models)
			// Ensure gpt-5.5 family is not dropped by partial upstream lists when
			// seed merge is desired by callers — discovery itself returns live set.
			return &CodexModelDiscoveryResult{
				Models:     normalized,
				Status:     OauthModelDiscoveryHealthy,
				Attempts:   attempts,
				DurationMs: now().Sub(started).Milliseconds(),
			}
		}
		lastErr = err
		code := ClassifyCodexModelDiscoveryError(err)
		if code != CodexDiscoveryTimeout {
			// Soft-retry only for timeouts (cold start). Auth/empty fail fast.
			msg := formatCodexDiscoveryFailure(code, err, attempts, budget, false)
			return &CodexModelDiscoveryResult{
				Models:       nil,
				Status:       OauthModelDiscoveryAbnormal,
				ErrorCode:    code,
				ErrorMessage: msg,
				Attempts:     attempts,
				TimedOut:     false,
				DurationMs:   now().Sub(started).Milliseconds(),
			}
		}
		timedOut = true
		// Soft-retry once after backoff when another attempt remains.
		if i+1 < len(timeouts) {
			if err := sleep(ctx, CodexModelDiscoveryBackoff); err != nil {
				lastErr = err
				break
			}
			continue
		}
	}

	code := ClassifyCodexModelDiscoveryError(lastErr)
	if code == "" {
		code = CodexDiscoveryUnknown
	}
	budget := CodexModelDiscoveryRetryTimeout
	if attempts == 1 {
		budget = CodexModelDiscoveryTimeout
	}
	msg := formatCodexDiscoveryFailure(code, lastErr, attempts, budget, timedOut)
	return &CodexModelDiscoveryResult{
		Models:       nil,
		Status:       OauthModelDiscoveryAbnormal,
		ErrorCode:    code,
		ErrorMessage: msg,
		Attempts:     attempts,
		TimedOut:     timedOut || code == CodexDiscoveryTimeout,
		DurationMs:   now().Sub(started).Milliseconds(),
	}
}

func formatCodexDiscoveryFailure(
	code CodexModelDiscoveryErrorCode,
	err error,
	attempts int,
	budget time.Duration,
	timedOut bool,
) string {
	if timedOut || code == CodexDiscoveryTimeout {
		return FormatCodexModelDiscoveryTimeoutStatus(budget, attempts)
	}
	raw := "codex model discovery failed"
	if err != nil {
		raw = err.Error()
	}
	return fmt.Sprintf("Codex 模型获取失败（%s）", raw)
}

// BuildOauthModelDiscoveryPatch builds the OauthInfo patch used to persist
// discovery status after a successful or failed pass.
func BuildOauthModelDiscoveryPatch(result *CodexModelDiscoveryResult, checkedAt string) *OauthInfo {
	if result == nil {
		return nil
	}
	if checkedAt == "" {
		checkedAt = time.Now().UTC().Format(time.RFC3339)
	}
	patch := &OauthInfo{
		ModelDiscoveryStatus: result.Status,
		LastModelSyncAt:      checkedAt,
		LastDiscoveredModels: result.Models,
	}
	if result.Status == OauthModelDiscoveryAbnormal {
		patch.LastModelSyncError = result.ErrorMessage
	} else {
		// Clear previous error on success by writing empty — callers merge carefully.
		patch.LastModelSyncError = ""
	}
	return patch
}
