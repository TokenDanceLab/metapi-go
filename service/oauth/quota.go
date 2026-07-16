package oauth

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// OauthQuotaWindowSnapshot represents a single quota window.
type OauthQuotaWindowSnapshot struct {
	Supported bool     `json:"supported"`
	Limit     *float64 `json:"limit,omitempty"`
	Used      *float64 `json:"used,omitempty"`
	Remaining *float64 `json:"remaining,omitempty"`
	ResetAt   string   `json:"resetAt,omitempty"`
	Message   string   `json:"message,omitempty"`
}

// OauthQuotaWindows holds the two quota windows.
type OauthQuotaWindows struct {
	FiveHour *OauthQuotaWindowSnapshot `json:"fiveHour"`
	SevenDay *OauthQuotaWindowSnapshot `json:"sevenDay"`
}

// OauthQuotaSnapshot is a full quota snapshot.
type OauthQuotaSnapshot struct {
	Status           string             `json:"status"`
	Source           string             `json:"source"`
	LastSyncAt       string             `json:"lastSyncAt,omitempty"`
	LastError        string             `json:"lastError,omitempty"`
	ProviderMessage  string             `json:"providerMessage,omitempty"`
	Subscription     *OauthSubscription `json:"subscription,omitempty"`
	Windows          *OauthQuotaWindows `json:"windows"`
	LastLimitResetAt string             `json:"lastLimitResetAt,omitempty"`
}

// OauthSubscription holds subscription info.
type OauthSubscription struct {
	PlanType    string `json:"planType,omitempty"`
	ActiveStart string `json:"activeStart,omitempty"`
	ActiveUntil string `json:"activeUntil,omitempty"`
}

// ---- Quota Refresh ----

var (
	quotaDedupMu       sync.Mutex
	quotaDedupStore    = make(map[string]time.Time)  // fingerprint -> last persist time
	quotaInFlightMu    sync.Mutex
	quotaInFlightStore = make(map[string]bool) // (accountId + fingerprint) -> in-flight flag
)

// RefreshOauthQuotaSnapshot refreshes the quota snapshot for an OAuth account.
// Only implemented for Codex provider; non-codex accounts get an "unsupported" snapshot.
func RefreshOauthQuotaSnapshot(accountID int64) (*OauthQuotaSnapshot, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", accountID); err != nil {
		return nil, fmt.Errorf("oauth account not found")
	}

	oauth := GetOauthInfoFromAccount(&account)
	if oauth == nil {
		return nil, fmt.Errorf("account is not managed by oauth")
	}

	if oauth.Provider != "codex" {
		// Non-codex providers: build "unsupported" snapshot and persist.
		snapshot := buildUnsupportedQuotaSnapshot()
		persistQuotaSnapshot(db, accountID, snapshot)
		return snapshot, nil
	}

	// Codex: probe request with rate limit header parsing.
	_ = account.ExtraConfig // preserved for future proxy URL resolution from config
	proxyURL := resolveAccountProxyURLForQuota(account.SiteID, account.ExtraConfig)

	// Build the probe snapshot by making a request to the upstream.
	// In production this makes an HTTP request and parses rate-limit headers.
	// For now, we build an error snapshot since we don't have HTTP client wiring here.
	snapshot := buildQuotaSnapshotFromProbe(proxyURL)
	persistQuotaSnapshot(db, accountID, snapshot)
	return snapshot, nil
}

