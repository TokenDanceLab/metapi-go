package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
)

// ---------------------------------------------------------------------------
// extractProxyToken tests — token extraction from 4 sources with
// EXCLUSIVE Authorization priority semantics.
// ---------------------------------------------------------------------------

func newProxyRequest(method, path string, headers map[string]string, queryKey string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if queryKey != "" {
		q := req.URL.Query()
		q.Set("key", queryKey)
		req.URL.RawQuery = q.Encode()
	}
	return req
}

func TestExtractProxyToken_AuthorizationBearer(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"Authorization": "Bearer sk-managed-key-123",
	}, "")
	if got := extractProxyToken(req); got != "sk-managed-key-123" {
		t.Errorf("expected sk-managed-key-123, got %q", got)
	}
}

func TestExtractProxyToken_AuthorizationBearerLowercase(t *testing.T) {
	// Proxy auth uses case-INSENSITIVE regex for "Bearer "
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"Authorization": "bearer sk-managed-key-123",
	}, "")
	if got := extractProxyToken(req); got != "sk-managed-key-123" {
		t.Errorf("expected sk-managed-key-123 from lowercase bearer, got %q", got)
	}
}

func TestExtractProxyToken_AuthorizationBearerMixedCase(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"Authorization": "BEARER sk-managed-key-123",
	}, "")
	if got := extractProxyToken(req); got != "sk-managed-key-123" {
		t.Errorf("expected sk-managed-key-123 from uppercase BEARER, got %q", got)
	}
}

func TestExtractProxyToken_AuthorizationBearerExtraWhitespace(t *testing.T) {
	// Regex /^Bearer\s+/i matches any whitespace after "Bearer"
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"Authorization": "Bearer    sk-managed-key-123",
	}, "")
	if got := extractProxyToken(req); got != "sk-managed-key-123" {
		t.Errorf("expected sk-managed-key-123 with extra whitespace, got %q", got)
	}
}

func TestExtractProxyToken_AuthorizationBearerTrim(t *testing.T) {
	// Token should be trimmed after Bearer prefix removal
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"Authorization": "Bearer   sk-managed-key-123   ",
	}, "")
	if got := extractProxyToken(req); got != "sk-managed-key-123" {
		t.Errorf("expected sk-managed-key-123 (trimmed), got %q", got)
	}
}

func TestExtractProxyToken_AuthorizationExclusive(t *testing.T) {
	// If Authorization is present, it is the EXCLUSIVE source — even if Bearer
	// token ends up empty, NO fallback to x-api-key or other headers.
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"Authorization": "Bearer ",
		"x-api-key":     "sk-valid-key",
	}, "")
	if got := extractProxyToken(req); got != "" {
		t.Errorf("expected empty (exclusive Authorization), got %q", got)
	}
}

func TestExtractProxyToken_AuthorizationBearerOnly(t *testing.T) {
	// "Bearer " with nothing after it (just whitespace)
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"Authorization": "Bearer ",
	}, "")
	if got := extractProxyToken(req); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractProxyToken_AuthorizationNoBearerPrefix(t *testing.T) {
	// Authorization without "Bearer " prefix — the regex won't match, so the
	// whole value is treated as token (since regex replaces nothing).
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"Authorization": "sk-direct-key",
	}, "")
	if got := extractProxyToken(req); got != "sk-direct-key" {
		t.Errorf("expected sk-direct-key (no Bearer prefix), got %q", got)
	}
}

func TestExtractProxyToken_XApiKey(t *testing.T) {
	// x-api-key header when Authorization is absent
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"x-api-key": "sk-api-key-123",
	}, "")
	if got := extractProxyToken(req); got != "sk-api-key-123" {
		t.Errorf("expected sk-api-key-123 from x-api-key, got %q", got)
	}
}

func TestExtractProxyToken_XApiKeyTrimmed(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"x-api-key": "  sk-api-key-123  ",
	}, "")
	if got := extractProxyToken(req); got != "sk-api-key-123" {
		t.Errorf("expected sk-api-key-123 (trimmed), got %q", got)
	}
}

func TestExtractProxyToken_XApiKeyEmpty(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"x-api-key": "   ",
	}, "")
	if got := extractProxyToken(req); got != "" {
		t.Errorf("expected empty (whitespace x-api-key), got %q", got)
	}
}

