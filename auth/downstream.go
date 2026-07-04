package auth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// ---------------------------------------------------------------------------
// DownstreamTokenAuthResult — returned by AuthorizeDownstreamToken.
// ---------------------------------------------------------------------------

// DownstreamTokenAuthResult is the result of authorizing a downstream token.
// On success (OK=true): Source, Token, Key (maybe nil for global), and Policy are populated.
// On failure (OK=false): StatusCode, Error, and Reason are populated.
type DownstreamTokenAuthResult struct {
	OK         bool
	Source     string               // "managed" | "global" (when OK)
	Token      string               // normalized token (when OK)
	Key        *managedKeyView      // managed key view (when OK, source="managed")
	Policy     DownstreamRoutingPolicy // resolved policy (when OK)
	StatusCode int                  // HTTP status (when !OK)
	Error      string               // error message (when !OK)
	Reason     string               // "missing"|"invalid"|"disabled"|"expired"|"over_cost"|"over_requests"
}

// managedKeyView is an internal struct mirroring the downstream_api_keys row
// with parsed JSON columns. Used only within the auth package for authorization.
type managedKeyView struct {
	ID           int64
	Name         string
	Enabled      bool
	ExpiresAt    *string
	MaxCost      *float64
	UsedCost     float64
	MaxRequests  *int64
	UsedRequests int64
	// Policy fields — parsed from JSON TEXT columns
	SupportedModels       []string
	AllowedRouteIDs       []int64
	SiteWeightMultipliers map[int64]float64
	ExcludedSiteIDs       []int64
	ExcludedCredentialRefs []ExcludedCredentialRef
}

// ---------------------------------------------------------------------------
// AuthorizeDownstreamToken — the core downstream auth logic.
// ---------------------------------------------------------------------------

// AuthorizeDownstreamToken validates a downstream token against the managed key
// table and the global proxy token.
//
// Flow (matches TS authorizeDownstreamToken exactly):
//   1. If token is empty → 401, reason: "missing"
//   2. Query downstream_api_keys WHERE key = token:
//      a. Found → check enabled/expired/over_cost/over_requests → return managed result
//      b. Not found → continue
//   3. Check if token == config.ProxyToken:
//      a. Match → return global result
//      b. No match → 403, reason: "invalid"
func AuthorizeDownstreamToken(token string, cfg *config.Config) DownstreamTokenAuthResult {
	normalized := strings.TrimSpace(token)
	if normalized == "" {
		return DownstreamTokenAuthResult{
			OK:         false,
			StatusCode: 401,
			Error:      "Missing Authorization or x-api-key header",
			Reason:     "missing",
		}
	}

	// ---- Check managed key table ----
	managed, err := getManagedKeyByToken(normalized)
	if err != nil {
		slog.Warn("downstream auth: failed to query managed key, falling back to global", "error", err)
		// DO NOT return 500. Fall through to global token check below.
		// This matches the TypeScript resilience pattern where the DB query
		// failure does not block the global token fallback.
		managed = nil
	}

	if managed != nil {
		// Check enabled
		if !managed.Enabled {
			return DownstreamTokenAuthResult{
				OK:         false,
				StatusCode: 403,
				Error:      "API key is disabled",
				Reason:     "disabled",
			}
		}

		// Check expiration
		if managed.ExpiresAt != nil {
			expiresAt, parseErr := parseISO8601(*managed.ExpiresAt)
			if parseErr == nil && !expiresAt.After(time.Now()) {
				return DownstreamTokenAuthResult{
					OK:         false,
					StatusCode: 403,
					Error:      "API key is expired",
					Reason:     "expired",
				}
			}
		}

		// Check max cost
		if managed.MaxCost != nil && managed.UsedCost >= *managed.MaxCost {
			return DownstreamTokenAuthResult{
				OK:         false,
				StatusCode: 403,
				Error:      "API key has exceeded max cost",
				Reason:     "over_cost",
			}
		}

		// Check max requests
		if managed.MaxRequests != nil && managed.UsedRequests >= *managed.MaxRequests {
			return DownstreamTokenAuthResult{
				OK:         false,
				StatusCode: 403,
				Error:      "API key has exceeded max requests",
				Reason:     "over_requests",
			}
		}

		// Success — build policy from managed key view
		policy := toPolicyFromView(managed)

		return DownstreamTokenAuthResult{
			OK:     true,
			Source: "managed",
			Token:  normalized,
			Key:    managed,
			Policy: policy,
		}
	}

	// ---- Check global proxy token ----
	if normalized == cfg.ProxyToken {
		return DownstreamTokenAuthResult{
			OK:     true,
			Source: "global",
			Token:  normalized,
			Key:    nil,
			Policy: EmptyDownstreamRoutingPolicy,
		}
	}

	// ---- No match ----
	return DownstreamTokenAuthResult{
		OK:         false,
		StatusCode: 403,
		Error:      "Invalid API key",
		Reason:     "invalid",
	}
}

