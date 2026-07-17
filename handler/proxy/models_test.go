package proxyhandler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
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
	fixed := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	resp := buildOpenAIModelsResponse(models, fixed)

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
	if data[0]["owned_by"] != modelsOwnedBy {
		t.Errorf("owned_by = %v, want %q", data[0]["owned_by"], modelsOwnedBy)
	}
	created, ok := data[0]["created"].(int64)
	if !ok {
		// JSON builder may keep int; accept both
		if createdInt, okInt := data[0]["created"].(int); okInt {
			created = int64(createdInt)
			ok = true
		}
	}
	if !ok {
		t.Fatalf("created type = %T, want int/int64", data[0]["created"])
	}
	if created != fixed.Unix() {
		t.Errorf("created = %d, want %d", created, fixed.Unix())
	}
	if data[0]["context_length"] != int64(128000) {
		t.Errorf("gpt-4o context_length = %v, want 128000", data[0]["context_length"])
	}
	if data[1]["context_length"] != int64(16385) {
		t.Errorf("gpt-3.5-turbo context_length = %v, want 16385", data[1]["context_length"])
	}
}

func TestBuildOpenAIModelsResponse_Empty(t *testing.T) {
	resp := buildOpenAIModelsResponse([]string{}, time.Unix(0, 0).UTC())
	if resp["object"] != "list" {
		t.Errorf("empty list must still set object=list, got %v", resp["object"])
	}
	data, _ := resp["data"].([]map[string]any)
	if len(data) != 0 {
		t.Errorf("expected 0 models, got %d", len(data))
	}
}

func TestBuildOpenAIModelsResponse_UnknownModelOmitsContextLength(t *testing.T) {
	resp := buildOpenAIModelsResponse([]string{"custom-vendor/my-model"}, time.Unix(1, 0).UTC())
	data, _ := resp["data"].([]map[string]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 model, got %d", len(data))
	}
	if _, ok := data[0]["context_length"]; ok {
		t.Errorf("unknown model should omit context_length, got %v", data[0]["context_length"])
	}
	if data[0]["owned_by"] != modelsOwnedBy {
		t.Errorf("owned_by = %v", data[0]["owned_by"])
	}
	if data[0]["object"] != "model" {
		t.Errorf("object = %v", data[0]["object"])
	}
}

// ---- buildClaudeModelsResponse ----