func TestExtractProxyToken_XGoogleApiKey(t *testing.T) {
	// x-goog-api-key header when Authorization and x-api-key are absent
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"x-goog-api-key": "sk-goog-key-123",
	}, "")
	if got := extractProxyToken(req); got != "sk-goog-key-123" {
		t.Errorf("expected sk-goog-key-123 from x-goog-api-key, got %q", got)
	}
}

func TestExtractProxyToken_XGoogleApiKeyTrimmed(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"x-goog-api-key": "  sk-goog-key-123  ",
	}, "")
	if got := extractProxyToken(req); got != "sk-goog-key-123" {
		t.Errorf("expected sk-goog-key-123 (trimmed), got %q", got)
	}
}

func TestExtractProxyToken_QueryKey(t *testing.T) {
	// ?key= query parameter when all headers are absent
	req := newProxyRequest("GET", "/v1/models", nil, "sk-query-key-123")
	if got := extractProxyToken(req); got != "sk-query-key-123" {
		t.Errorf("expected sk-query-key-123 from query, got %q", got)
	}
}

func TestExtractProxyToken_QueryKeyTrimmed(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", nil, "  sk-query-key-123  ")
	if got := extractProxyToken(req); got != "  sk-query-key-123  " {
		// URL query values preserve whitespace; trim is applied in extractProxyToken.
		// Actually the test expectation should match what's in the URL, then check trim.
		// The trim is applied when reading query value.
	}
	// The extractProxyToken trims the query value, so it should return trimmed
	if got := extractProxyToken(req); got != "sk-query-key-123" {
		t.Errorf("expected sk-query-key-123 (trimmed query), got %q", got)
	}
}

func TestExtractProxyToken_NoTokenSource(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", nil, "")
	if got := extractProxyToken(req); got != "" {
		t.Errorf("expected empty (no token source), got %q", got)
	}
}

func TestExtractProxyToken_XApiKeyPriorityOverXGoogleApiKey(t *testing.T) {
	// x-api-key takes priority over x-goog-api-key when Authorization is absent
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"x-api-key":      "sk-api-key-first",
		"x-goog-api-key": "sk-goog-key-second",
	}, "")
	if got := extractProxyToken(req); got != "sk-api-key-first" {
		t.Errorf("expected x-api-key value, got %q", got)
	}
}

func TestExtractProxyToken_XApiKeyPriorityOverQuery(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"x-api-key": "sk-api-key-first",
	}, "sk-query-key-second")
	if got := extractProxyToken(req); got != "sk-api-key-first" {
		t.Errorf("expected x-api-key value, got %q", got)
	}
}