// ---------------------------------------------------------------------------
// DB query — managed key lookup with JSON column parsing.
// ---------------------------------------------------------------------------

// getManagedKeyByToken queries the downstream_api_keys table for a row matching
// the given token (key column). Returns nil if no row matches.
func getManagedKeyByToken(token string) (*managedKeyView, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	row := db.QueryRowx(
		`SELECT id, name, enabled, expires_at, max_cost, used_cost,
		        max_requests, used_requests,
		        supported_models, allowed_route_ids,
		        site_weight_multipliers, excluded_site_ids,
		        excluded_credential_refs
		 FROM downstream_api_keys
		 WHERE key = ?`, token,
	)

	var v managedKeyView
	var supportedModelsJSON, allowedRouteIDsJSON *string
	var siteWeightMultiJSON, excludedSiteIDsJSON *string
	var excludedCredRefsJSON *string

	err := row.Scan(
		&v.ID, &v.Name, &v.Enabled, &v.ExpiresAt, &v.MaxCost, &v.UsedCost,
		&v.MaxRequests, &v.UsedRequests,
		&supportedModelsJSON, &allowedRouteIDsJSON,
		&siteWeightMultiJSON, &excludedSiteIDsJSON,
		&excludedCredRefsJSON,
	)
	if err != nil {
		// sql.ErrNoRows → return nil, nil (not found)
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("query downstream_api_keys: %w", err)
	}

	// Parse JSON columns into typed slices/maps
	v.SupportedModels = parseStringArray(supportedModelsJSON)
	v.AllowedRouteIDs = parseInt64Array(allowedRouteIDsJSON)
	v.SiteWeightMultipliers = parseSiteWeightMultipliers(siteWeightMultiJSON)
	v.ExcludedSiteIDs = parseInt64Array(excludedSiteIDsJSON)
	v.ExcludedCredentialRefs = parseExcludedCredentialRefs(excludedCredRefsJSON)

	return &v, nil
}

// ---------------------------------------------------------------------------
// Usage consumption — atomic SQL increment (matches TS).
// ---------------------------------------------------------------------------

