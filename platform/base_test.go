package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// --- BaseAdapter defaults ---

func TestBaseAdapterDefaults(t *testing.T) {
	b := NewBaseAdapter("test-platform")

	if b.PlatformName() != "test-platform" {
		t.Errorf("expected PlatformName=%q, got %q", "test-platform", b.PlatformName())
	}

	ctx := context.Background()

	// Detect should return error
	ok, err := b.Detect(ctx, "http://example.com")
	if ok != false || err == nil {
		t.Error("BaseAdapter.Detect should return false and error")
	}

	// Checkin should return unsupported
	cr, err := b.Checkin(ctx, "http://example.com", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cr.Success {
		t.Error("BaseAdapter.Checkin should return Success=false")
	}

	// GetBalance should return zeros
	bi, err := b.GetBalance(ctx, "http://example.com", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if bi.Balance != 0 || bi.Used != 0 || bi.Quota != 0 {
		t.Error("BaseAdapter.GetBalance should return all zeros")
	}

	// GetModels should return empty
	models, err := b.GetModels(ctx, "http://example.com", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Error("BaseAdapter.GetModels should return empty slice")
	}

	// GetAPIToken should return nil
	tok, err := b.GetAPIToken(ctx, "http://example.com", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tok != nil {
		t.Error("BaseAdapter.GetAPIToken should return nil")
	}

	// CreateAPIToken should return false
	created, err := b.CreateAPIToken(ctx, "http://example.com", "token", nil, nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if created {
		t.Error("BaseAdapter.CreateAPIToken should return false")
	}

	// DeleteAPIToken should return nil
	if err := b.DeleteAPIToken(ctx, "http://example.com", "token", "key", nil, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// GetSiteAnnouncements should return empty
	anns, err := b.GetSiteAnnouncements(ctx, "http://example.com", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(anns) != 0 {
		t.Error("BaseAdapter.GetSiteAnnouncements should return empty slice")
	}

	// GetUserGroups should return ["default"]
	groups, err := b.GetUserGroups(ctx, "http://example.com", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(groups) != 1 || groups[0] != "default" {
		t.Errorf("BaseAdapter.GetUserGroups should return ['default'], got %v", groups)
	}

	// GetUserInfo should return nil (default falls through to fetch failure, returns nil)
	ui, err := b.GetUserInfo(ctx, "http://example.com", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Base GetUserInfo fetches /api/user/self, which will fail on example.com, returning nil
	if ui != nil {
		t.Error("BaseAdapter.GetUserInfo should return nil for unreachable URL")
	}

	// VerifyToken should return TokenType="unknown" for unreachable URL
	tvr, err := b.VerifyToken(ctx, "http://example.com", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tvr.TokenType != "unknown" {
		t.Errorf("BaseAdapter.VerifyToken expected 'unknown', got %q", tvr.TokenType)
	}
}

func TestFetchJSONRejectsOversizedSuccessBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("x", platformJSONResponseBodyLimit) + `"}`))
	}))
	defer server.Close()

	_, err := fetchJSON(context.Background(), server.URL, http.MethodGet, nil, nil, nil)
	if err == nil {
		t.Fatal("expected oversized JSON response body to fail")
	}
	if !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("error = %q, want response body size failure", err)
	}
}

func TestFetchJSONRejectsOversizedErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("x", platformErrorResponseBodyLimit+1), http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := fetchJSON(context.Background(), server.URL, http.MethodGet, nil, nil, nil)
	if err == nil {
		t.Fatal("expected oversized error response body to fail")
	}
	if !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("error = %q, want response body size failure", err)
	}
}

func TestFetchTextRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(strings.Repeat("x", platformTextResponseBodyLimit+1)))
	}))
	defer server.Close()

	_, _, err := fetchText(context.Background(), server.URL, nil)
	if err == nil {
		t.Fatal("expected oversized text response body to fail")
	}
	if !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("error = %q, want response body size failure", err)
	}
}

// --- JSON helpers ---