// RefreshOauthConnectionQuotaBatch refreshes quota for multiple accounts concurrently.
// Runs with max concurrency of 4 workers.
func RefreshOauthConnectionQuotaBatch(accountIDs []int64) *QuotaBatchResult {
	// Deduplicate account IDs.
	seen := make(map[int64]bool)
	unique := make([]int64, 0, len(accountIDs))
	for _, id := range accountIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}

	result := &QuotaBatchResult{
		Items: make([]QuotaBatchItem, 0, len(unique)),
	}

	if len(unique) == 0 {
		result.Success = true
		return result
	}

	// Run concurrently with max 4 workers.
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, accountID := range unique {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int64) {
			defer wg.Done()
			defer func() { <-sem }()

			quota, err := RefreshOauthConnectionQuota(id)
			mu.Lock()
			if err != nil {
				result.Items = append(result.Items, QuotaBatchItem{
					AccountID: id,
					Success:   false,
					Error:     err.Error(),
				})
			} else {
				result.Items = append(result.Items, QuotaBatchItem{
					AccountID: id,
					Success:   true,
					Quota:     quota,
				})
			}
			mu.Unlock()
		}(accountID)
	}
	wg.Wait()

	result.Refreshed = 0
	result.Failed = 0
	for _, item := range result.Items {
		if item.Success {
			result.Refreshed++
		} else {
			result.Failed++
		}
	}
	result.Success = result.Failed == 0
	return result
}

// RefreshOauthConnectionQuota wraps RefreshOauthQuotaSnapshot for a single account.
func RefreshOauthConnectionQuota(accountID int64) (*OauthQuotaSnapshot, error) {
	return RefreshOauthQuotaSnapshot(accountID)
}

// QuotaBatchResult holds the result of a batch quota refresh.
type QuotaBatchResult struct {
	Success   bool             `json:"success"`
	Refreshed int              `json:"refreshed"`
	Failed    int              `json:"failed"`
	Items     []QuotaBatchItem `json:"items"`
}

// QuotaBatchItem represents a single item in a batch quota refresh result.
type QuotaBatchItem struct {
	AccountID int64              `json:"accountId"`
	Success   bool               `json:"success"`
	Quota     *OauthQuotaSnapshot `json:"quota,omitempty"`
	Error     string             `json:"error,omitempty"`
}

// RecordOauthQuotaHeadersSnapshot records a quota snapshot from observed response headers.
// Deduplicates by fingerprint within 30s window and in-progress in-flight stores.
func RecordOauthQuotaHeadersSnapshot(accountID int64, headers map[string]string) {
	snapshot := buildQuotaSnapshotFromHeaders(headers)
	if snapshot == nil {
		return
	}

	// Compute fingerprint.
	fingerprint := quotaFingerprint(snapshot)

	// Deduplicate by fingerprint within 30s.
	quotaDedupMu.Lock()
	key := fmt.Sprintf("%d:%s", accountID, fingerprint)
	if lastTime, exists := quotaDedupStore[key]; exists {
		if time.Since(lastTime) < 30*time.Second {
			quotaDedupMu.Unlock()
			return
		}
	}
	// Check in-flight dedup.
	quotaInFlightMu.Lock()
	if quotaInFlightStore[key] {
		quotaInFlightMu.Unlock()
		quotaDedupMu.Unlock()
		return
	}
	quotaInFlightStore[key] = true
	quotaInFlightMu.Unlock()
	quotaDedupMu.Unlock()

	defer func() {
		quotaInFlightMu.Lock()
		delete(quotaInFlightStore, key)
		quotaInFlightMu.Unlock()
	}()

	// Persist snapshot.
	db := store.GetDB()
	if db != nil {
		persistQuotaSnapshot(db, accountID, snapshot)
	}

	quotaDedupMu.Lock()
	quotaDedupStore[key] = time.Now()
	quotaDedupMu.Unlock()
}