// consumeManagedKeyRequest atomically increments used_requests for a managed
// key via SQL COALESCE to avoid lost updates under concurrency.
//
// SQL:
//   UPDATE downstream_api_keys
//   SET used_requests = COALESCE(used_requests, 0) + 1,
//       last_used_at = ?, updated_at = ?
//   WHERE id = ?
func consumeManagedKeyRequest(keyID int64) {
	db := store.GetDB()
	if db == nil {
		slog.Warn("consumeManagedKeyRequest: database not initialized, skipping")
		return
	}

	nowISO := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE downstream_api_keys
		 SET used_requests = COALESCE(used_requests, 0) + 1,
		     last_used_at = ?,
		     updated_at = ?
		 WHERE id = ?`,
		nowISO, nowISO, keyID,
	)
	if err != nil {
		slog.Warn("consumeManagedKeyRequest: failed to increment used_requests",
			"keyID", keyID, "error", err)
	}
}

// RecordManagedKeyCostUsage atomically increments used_cost for a managed key.
// Only called when estimatedCost > 0 (checked by caller).
// This is NOT called from the auth middleware; it is called by the proxy
// handler after the request completes.
//
// SQL:
//   UPDATE downstream_api_keys
//   SET used_cost = COALESCE(used_cost, 0) + ?,
//       last_used_at = ?, updated_at = ?
//   WHERE id = ?
func RecordManagedKeyCostUsage(keyID int64, estimatedCost float64) {
	if estimatedCost <= 0 || math.IsNaN(estimatedCost) || math.IsInf(estimatedCost, 0) {
		return
	}

	db := store.GetDB()
	if db == nil {
		slog.Warn("RecordManagedKeyCostUsage: database not initialized, skipping")
		return
	}

	nowISO := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE downstream_api_keys
		 SET used_cost = COALESCE(used_cost, 0) + ?,
		     last_used_at = ?,
		     updated_at = ?
		 WHERE id = ?`,
		estimatedCost, nowISO, nowISO, keyID,
	)
	if err != nil {
		slog.Warn("RecordManagedKeyCostUsage: failed to increment used_cost",
			"keyID", keyID, "cost", estimatedCost, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Policy derivation — matches TS toPolicyFromView.
// ---------------------------------------------------------------------------

// toPolicyFromView derives a DownstreamRoutingPolicy from a managed key view.
// Managed keys default to DenyAllWhenEmpty=true (deny all models when no
// patterns/routes are configured).
func toPolicyFromView(v *managedKeyView) DownstreamRoutingPolicy {
	return DownstreamRoutingPolicy{
		SupportedModels:        normalizeStringSlice(v.SupportedModels),
		AllowedRouteIDs:        normalizeInt64Slice(v.AllowedRouteIDs),
		SiteWeightMultipliers:  normalizeSiteWeightMap(v.SiteWeightMultipliers),
		ExcludedSiteIDs:        normalizeInt64Slice(v.ExcludedSiteIDs),
		ExcludedCredentialRefs: normalizeExcludedRefs(v.ExcludedCredentialRefs),
		DenyAllWhenEmpty:       true, // managed keys default to deny-all
	}
}

// ---------------------------------------------------------------------------
// JSON parsing helpers for TEXT columns from downstream_api_keys.
// ---------------------------------------------------------------------------

// parseStringArray parses a JSON TEXT column containing an array of strings.
// Returns nil on empty, null, or parse errors.
func parseStringArray(raw *string) []string {
	if raw == nil || *raw == "" || *raw == "null" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(*raw), &arr); err != nil {
		return nil
	}
	return arr
}

// parseInt64Array parses a JSON TEXT column containing an array of numbers.
// Returns an empty slice on nil, empty, or parse errors.
func parseInt64Array(raw *string) []int64 {
	if raw == nil || *raw == "" || *raw == "null" {
		return nil
	}
	var arr []json.Number
	if err := json.Unmarshal([]byte(*raw), &arr); err != nil {
		// Try parsing as []int64 directly (PostgreSQL may store differently)
		var intArr []int64
		if err2 := json.Unmarshal([]byte(*raw), &intArr); err2 != nil {
			return nil
		}
		return intArr
	}
	result := make([]int64, 0, len(arr))
	for _, n := range arr {
		v, err := n.Int64()
		if err != nil {
			continue
		}
		result = append(result, v)
	}
	return result
}

// parseSiteWeightMultipliers parses a JSON TEXT column containing a
// {"site_id": multiplier} object.
func parseSiteWeightMultipliers(raw *string) map[int64]float64 {
	if raw == nil || *raw == "" || *raw == "null" {
		return nil
	}
	var rawMap map[string]json.Number
	if err := json.Unmarshal([]byte(*raw), &rawMap); err != nil {
		// Try direct float map (PostgreSQL)
		var floatMap map[int64]float64
		if err2 := json.Unmarshal([]byte(*raw), &floatMap); err2 != nil {
			return nil
		}
		return floatMap
	}
	result := make(map[int64]float64, len(rawMap))
	for k, n := range rawMap {
		siteID, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue
		}
		multiplier, err := n.Float64()
		if err != nil {
			continue
		}
		result[siteID] = multiplier
	}
	return result
}

// parseExcludedCredentialRefs parses a JSON TEXT column containing an array of
// ExcludedCredentialRef objects.
func parseExcludedCredentialRefs(raw *string) []ExcludedCredentialRef {
	if raw == nil || *raw == "" || *raw == "null" {
		return nil
	}
	var refs []ExcludedCredentialRef
	if err := json.Unmarshal([]byte(*raw), &refs); err != nil {
		return nil
	}
	return refs
}

// ---------------------------------------------------------------------------
// Normalization helpers (no-ops for already-parsed data, but ensure non-nil).
// ---------------------------------------------------------------------------

func normalizeStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func normalizeInt64Slice(s []int64) []int64 {
	if s == nil {
		return []int64{}
	}
	return s
}

func normalizeSiteWeightMap(m map[int64]float64) map[int64]float64 {
	if m == nil {
		return map[int64]float64{}
	}
	return m
}

func normalizeExcludedRefs(r []ExcludedCredentialRef) []ExcludedCredentialRef {
	if r == nil {
		return []ExcludedCredentialRef{}
	}
	return r
}

// ---------------------------------------------------------------------------
// Utility helpers.
// ---------------------------------------------------------------------------

// parseISO8601 parses an ISO 8601 / RFC 3339 timestamp string.
// Tries time.RFC3339 first, then time.RFC3339Nano.
func parseISO8601(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	t, err = time.Parse(time.RFC3339Nano, s)
	if err == nil {
		return t, nil
	}
	// Try a few more common ISO 8601 variants
	layouts := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		t, err = time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse ISO 8601: %q", s)
}

// isNoRows checks whether the error is a "no rows in result set" error.
func isNoRows(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no rows") ||
		strings.Contains(msg, "sql: no rows")
}