func TestGetHelpers(t *testing.T) {
	m := map[string]interface{}{
		"str":  "hello",
		"num":  42.0,
		"bool": true,
		"obj":  map[string]interface{}{"key": "val"},
		"int":  json.Number("99"),
	}

	// getString
	if s, ok := getString(m, "str"); !ok || s != "hello" {
		t.Errorf("getString: %q, %v", s, ok)
	}
	if _, ok := getString(m, "missing"); ok {
		t.Error("getString should return ok=false for missing key")
	}

	// getBool
	if b, ok := getBool(m, "bool"); !ok || !b {
		t.Errorf("getBool: %v, %v", b, ok)
	}

	// getFloat
	if f, ok := getFloat(m, "num"); !ok || f != 42.0 {
		t.Errorf("getFloat: %f, %v", f, ok)
	}

	// getIntPtr
	if ip := getIntPtr(m, "num"); ip == nil || *ip != 42 {
		t.Errorf("getIntPtr from float64: %v", ip)
	}
	if ip := getIntPtr(m, "int"); ip == nil || *ip != 99 {
		t.Errorf("getIntPtr from json.Number: %v", ip)
	}
	if ip := getIntPtr(m, "missing"); ip != nil {
		t.Error("getIntPtr should return nil for missing key")
	}

	// getMap
	if mm, ok := getMap(m, "obj"); !ok || mm["key"] != "val" {
		t.Errorf("getMap: %v, %v", mm, ok)
	}
}

// --- Auth helpers ---

func TestAuthBearerHeaders(t *testing.T) {
	h := authBearerHeaders("my-token")
	if h["Authorization"] != "Bearer my-token" {
		t.Errorf("authBearerHeaders: %v", h)
	}

	h2 := authBearerHeaders("Bearer already-prefixed")
	if h2["Authorization"] != "Bearer already-prefixed" {
		t.Errorf("authBearerHeaders with prefix: %v", h2)
	}

	// Internal spaces are preserved (only leading/trailing trimmed)
	h3 := authBearerHeaders("  Bearer  spaces  ")
	if h3["Authorization"] != "Bearer  spaces" {
		t.Errorf("authBearerHeaders with spaces: %q", h3["Authorization"])
	}
}

func TestStripBearerPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Bearer abc", "abc"},
		{"  Bearer abc  ", "abc"},
		{"plain-token", "plain-token"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := stripBearerPrefix(tt.input); got != tt.expected {
			t.Errorf("stripBearerPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// --- Cookie helpers ---

func TestBuildCookieCandidates(t *testing.T) {
	// Plain token
	c := buildCookieCandidates("plain-token")
	if len(c) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(c), c)
	}
	if c[0] != "session=plain-token" {
		t.Errorf("first candidate: %q", c[0])
	}
	if c[1] != "token=plain-token" {
		t.Errorf("second candidate: %q", c[1])
	}

	// Token with Bearer prefix
	c2 := buildCookieCandidates("Bearer mytoken123")
	if c2[0] != "session=mytoken123" {
		t.Errorf("with Bearer prefix: %q", c2[0])
	}

	// Cookie string (contains =)
	c3 := buildCookieCandidates("session=abc123; token=xyz")
	if len(c3) != 3 { // original, session=, token=
		t.Fatalf("expected 3 candidates, got %d: %v", len(c3), c3)
	}

	// Empty
	c4 := buildCookieCandidates("")
	if len(c4) != 0 {
		t.Errorf("expected empty for blank token, got %v", c4)
	}
}

func TestMergeSetCookie(t *testing.T) {
	result := mergeSetCookie("", []string{"session=abc", "token=xyz"})
	// Order after merge may vary; check both keys are present
	if !containsCookie(result, "session", "abc") || !containsCookie(result, "token", "xyz") {
		t.Errorf("mergeSetCookie basic: %q", result)
	}

	// Update existing
	result2 := mergeSetCookie("session=old; other=data", []string{"session=new"})
	if !containsCookie(result2, "session", "new") || !containsCookie(result2, "other", "data") {
		t.Errorf("mergeSetCookie update: %q", result2)
	}

	// Empty input
	result3 := mergeSetCookie("", []string{""})
	if result3 != "" {
		t.Errorf("mergeSetCookie empty: %q", result3)
	}
}

func containsCookie(header, name, value string) bool {
	pairs := strings.Split(header, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		eq := strings.Index(p, "=")
		if eq < 0 {
			continue
		}
		if p[:eq] == name && p[eq+1:] == value {
			return true
		}
	}
	return false
}

func TestUpsertCookie(t *testing.T) {
	r := upsertCookie("a=1; b=2", "b", "3")
	if r != "a=1; b=3" {
		t.Errorf("upsertCookie replace: %q", r)
	}

	r2 := upsertCookie("a=1", "c", "2")
	if r2 != "a=1; c=2" {
		t.Errorf("upsertCookie add: %q", r2)
	}
}

func TestHasUsableSessionCookie(t *testing.T) {
	if hasUsableSessionCookie("") {
		t.Error("empty string should not have usable session cookie")
	}

	if !hasUsableSessionCookie("session=my-session-token") {
		t.Error("session cookie should be usable")
	}

	if !hasUsableSessionCookie("auth_token=xyz; other=val") {
		t.Error("auth_token should be usable")
	}

	if !hasUsableSessionCookie("token=abc") {
		t.Error("token cookie should be usable")
	}

	if !hasUsableSessionCookie("jwt_token=xyz") {
		t.Error("jwt_token should be usable")
	}

	// Shield cookies should be ignored
	if hasUsableSessionCookie("acw_sc__v2=xyz; acw_tc=abc") {
		t.Error("shield cookies only should not be usable")
	}
}

// --- Login helpers ---

func TestExtractLoginToken(t *testing.T) {
	// data as string: first candidate is data itself, not a string, then falls through to data["token"]
	data := map[string]interface{}{"token": "data-token"}
	tok := extractLoginToken(map[string]interface{}{}, data)
	if tok != "data-token" {
		t.Errorf("extractLoginToken from data.token: %q", tok)
	}

	// resp.token
	resp := map[string]interface{}{"token": "resp-token"}
	tok2 := extractLoginToken(resp, nil)
	if tok2 != "resp-token" {
		t.Errorf("extractLoginToken from resp.token: %q", tok2)
	}

	// resp.accessToken
	resp2 := map[string]interface{}{"accessToken": "access-token"}
	tok3 := extractLoginToken(resp2, nil)
	if tok3 != "access-token" {
		t.Errorf("extractLoginToken from resp.accessToken: %q", tok3)
	}

	// Neither data nor resp have tokens
	tok4 := extractLoginToken(map[string]interface{}{}, nil)
	if tok4 != "" {
		t.Errorf("extractLoginToken empty: %q", tok4)
	}
}

// --- Message helpers ---

func TestExtractResponseMessage(t *testing.T) {
	// message field
	msg := extractResponseMessage(map[string]interface{}{"message": "hello"})
	if msg != "hello" {
		t.Errorf("message: %q", msg)
	}

	// error.message
	msg2 := extractResponseMessage(map[string]interface{}{
		"error": map[string]interface{}{"message": "err msg"},
	})
	if msg2 != "err msg" {
		t.Errorf("error.message: %q", msg2)
	}

	// msg field
	msg3 := extractResponseMessage(map[string]interface{}{"msg": "short"})
	if msg3 != "short" {
		t.Errorf("msg: %q", msg3)
	}

	// empty
	msg4 := extractResponseMessage(map[string]interface{}{})
	if msg4 != "" {
		t.Errorf("empty: %q", msg4)
	}
}

func TestExtractResponseMessageFromBytes(t *testing.T) {
	msg := extractResponseMessageFromBytes([]byte(`{"message":"hello world"}`))
	if msg != "hello world" {
		t.Errorf("extractResponseMessageFromBytes: %q", msg)
	}

	msg2 := extractResponseMessageFromBytes([]byte(`not json`))
	if msg2 != "" {
		t.Errorf("non-json should return empty: %q", msg2)
	}
}

// --- Notice helpers ---

func TestBuildNoticeSourceKey(t *testing.T) {
	key := buildNoticeSourceKey("test notice content")
	if key == "" || key[:7] != "notice:" {
		t.Errorf("buildNoticeSourceKey: %q", key)
	}

	// Same content = same key
	key2 := buildNoticeSourceKey("test notice content")
	if key != key2 {
		t.Error("same content should produce same key")
	}

	// Different content = different key
	key3 := buildNoticeSourceKey("different content")
	if key == key3 {
		t.Error("different content should produce different keys")
	}
}

// --- Token helpers ---

func TestNormalizeTokenKeyForCompare(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sk-abc123", "sk-abc123"},
		{"Bearer sk-abc123", "sk-abc123"},
		{"  Bearer  sk-abc123  ", "sk-abc123"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeTokenKeyForCompare(tt.input); got != tt.expected {
			t.Errorf("normalizeTokenKeyForCompare(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseTokenItemsFromMap(t *testing.T) {
	// data as array
	items := parseTokenItemsFromMap(map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"key": "sk-abc", "name": "t1"},
		},
	})
	if len(items) != 1 || items[0]["key"] != "sk-abc" {
		t.Errorf("parseTokenItemsFromMap data array: %v", items)
	}

	// data.items
	items2 := parseTokenItemsFromMap(map[string]interface{}{
		"data": map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"key": "sk-def"},
			},
		},
	})
	if len(items2) != 1 || items2[0]["key"] != "sk-def" {
		t.Errorf("parseTokenItemsFromMap data.items: %v", items2)
	}

	// resp.items (direct)
	items3 := parseTokenItemsFromMap(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"key": "sk-ghi"},
		},
	})
	if len(items3) != 1 || items3[0]["key"] != "sk-ghi" {
		t.Errorf("parseTokenItemsFromMap resp.items: %v", items3)
	}

	// Empty
	items4 := parseTokenItemsFromMap(map[string]interface{}{})
	if len(items4) != 0 {
		t.Errorf("parseTokenItemsFromMap empty: %v", items4)
	}

	// data.list
	items5 := parseTokenItemsFromMap(map[string]interface{}{
		"data": map[string]interface{}{
			"list": []interface{}{
				map[string]interface{}{"key": "sk-jkl"},
			},
		},
	})
	if len(items5) != 1 || items5[0]["key"] != "sk-jkl" {
		t.Errorf("parseTokenItemsFromMap data.list: %v", items5)
	}
}