func TestExtractProxyToken_XGoogleApiKeyPriorityOverQuery(t *testing.T) {
	req := newProxyRequest("GET", "/v1/models", map[string]string{
		"x-goog-api-key": "sk-goog-key-first",
	}, "sk-query-key-second")
	if got := extractProxyToken(req); got != "sk-goog-key-first" {
		t.Errorf("expected x-goog-api-key value, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// AuthorizeDownstreamToken tests — global token and error cases (no DB)
// ---------------------------------------------------------------------------

// proxyCfg returns a basic config for proxy auth tests.
func proxyCfg(proxyToken string) *config.Config {
	return &config.Config{
		ProxyToken: proxyToken,
	}
}

func TestAuthorizeDownstreamToken_EmptyToken(t *testing.T) {
	result := AuthorizeDownstreamToken("", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for empty token")
	}
	if result.StatusCode != 401 {
		t.Errorf("expected 401, got %d", result.StatusCode)
	}
	if result.Reason != "missing" {
		t.Errorf("expected reason=missing, got %q", result.Reason)
	}
}

func TestAuthorizeDownstreamToken_WhitespaceOnlyToken(t *testing.T) {
	result := AuthorizeDownstreamToken("   ", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for whitespace-only token")
	}
	if result.StatusCode != 401 {
		t.Errorf("expected 401, got %d", result.StatusCode)
	}
}

func TestAuthorizeDownstreamToken_GlobalTokenMatch(t *testing.T) {
	// Requires DB to be initialized so getManagedKeyByToken doesn't error.
	// Test only the global token path — the DB is empty, so managed key lookup
	// returns nil (not found), and we fall through to global token check.
	setupTestDB(t)
	result := AuthorizeDownstreamToken("global-secret", proxyCfg("global-secret"))
	if !result.OK {
		t.Fatalf("expected OK=true, got error: %s (code=%d)", result.Error, result.StatusCode)
	}
	if result.Source != "global" {
		t.Errorf("expected source=global, got %q", result.Source)
	}
	if result.Key != nil {
		t.Errorf("expected Key=nil for global token, got %+v", result.Key)
	}
	if result.Policy.DenyAllWhenEmpty {
		t.Error("expected DenyAllWhenEmpty=false for global token policy")
	}
}

func TestAuthorizeDownstreamToken_UnknownToken(t *testing.T) {
	// Unknown token that is neither a managed key nor the global proxy token
	setupTestDB(t)
	result := AuthorizeDownstreamToken("unknown-key-12345", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for unknown token")
	}
	if result.StatusCode != 403 {
		t.Errorf("expected 403, got %d", result.StatusCode)
	}
	if result.Reason != "invalid" {
		t.Errorf("expected reason=invalid, got %q", result.Reason)
	}
	if !strings.Contains(result.Error, "Invalid API key") {
		t.Errorf("expected 'Invalid API key' error, got %q", result.Error)
	}
}

// ---------------------------------------------------------------------------
// AuthorizeDownstreamToken DB-backed tests — managed key scenarios.
// These insert rows into downstream_api_keys and test the full auth flow.
// ---------------------------------------------------------------------------

// insertTestKey inserts a downstream_api_key row and returns the ID.
func insertTestKey(t *testing.T, key string, enabled bool, maxCost *float64, usedCost float64, maxRequests *int64, usedRequests int64, expiresAt *string) int64 {
	t.Helper()
	db := testDB(t)

	now := "2026-07-04T00:00:00Z"
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, expires_at, max_cost, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"test-key-"+key, key, enabled, expiresAt, maxCost, usedCost, maxRequests, usedRequests,
		now, now,
	)
	if err != nil {
		t.Fatalf("insert test key: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestAuthorizeDownstreamToken_ManagedKeyMatch(t *testing.T) {
	setupTestDB(t)
	id := insertTestKey(t, "sk-managed-valid", true, nil, 0, nil, 0, nil)

	result := AuthorizeDownstreamToken("sk-managed-valid", proxyCfg("global-secret"))
	if !result.OK {
		t.Fatalf("expected OK=true, got error: %s (code=%d)", result.Error, result.StatusCode)
	}
	if result.Source != "managed" {
		t.Errorf("expected source=managed, got %q", result.Source)
	}
	if result.Key == nil {
		t.Fatal("expected Key to be non-nil for managed key")
	}
	if result.Key.ID != id {
		t.Errorf("expected key ID=%d, got %d", id, result.Key.ID)
	}
	// Managed keys have DenyAllWhenEmpty=true
	if !result.Policy.DenyAllWhenEmpty {
		t.Error("expected DenyAllWhenEmpty=true for managed key policy")
	}
}

func TestAuthorizeDownstreamToken_ManagedKeyDisabled(t *testing.T) {
	setupTestDB(t)
	insertTestKey(t, "sk-managed-disabled", false, nil, 0, nil, 0, nil)

	result := AuthorizeDownstreamToken("sk-managed-disabled", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for disabled key")
	}
	if result.StatusCode != 403 {
		t.Errorf("expected 403, got %d", result.StatusCode)
	}
	if result.Reason != "disabled" {
		t.Errorf("expected reason=disabled, got %q", result.Reason)
	}
	if !strings.Contains(result.Error, "disabled") {
		t.Errorf("expected 'disabled' error, got %q", result.Error)
	}
}

func TestAuthorizeDownstreamToken_ManagedKeyExpired(t *testing.T) {
	setupTestDB(t)
	// Set expires_at to a past date
	expiredAt := "2020-01-01T00:00:00Z"
	insertTestKey(t, "sk-managed-expired", true, nil, 0, nil, 0, &expiredAt)

	result := AuthorizeDownstreamToken("sk-managed-expired", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for expired key")
	}
	if result.StatusCode != 403 {
		t.Errorf("expected 403, got %d", result.StatusCode)
	}
	if result.Reason != "expired" {
		t.Errorf("expected reason=expired, got %q", result.Reason)
	}
	if !strings.Contains(result.Error, "expired") {
		t.Errorf("expected 'expired' error, got %q", result.Error)
	}
}

func TestAuthorizeDownstreamToken_ManagedKeyNotExpired(t *testing.T) {
	setupTestDB(t)
	// Set expires_at to a future date
	futureExpiry := "2099-12-31T23:59:59Z"
	insertTestKey(t, "sk-managed-future", true, nil, 0, nil, 0, &futureExpiry)

	result := AuthorizeDownstreamToken("sk-managed-future", proxyCfg("global-secret"))
	if !result.OK {
		t.Fatalf("expected OK=true for non-expired key, got error: %s (code=%d)", result.Error, result.StatusCode)
	}
	if result.Source != "managed" {
		t.Errorf("expected source=managed, got %q", result.Source)
	}
}

func TestAuthorizeDownstreamToken_ManagedKeyOverCost(t *testing.T) {
	setupTestDB(t)
	maxCost := 10.0
	usedCost := 10.0 // usedCost >= maxCost → over_cost
	insertTestKey(t, "sk-managed-overcost", true, &maxCost, usedCost, nil, 0, nil)

	result := AuthorizeDownstreamToken("sk-managed-overcost", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for over-cost key")
	}
	if result.StatusCode != 403 {
		t.Errorf("expected 403, got %d", result.StatusCode)
	}
	if result.Reason != "over_cost" {
		t.Errorf("expected reason=over_cost, got %q", result.Reason)
	}
	if !strings.Contains(result.Error, "max cost") {
		t.Errorf("expected 'max cost' error, got %q", result.Error)
	}
}

func TestAuthorizeDownstreamToken_ManagedKeyMaxCostZero_ImmediateBlock(t *testing.T) {
	setupTestDB(t)
	// max_cost=0 with used_cost=0 → usedCost (0) >= maxCost (0) → blocked immediately
	maxCost := 0.0
	insertTestKey(t, "sk-managed-costzero", true, &maxCost, 0, nil, 0, nil)

	result := AuthorizeDownstreamToken("sk-managed-costzero", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for max_cost=0 key")
	}
	if result.Reason != "over_cost" {
		t.Errorf("expected reason=over_cost, got %q", result.Reason)
	}
}

func TestAuthorizeDownstreamToken_ManagedKeyOverRequests(t *testing.T) {
	setupTestDB(t)
	maxRequests := int64(100)
	usedRequests := int64(100) // usedRequests >= maxRequests → over_requests
	insertTestKey(t, "sk-managed-overreq", true, nil, 0, &maxRequests, usedRequests, nil)

	result := AuthorizeDownstreamToken("sk-managed-overreq", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for over-requests key")
	}
	if result.StatusCode != 403 {
		t.Errorf("expected 403, got %d", result.StatusCode)
	}
	if result.Reason != "over_requests" {
		t.Errorf("expected reason=over_requests, got %q", result.Reason)
	}
	if !strings.Contains(result.Error, "max requests") {
		t.Errorf("expected 'max requests' error, got %q", result.Error)
	}
}

func TestAuthorizeDownstreamToken_ManagedKeyMaxRequestsZero_ImmediateBlock(t *testing.T) {
	setupTestDB(t)
	// max_requests=0 with used_requests=0 → usedRequests (0) >= maxRequests (0) → blocked immediately
	maxRequests := int64(0)
	insertTestKey(t, "sk-managed-reqzero", true, nil, 0, &maxRequests, 0, nil)

	result := AuthorizeDownstreamToken("sk-managed-reqzero", proxyCfg("global-secret"))
	if result.OK {
		t.Error("expected OK=false for max_requests=0 key")
	}
	if result.Reason != "over_requests" {
		t.Errorf("expected reason=over_requests, got %q", result.Reason)
	}
}

func TestAuthorizeDownstreamToken_ManagedKeyUnderLimits(t *testing.T) {
	setupTestDB(t)
	// Under cost and request limits — should pass
	maxCost := 100.0
	usedCost := 50.0
	maxRequests := int64(1000)
	usedRequests := int64(500)
	insertTestKey(t, "sk-managed-under", true, &maxCost, usedCost, &maxRequests, usedRequests, nil)

	result := AuthorizeDownstreamToken("sk-managed-under", proxyCfg("global-secret"))
	if !result.OK {
		t.Fatalf("expected OK=true for under-limits key, got error: %s", result.Error)
	}
	if result.Source != "managed" {
		t.Errorf("expected source=managed, got %q", result.Source)
	}
}

func TestAuthorizeDownstreamToken_GlobalTokenTakesPriorityOverManaged(t *testing.T) {
	setupTestDB(t)
	// If a managed key has the SAME value as the global proxy token,
	// the managed key should be found first (it's checked before global fallback).
	insertTestKey(t, "shared-token", true, nil, 0, nil, 0, nil)

	result := AuthorizeDownstreamToken("shared-token", proxyCfg("shared-token"))
	if !result.OK {
		t.Fatalf("expected OK=true, got: %s", result.Error)
	}
	// Managed key is checked first, so it should win
	if result.Source != "managed" {
		t.Errorf("expected source=managed (managed key checked before global), got %q", result.Source)
	}
}

// ---------------------------------------------------------------------------
// ProxyAuth middleware integration tests
// ---------------------------------------------------------------------------

func proxyAuthMiddlewareHelper(t *testing.T, cfg *config.Config, headers map[string]string, queryKey string) *httptest.ResponseRecorder {
	t.Helper()
	middleware := ProxyAuth(cfg)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify proxy auth context is stored
		pac := GetProxyAuth(r.Context())
		if pac == nil {
			t.Error("expected ProxyAuthContext in handler context")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := newProxyRequest("POST", "/v1/chat/completions", headers, queryKey)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestProxyAuthMiddleware_GlobalToken(t *testing.T) {
	setupTestDB(t)
	cfg := proxyCfg("global-secret")
	w := proxyAuthMiddlewareHelper(t, cfg, map[string]string{
		"Authorization": "Bearer global-secret",
	}, "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for global token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProxyAuthMiddleware_UnknownToken(t *testing.T) {
	setupTestDB(t)
	cfg := proxyCfg("global-secret")
	w := proxyAuthMiddlewareHelper(t, cfg, map[string]string{
		"Authorization": "Bearer unknown-token",
	}, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for unknown token, got %d", w.Code)
	}
}

func TestProxyAuthMiddleware_EmptyToken_401(t *testing.T) {
	// No Authorization, no other token source → empty token → 401
	// Does NOT need DB — returns early before AuthorizeDownstreamToken.
	cfg := proxyCfg("global-secret")
	w := proxyAuthMiddlewareHelper(t, cfg, nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty token, got %d", w.Code)
	}
}

func TestProxyAuthMiddleware_XApiKey(t *testing.T) {
	setupTestDB(t)
	cfg := proxyCfg("global-secret")
	w := proxyAuthMiddlewareHelper(t, cfg, map[string]string{
		"x-api-key": "global-secret",
	}, "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for x-api-key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProxyAuthMiddleware_AuthorizationExclusive_EmptyToken(t *testing.T) {
	// Authorization: Bearer (empty) + x-api-key: valid → Authorization is exclusive,
	// token is empty → 401, does NOT fall back to x-api-key.
	// Does NOT need DB — returns early before AuthorizeDownstreamToken.
	cfg := proxyCfg("global-secret")
	w := proxyAuthMiddlewareHelper(t, cfg, map[string]string{
		"Authorization": "Bearer ",
		"x-api-key":     "global-secret",
	}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (Authorization exclusive, empty token), got %d", w.Code)
	}
}

func TestProxyAuthMiddleware_ManagedKey(t *testing.T) {
	setupTestDB(t)
	insertTestKey(t, "sk-managed-mw", true, nil, 0, nil, 0, nil)

	cfg := proxyCfg("global-secret")
	w := proxyAuthMiddlewareHelper(t, cfg, map[string]string{
		"Authorization": "Bearer sk-managed-mw",
	}, "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for managed key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProxyAuthMiddleware_ManagedKeyDisabled_403(t *testing.T) {
	setupTestDB(t)
	insertTestKey(t, "sk-managed-mw-disabled", false, nil, 0, nil, 0, nil)

	cfg := proxyCfg("global-secret")
	w := proxyAuthMiddlewareHelper(t, cfg, map[string]string{
		"Authorization": "Bearer sk-managed-mw-disabled",
	}, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for disabled managed key, got %d", w.Code)
	}
}

func TestProxyAuthMiddleware_ResponseContentType(t *testing.T) {
	// Does NOT need DB — empty token returns early.
	cfg := proxyCfg("global-secret")
	w := proxyAuthMiddlewareHelper(t, cfg, nil, "")
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}
}