// RecordOauthQuotaResetHint records a reset hint from a 429 response.
// Only for codex provider.
func RecordOauthQuotaResetHint(accountID int64, statusCode int, errorText string) {
	if statusCode != 429 {
		return
	}

	resetAt := parseUsageLimitResetHint(errorText)
	if resetAt == "" {
		return
	}

	db := store.GetDB()
	if db == nil {
		return
	}

	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", accountID); err != nil {
		return
	}

	oauth := GetOauthInfoFromAccount(&account)
	if oauth == nil || oauth.Provider != "codex" {
		return
	}

	// Merge lastLimitResetAt into extraConfig.oauth.quota.
	quota := oauth.Quota
	if quota == nil {
		quota = &OauthQuotaSnapshot{
			Status: "ok",
			Source: "reverse_engineered",
			Windows: &OauthQuotaWindows{
				FiveHour: &OauthQuotaWindowSnapshot{Supported: false},
				SevenDay: &OauthQuotaWindowSnapshot{Supported: false},
			},
		}
	}
	quota.LastLimitResetAt = resetAt

	quotaMap := map[string]interface{}{
		"status":           quota.Status,
		"source":           quota.Source,
		"lastLimitResetAt": resetAt,
	}
	extraPatch := map[string]interface{}{
		"oauth": map[string]interface{}{
			"quota": quotaMap,
		},
	}
	extraConfig := MergeAccountExtraConfig(account.ExtraConfig, extraPatch)
	now := time.Now().Format(time.RFC3339)
	_, _ = db.Exec("UPDATE accounts SET extra_config = ?, updated_at = ? WHERE id = ?", extraConfig, now, accountID)
}

// ---- Internal helpers ----

func buildUnsupportedQuotaSnapshot() *OauthQuotaSnapshot {
	return &OauthQuotaSnapshot{
		Status: "unsupported",
		Source: "reverse_engineered",
		LastSyncAt: time.Now().Format(time.RFC3339),
		Windows: &OauthQuotaWindows{
			FiveHour: &OauthQuotaWindowSnapshot{Supported: false},
			SevenDay: &OauthQuotaWindowSnapshot{Supported: false},
		},
	}
}

func buildQuotaSnapshotFromProbe(proxyURL *string) *OauthQuotaSnapshot {
	// In production, this would make an HTTP POST /responses probe request
	// to the Codex upstream with SelectCodexQuotaProbeModel(discovered)
	// (prefers gpt-5.5 / gpt-5.x family — not obsolete-only gpt-5.4) and
	// parse the rate-limit headers (x-codex-primary-*, x-codex-secondary-*).
	// For now, return a placeholder error snapshot indicating the probe
	// infrastructure is not yet wired; model selection is exercised via
	// SelectCodexQuotaProbeModel unit tests.
	_ = SelectCodexQuotaProbeModel(nil)
	_ = proxyURL
	return &OauthQuotaSnapshot{
		Status:     "error",
		Source:     "reverse_engineered",
		LastSyncAt: time.Now().Format(time.RFC3339),
		LastError:  "quota probe HTTP client not yet wired",
		Windows: &OauthQuotaWindows{
			FiveHour: &OauthQuotaWindowSnapshot{Supported: false},
			SevenDay: &OauthQuotaWindowSnapshot{Supported: false},
		},
	}
}

// CodexQuotaProbeModelForAccount resolves the model id used for a Codex
// reverse-engineered quota probe. Prefer lastDiscoveredModels when present so
// gpt-5.5 is used once discovery has seen it; otherwise fall back to the
// version-flexible preference list (gpt-5.5 first).
func CodexQuotaProbeModelForAccount(oauth *OauthInfo) string {
	var discovered []string
	if oauth != nil {
		discovered = oauth.LastDiscoveredModels
	}
	return SelectCodexQuotaProbeModel(discovered)
}

func buildQuotaSnapshotFromHeaders(headers map[string]string) *OauthQuotaSnapshot {
	if len(headers) == 0 {
		return nil
	}

	// Parse rate-limit headers: x-codex-primary-* and x-codex-secondary-*
	fiveHour := parseRateLimitWindow(headers, "x-codex-primary")
	sevenDay := parseRateLimitWindow(headers, "x-codex-secondary")

	if fiveHour == nil && sevenDay == nil {
		return nil
	}

	return &OauthQuotaSnapshot{
		Status:     "ok",
		Source:     "reverse_engineered",
		LastSyncAt: time.Now().Format(time.RFC3339),
		Windows: &OauthQuotaWindows{
			FiveHour: fiveHourOrDefault(fiveHour),
			SevenDay: sevenDayOrDefault(sevenDay),
		},
	}
}