func TestNormalizeTokenItems(t *testing.T) {
	items := []map[string]interface{}{
		{"key": "sk-1", "name": "token1", "group": "vip", "status": float64(1)},
		{"key": "sk-2", "name": "", "token_group": "free", "status": float64(0)},
		{"key": "", "name": "empty-key"},
	}
	result := normalizeTokenItems(items)
	if len(result) != 2 {
		t.Fatalf("expected 2 items (empty key filtered), got %d", len(result))
	}
	if result[0].Name != "token1" || result[0].Key != "sk-1" {
		t.Errorf("first token: %+v", result[0])
	}
	if result[0].TokenGroup != "vip" {
		t.Errorf("first token group: %q", result[0].TokenGroup)
	}
	if result[0].Enabled != true {
		t.Error("first token should be enabled (status=1)")
	}
	if result[1].Enabled {
		t.Error("second token should be disabled (status=0)")
	}
	if result[1].Name != "token-2" {
		t.Errorf("second token auto-name: %q", result[1].Name)
	}
}

func TestFindFirstEnabledToken(t *testing.T) {
	tokens := []ApiTokenInfo{
		{Name: "t1", Key: "sk-disabled", Enabled: false},
		{Name: "t2", Key: "sk-enabled", Enabled: true},
		{Name: "t3", Key: "sk-also", Enabled: true},
	}
	tok := findFirstEnabledToken(tokens)
	if tok == nil || *tok != "sk-enabled" {
		t.Errorf("findFirstEnabledToken: %v", tok)
	}

	// All disabled: returns first token
	tokens2 := []ApiTokenInfo{
		{Name: "t1", Key: "sk-first", Enabled: false},
	}
	tok2 := findFirstEnabledToken(tokens2)
	if tok2 == nil || *tok2 != "sk-first" {
		t.Errorf("findFirstEnabledToken all disabled: %v", tok2)
	}

	// Empty
	tok3 := findFirstEnabledToken([]ApiTokenInfo{})
	if tok3 != nil {
		t.Error("findFirstEnabledToken empty should be nil")
	}
}

func TestPickTokenID(t *testing.T) {
	items := []map[string]interface{}{
		{"key": "sk-abc", "id": float64(42)},
		{"key": "sk-def", "id": float64(99)},
	}

	id := pickTokenID(items, "sk-def")
	if id == nil || *id != 99 {
		t.Errorf("pickTokenID: %v", id)
	}

	id2 := pickTokenID(items, "sk-missing")
	if id2 != nil {
		t.Errorf("pickTokenID missing: %v", id2)
	}
}

// --- Unsupported method contract verification ---