func TestBuildClaudeModelsResponse(t *testing.T) {
	models := []string{"claude-sonnet-4-20250514", "claude-3-5-sonnet-latest"}
	fixed := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	resp := buildClaudeModelsResponse(models, fixed)

	data, _ := resp["data"].([]map[string]any)
	if len(data) != 2 {
		t.Errorf("expected 2 models, got %d", len(data))
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
	if data[0]["created_at"] != "2026-03-19T00:00:00Z" {
		t.Errorf("created_at = %v, want 2026-03-19T00:00:00Z", data[0]["created_at"])
	}
	if resp["first_id"] != "claude-sonnet-4-20250514" {
		t.Errorf("first_id = %v", resp["first_id"])
	}
	if resp["last_id"] != "claude-3-5-sonnet-latest" {
		t.Errorf("last_id = %v", resp["last_id"])
	}
	if resp["has_more"] != false {
		t.Errorf("has_more = %v, want false", resp["has_more"])
	}
}

func TestBuildClaudeModelsResponse_Empty(t *testing.T) {
	resp := buildClaudeModelsResponse([]string{}, time.Unix(0, 0).UTC())
	data, _ := resp["data"].([]map[string]any)
	if len(data) != 0 {
		t.Fatalf("expected empty data, got %v", data)
	}
	if resp["first_id"] != nil {
		t.Errorf("first_id = %v, want nil", resp["first_id"])
	}
	if resp["last_id"] != nil {
		t.Errorf("last_id = %v, want nil", resp["last_id"])
	}
	if resp["has_more"] != false {
		t.Errorf("has_more = %v, want false", resp["has_more"])
	}
}

// ---- getAvailableModels ----

func TestGetAvailableModels(t *testing.T) {
	models := getAvailableModels(context.Background(), auth.EmptyDownstreamRoutingPolicy)
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
	if got := getAvailableModels(context.Background(), denyAll); len(got) != 0 {
		t.Fatalf("deny-all policy returned %v, want empty", got)
	}

	supported := auth.DownstreamRoutingPolicy{
		SupportedModels:  []string{"gpt-4o"},
		DenyAllWhenEmpty: true,
	}
	got := getAvailableModels(context.Background(), supported)
	if len(got) != 1 || got[0] != "gpt-4o" {
		t.Fatalf("supported policy returned %v, want [gpt-4o]", got)
	}

	wildcard := auth.DownstreamRoutingPolicy{
		SupportedModels:  []string{"claude-*"},
		DenyAllWhenEmpty: true,
	}
	got = getAvailableModels(context.Background(), wildcard)
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
	if got := getAvailableModels(context.Background(), routeOnly); len(got) != 0 {
		t.Fatalf("route-only policy returned %v, want empty until route-aware model listing is available", got)
	}
}

func TestKnownModelContextLength(t *testing.T) {
	if got, ok := knownModelContextLength("gpt-4o"); !ok || got != 128000 {
		t.Fatalf("gpt-4o = (%d,%v), want (128000,true)", got, ok)
	}
	if got, ok := knownModelContextLength("claude-sonnet-4-20250514"); !ok || got != 200000 {
		t.Fatalf("claude = (%d,%v), want (200000,true)", got, ok)
	}
	if got, ok := knownModelContextLength("gemini-2.5-flash"); !ok || got != 1048576 {
		t.Fatalf("gemini = (%d,%v), want (1048576,true)", got, ok)
	}
	if _, ok := knownModelContextLength("vendor/unknown-model"); ok {
		t.Fatal("unknown model should not report context_length")
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
		t.Fatal("expected non-empty model list")
	}
	first := data[0].(map[string]any)
	if first["id"] == nil || first["id"] == "" {
		t.Errorf("missing id: %v", first)
	}
	if first["object"] != "model" {
		t.Errorf("object = %v, want model", first["object"])
	}
	if first["owned_by"] != modelsOwnedBy {
		t.Errorf("owned_by = %v, want %q", first["owned_by"], modelsOwnedBy)
	}
	if first["created"] == nil {
		t.Error("missing created field")
	}
	// JSON numbers decode as float64
	if _, ok := first["created"].(float64); !ok {
		t.Errorf("created type = %T, want number", first["created"])
	}
	if first["context_length"] == nil {
		t.Errorf("expected context_length on owned catalog model: %v", first)
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
	if obj, ok := m["object"]; ok {
		t.Errorf("Claude format should not set object=list, got object=%v", obj)
	}
	first := data[0].(map[string]any)
	if first["type"] != "model" {
		t.Errorf("Claude format missing 'type: model': %v", first)
	}
	if first["created_at"] == nil || first["created_at"] == "" {
		t.Errorf("Claude format missing created_at: %v", first)
	}
	if m["has_more"] != false {
		t.Errorf("has_more = %v, want false", m["has_more"])
	}
	if m["first_id"] == nil {
		t.Error("missing first_id")
	}
	if m["last_id"] == nil {
		t.Error("missing last_id")
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
	if _, ok := m["has_more"]; !ok {
		t.Error("Claude format should include has_more pagination field")
	}
	if _, ok := m["object"]; ok {
		t.Error("x-api-key path should not return OpenAI object=list")
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
	if first["owned_by"] != modelsOwnedBy {
		t.Fatalf("owned_by = %v, want %q", first["owned_by"], modelsOwnedBy)
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
	if m["object"] != "list" {
		t.Errorf("object = %v, want list", m["object"])
	}
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

type fakeModelsRouter struct {
	proxy.TokenRouterInterface
	models []string
	err    error
}

func (f *fakeModelsRouter) GetAvailableModels(ctx context.Context) ([]string, error) {
	return f.models, f.err
}

func (f *fakeModelsRouter) SelectChannel(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (f *fakeModelsRouter) SelectNextChannel(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (f *fakeModelsRouter) SelectPreferredChannel(ctx context.Context, requestedModel string, preferredChannelID int64, policy routing.DownstreamRoutingPolicy, excludeChannelIDs []int64) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (f *fakeModelsRouter) RecordSuccess(ctx context.Context, channelID int64, latencyMs float64, cost float64, modelName *string, actualAccountID *int64) error {
	return nil
}
func (f *fakeModelsRouter) RecordFailure(ctx context.Context, channelID int64, failureCtx routing.SiteRuntimeFailureContext, actualAccountID *int64) error {
	return nil
}

func TestGetAvailableModels_RouterBacked(t *testing.T) {
	SetUpstreamConfig(&UpstreamConfig{Router: &fakeModelsRouter{models: []string{"alpha-model", "beta-model"}}})
	t.Cleanup(func() { SetUpstreamConfig(nil) })
	got := getAvailableModels(context.Background(), auth.EmptyDownstreamRoutingPolicy)
	if len(got) != 2 {
		t.Fatalf("got=%v", got)
	}
	policy := auth.DownstreamRoutingPolicy{SupportedModels: []string{"alpha*"}}
	got = getAvailableModels(context.Background(), policy)
	if len(got) != 1 || got[0] != "alpha-model" {
		t.Fatalf("filtered=%v", got)
	}
}
