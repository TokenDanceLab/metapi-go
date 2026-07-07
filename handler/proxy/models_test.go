package proxyhandler

import (
	"net/http/httptest"
	"testing"

	"github.com/tokendancelab/metapi-go/auth"
)

// ---- matchModelPattern ----

func TestMatchModelPattern(t *testing.T) {
	tests := []struct {
		model   string
		pattern string
		want    bool
	}{
		// Exact match
		{"gpt-4o", "gpt-4o", true},
		{"gpt-4o", "gpt-3.5", false},
		// Wildcard
		{"gpt-4o", "*", true},
		// Prefix wildcard
		{"gpt-4o", "gpt-*", true},
		{"claude-sonnet", "gpt-*", false},
		{"gpt-4o-2024", "gpt-4o*", true},
		// Suffix wildcard
		{"gpt-4o", "*-4o", true},
		{"gpt-3.5", "*-4o", false},
		// Empty
		{"", "", true},
		{"gpt-4o", "", false},
	}
	for _, tt := range tests {
		got := matchModelPattern(tt.model, tt.pattern)
		if got != tt.want {
			t.Errorf("matchModelPattern(%q, %q) = %v, want %v", tt.model, tt.pattern, got, tt.want)
		}
	}
}

// ---- IsModelAllowedByPolicy ----

func TestIsModelAllowedByPolicy(t *testing.T) {
	// Empty policy: DenyAllWhenEmpty=false -> allow all
	policy := auth.EmptyDownstreamRoutingPolicy
	if !IsModelAllowedByPolicy("gpt-4o", policy) {
		t.Error("empty policy should allow all by default")
	}

	// Deny when empty
	denyPolicy := auth.DownstreamRoutingPolicy{
		DenyAllWhenEmpty: true,
	}
	if IsModelAllowedByPolicy("gpt-4o", denyPolicy) {
		t.Error("deny-all-empty policy should reject all")
	}

	// Supported models
	supportPolicy := auth.DownstreamRoutingPolicy{
		SupportedModels: []string{"gpt-4o", "claude-*"},
	}
	if !IsModelAllowedByPolicy("gpt-4o", supportPolicy) {
		t.Error("gpt-4o should be allowed")
	}
	if !IsModelAllowedByPolicy("claude-sonnet-4-20250514", supportPolicy) {
		t.Error("claude-sonnet-4 should match claude-*")
	}
	if IsModelAllowedByPolicy("gemini-pro", supportPolicy) {
		t.Error("gemini-pro should not match")
	}

	// AllowedRouteIDs (stub — always true if no supported models)
	routePolicy := auth.DownstreamRoutingPolicy{
		AllowedRouteIDs: []int64{1, 2, 3},
	}
	if !IsModelAllowedByPolicy("any-model", routePolicy) {
		t.Error("AllowedRouteIDs policy should allow all")
	}
}

// ---- buildOpenAIModelsResponse ----

func TestBuildOpenAIModelsResponse(t *testing.T) {
	models := []string{"gpt-4o", "gpt-3.5-turbo"}
	resp := buildOpenAIModelsResponse(models)

	if resp["object"] != "list" {
		t.Errorf("object = %v", resp["object"])
	}
	data, _ := resp["data"].([]map[string]any)
	if len(data) != 2 {
		t.Errorf("expected 2 models, got %d", len(data))
	}
	if data[0]["id"] != "gpt-4o" {
		t.Errorf("first model = %v", data[0]["id"])
	}
	if data[0]["object"] != "model" {
		t.Errorf("object = %v", data[0]["object"])
	}
	if data[0]["owned_by"] != "metapi" {
		t.Errorf("owned_by = %v", data[0]["owned_by"])
	}
	if _, ok := data[0]["created"]; !ok {
		t.Error("missing created field")
	}
}

func TestBuildOpenAIModelsResponse_Empty(t *testing.T) {
	resp := buildOpenAIModelsResponse([]string{})
	data, _ := resp["data"].([]map[string]any)
	if len(data) != 0 {
		t.Errorf("expected 0 models, got %d", len(data))
	}
}

// ---- buildClaudeModelsResponse ----

func TestBuildClaudeModelsResponse(t *testing.T) {
	models := []string{"claude-sonnet-4-20250514"}
	resp := buildClaudeModelsResponse(models)

	data, _ := resp["data"].([]map[string]any)
	if len(data) != 1 {
		t.Errorf("expected 1 model, got %d", len(data))
	}
	if data[0]["id"] != "claude-sonnet-4-20250514" {
		t.Errorf("id = %v", data[0]["id"])
	}
	if data[0]["display_name"] != "claude-sonnet-4-20250514" {
		t.Errorf("display_name = %v", data[0]["display_name"])
	}
	if data[0]["type"] != "model" {
		t.Errorf("type = %v", data[0]["type"])
	}
}

// ---- getAvailableModels ----