func TestUnsupportedMethodContract(t *testing.T) {
	// Verify that CodexAdapter returns proper unsupported results for all methods
	codex := &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}
	ctx := context.Background()

	// Login
	lr, err := codex.Login(ctx, "http://x", "u", "p", nil, nil)
	if err != nil {
		t.Errorf("Login should not return error: %v", err)
	}
	if lr.Success {
		t.Error("Codex Login should return Success=false")
	}

	// Checkin
	cr, err := codex.Checkin(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("Checkin should not return error: %v", err)
	}
	if cr.Success {
		t.Error("Codex Checkin should return Success=false")
	}

	// GetBalance
	bi, err := codex.GetBalance(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("GetBalance should not return error: %v", err)
	}
	if bi.Balance != 0 || bi.Used != 0 || bi.Quota != 0 {
		t.Error("Codex GetBalance should return all zeros")
	}

	// GetModels
	models, err := codex.GetModels(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("GetModels should not return error: %v", err)
	}
	if len(models) != 0 {
		t.Error("Codex GetModels should return empty slice")
	}

	// GetAPIToken
	tok, err := codex.GetAPIToken(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("GetAPIToken should not return error: %v", err)
	}
	if tok != nil {
		t.Error("Codex GetAPIToken should return nil")
	}

	// CreateAPIToken
	created, err := codex.CreateAPIToken(ctx, "http://x", "t", nil, nil, nil)
	if err != nil {
		t.Errorf("CreateAPIToken should not return error: %v", err)
	}
	if created {
		t.Error("Codex CreateAPIToken should return false")
	}

	// DeleteAPIToken
	if err := codex.DeleteAPIToken(ctx, "http://x", "t", "k", nil, nil); err != nil {
		t.Errorf("DeleteAPIToken should not return error: %v", err)
	}
}

// TestTokenItemGroupExtraction tests group field extraction from normalizeTokenItems.
func TestTokenItemGroupExtraction(t *testing.T) {
	// group_name field
	items := []map[string]interface{}{
		{"key": "sk-1", "name": "t1", "group_name": "vip"},
	}
	result := normalizeTokenItems(items)
	if result[0].TokenGroup != "vip" {
		t.Errorf("group_name: %q", result[0].TokenGroup)
	}

	// token_group field
	items2 := []map[string]interface{}{
		{"key": "sk-2", "name": "t2", "token_group": "premium"},
	}
	result2 := normalizeTokenItems(items2)
	if result2[0].TokenGroup != "premium" {
		t.Errorf("token_group: %q", result2[0].TokenGroup)
	}

	// Default group
	items3 := []map[string]interface{}{
		{"key": "sk-3"},
	}
	result3 := normalizeTokenItems(items3)
	if result3[0].TokenGroup != "" {
		t.Errorf("default group should be empty: %q", result3[0].TokenGroup)
	}
}

// TestNewBaseAdapterHasName ensures NewBaseAdapter sets a name.
func TestNewBaseAdapterHasName(t *testing.T) {
	b := NewBaseAdapter("my-platform")
	if b.name != "my-platform" {
		t.Errorf("expected name='my-platform', got %q", b.name)
	}
}

// Verify that BaseAdapter is embedded into all relevant types.
func TestBaseAdapterEmbedding(t *testing.T) {
	// Check through registry that each adapter type embeds *BaseAdapter
	for _, a := range ListAdapters() {
		// Use reflection to check for BaseAdapter field
		v := reflect.ValueOf(a)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			continue
		}

		// All adapters should have an embed of *BaseAdapter or *StandardAdapter (which has *BaseAdapter)
		hasBase := false
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.Kind() == reflect.Ptr && !field.IsNil() {
				if _, ok := field.Interface().(*BaseAdapter); ok {
					hasBase = true
					break
				}
				if _, ok := field.Interface().(*StandardAdapter); ok {
					hasBase = true
					break
				}
				if _, ok := field.Interface().(*NewApiAdapter); ok {
					hasBase = true
					break
				}
				if _, ok := field.Interface().(*OneApiAdapter); ok {
					hasBase = true
					break
				}
				if _, ok := field.Interface().(*OneHubAdapter); ok {
					hasBase = true
					break
				}
				if _, ok := field.Interface().(*GeminiAdapter); ok {
					hasBase = true
					break
				}
			}
		}
		if !hasBase {
			t.Errorf("adapter %q does not embed *BaseAdapter", a.PlatformName())
		}
	}
}