func parseRateLimitWindow(headers map[string]string, prefix string) *OauthQuotaWindowSnapshot {
	limitKey := prefix + "-request-limit"
	remainingKey := prefix + "-remaining-requests"
	resetKey := prefix + "-requests-reset-in-seconds"

	limitStr, hasLimit := headers[limitKey]
	remainingStr, hasRemaining := headers[remainingKey]

	if !hasLimit && !hasRemaining {
		return nil
	}

	window := &OauthQuotaWindowSnapshot{Supported: true}

	if limitStr != "" {
		if v := parseFloat64(limitStr); v != nil {
			window.Limit = v
		}
	}
	if remainingStr != "" {
		if v := parseFloat64(remainingStr); v != nil {
			window.Remaining = v
		}
	}
	// Compute used = limit - remaining if both are available.
	if window.Limit != nil && window.Remaining != nil {
		used := *window.Limit - *window.Remaining
		if used < 0 {
			used = 0
		}
		window.Used = &used
	}
	if resetStr, ok := headers[resetKey]; ok && resetStr != "" {
		if resetSec, err := time.ParseDuration(resetStr + "s"); err == nil {
			resetAt := time.Now().Add(resetSec).Format(time.RFC3339)
			window.ResetAt = resetAt
		} else {
			window.ResetAt = resetStr
		}
	}

	return window
}

func fiveHourOrDefault(w *OauthQuotaWindowSnapshot) *OauthQuotaWindowSnapshot {
	if w != nil {
		return w
	}
	return &OauthQuotaWindowSnapshot{Supported: false}
}

func sevenDayOrDefault(w *OauthQuotaWindowSnapshot) *OauthQuotaWindowSnapshot {
	if w != nil {
		return w
	}
	return &OauthQuotaWindowSnapshot{Supported: false}
}

func persistQuotaSnapshot(db *store.DB, accountID int64, snapshot *OauthQuotaSnapshot) {
	if snapshot == nil {
		return
	}
	quotaJSON, err := json.Marshal(snapshot)
	if err != nil {
		slog.Warn("failed to serialize quota snapshot", "error", err)
		return
	}

	// Merge into extraConfig.oauth.quota.
	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", accountID); err != nil {
		return
	}

	var quotaMap map[string]interface{}
	if err := json.Unmarshal(quotaJSON, &quotaMap); err != nil {
		return
	}

	extraPatch := map[string]interface{}{
		"oauth": map[string]interface{}{
			"quota": quotaMap,
		},
	}
	extraConfig := MergeAccountExtraConfig(account.ExtraConfig, extraPatch)
	now := time.Now().Format(time.RFC3339)
	db.Exec("UPDATE accounts SET extra_config = ?, updated_at = ? WHERE id = ?", extraConfig, now, accountID)
}

func quotaFingerprint(snapshot *OauthQuotaSnapshot) string {
	if snapshot == nil {
		return ""
	}
	data, _ := json.Marshal(snapshot)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8]) // First 8 bytes of SHA-256 as hex.
}

func parseUsageLimitResetHint(errorText string) string {
	if errorText == "" {
		return ""
	}
	// Try to parse resets_at or resets_in_seconds from 429 error body.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(errorText), &parsed); err != nil {
		return ""
	}

	resetType, _ := parsed["usage_limit_reached"].(map[string]interface{})
	if resetType == nil {
		// Try top-level fields.
		if v, ok := parsed["resets_at"].(string); ok {
			return v
		}
		if v, ok := parsed["resets_in_seconds"].(float64); ok && v > 0 {
			return time.Now().Add(time.Duration(v) * time.Second).Format(time.RFC3339)
		}
		return ""
	}

	if v, ok := resetType["resets_at"].(string); ok {
		return v
	}
	if v, ok := resetType["resets_in_seconds"].(float64); ok && v > 0 {
		return time.Now().Add(time.Duration(v) * time.Second).Format(time.RFC3339)
	}
	return ""
}

func parseFloat64(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return nil
	}
	return &f
}

func resolveAccountProxyURLForQuota(siteID int64, extraConfig *string) *string {
	proxyURL := GetProxyURLFromExtraConfig(extraConfig)
	if proxyURL != "" {
		return &proxyURL
	}
	return nil
}