func TestGetAvailableModels(t *testing.T) {
	models := getAvailableModels(auth.EmptyDownstreamRoutingPolicy)
	if len(models) == 0 {
		t.Error("expected non-empty model list")
	}
	// Check known models
	known := map[string]bool{}
	for _, m := range models {
		known[m] = true
	}
	if !known["gpt-4o"] {
		t.Error("missing gpt-4o")
	}
	if !known["claude-sonnet-4-20250514"] {
		t.Error("missing claude-sonnet-4-20250514")
	}
	if !known["gemini-2.5-pro"] {
		t.Error("missing gemini-2.5-pro")
	}
}

func TestGetAvailableModelsAppliesManagedPolicy(t *testing.T) {
	denyAll := auth.DownstreamRoutingPolicy{DenyAllWhenEmpty: true}
	if got := getAvailableModels(denyAll); len(got) != 0 {
		t.Fatalf("deny-all policy returned %v, want empty", got)
	}

	supported := auth.DownstreamRoutingPolicy{
		SupportedModels:  []string{"gpt-4o"},
		DenyAllWhenEmpty: true,
	}
	got := getAvailableModels(supported)
	if len(got) != 1 || got[0] != "gpt-4o" {
		t.Fatalf("supported policy returned %v, want [gpt-4o]", got)
	}

	wildcard := auth.DownstreamRoutingPolicy{
		SupportedModels:  []string{"claude-*"},
		DenyAllWhenEmpty: true,
	}
	got = getAvailableModels(wildcard)
	if len(got) == 0 {
		t.Fatal("wildcard supported policy returned empty, want Claude models")
	}
	for _, model := range got {
		if !matchModelPattern(model, "claude-*") {
			t.Fatalf("wildcard supported policy returned non-Claude model %q in %v", model, got)
		}
	}

	routeOnly := auth.DownstreamRoutingPolicy{
		AllowedRouteIDs:  []int64{1},
		DenyAllWhenEmpty: true,
	}
	if got := getAvailableModels(routeOnly); len(got) != 0 {
		t.Fatalf("route-only policy returned %v, want empty until route-aware model listing is available", got)
	}
}

// ---- HandleModels ----

func TestHandleModels_OpenAIFormat(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req = auth.ProxyAuthFromRequest(req, &auth.ProxyAuthContext{
		Token:  "test",
		Source: "global",
		Policy: auth.EmptyDownstreamRoutingPolicy,
	})
	rec := httptest.NewRecorder()
	HandleModels(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	if m["object"] != "list" {
		t.Errorf("object = %v, want list", m["object"])
	}
	data, _ := m["data"].([]any)
	if len(data) == 0 {
		t.Error("expected non-empty model list")
	}
}

func TestHandleModels_ClaudeFormat(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("anthropic-version", "2023-06-01")
	req = auth.ProxyAuthFromRequest(req, &auth.ProxyAuthContext{
		Token:  "test",
		Source: "global",
		Policy: auth.EmptyDownstreamRoutingPolicy,
	})
	rec := httptest.NewRecorder()
	HandleModels(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	data, _ := m["data"].([]any)
	if len(data) == 0 {
		t.Error("expected non-empty model list")
	}
	// Claude format: should have data array, no "object" field with "list"
	first := data[0].(map[string]any)
	if first["type"] != "model" {
		t.Errorf("Claude format missing 'type: model': %v", first)
	}
}

func TestHandleModels_XApiKeyFormat(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", "test-key")
	req = auth.ProxyAuthFromRequest(req, &auth.ProxyAuthContext{
		Token:  "test",
		Source: "global",
		Policy: auth.EmptyDownstreamRoutingPolicy,
	})
	rec := httptest.NewRecorder()
	HandleModels(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// x-api-key should trigger Claude format
	m := unmarshalResponse(t, rec)
	if _, ok := m["data"]; !ok {
		t.Error("Claude format should have data field")
	}
}

func TestHandleModelsFiltersManagedKeyPolicy(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req = auth.ProxyAuthFromRequest(req, &auth.ProxyAuthContext{
		Token:  "managed-key",
		Source: "managed",
		Policy: auth.DownstreamRoutingPolicy{
			SupportedModels:  []string{"gpt-4o"},
			DenyAllWhenEmpty: true,
		},
	})
	rec := httptest.NewRecorder()
	HandleModels(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	data, _ := m["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 model, got %d: %#v", len(data), data)
	}
	first := data[0].(map[string]any)
	if first["id"] != "gpt-4o" {
		t.Fatalf("model id = %v, want gpt-4o", first["id"])
	}
}

func TestHandleModelsDenyAllManagedPolicyReturnsEmptyList(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req = auth.ProxyAuthFromRequest(req, &auth.ProxyAuthContext{
		Token:  "managed-key",
		Source: "managed",
		Policy: auth.DownstreamRoutingPolicy{
			DenyAllWhenEmpty: true,
		},
	})
	rec := httptest.NewRecorder()
	HandleModels(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	data, _ := m["data"].([]any)
	if len(data) != 0 {
		t.Fatalf("expected empty model list, got %#v", data)
	}
}

func TestHandleModels_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	HandleModels(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
