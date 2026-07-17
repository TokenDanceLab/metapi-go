package proxyhandler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
)

// modelsTestRouter is a channel-selection router that implements
// AvailableModelsSource for /v1/models unit tests.
// Optional contextLengths implements AvailableModelContextLengthsSource (#327).
type modelsTestRouter struct {
	models         []string
	err            error
	contextLengths map[string]int64
	contextErr     error
}

func (r *modelsTestRouter) SelectChannel(context.Context, string, routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (r *modelsTestRouter) SelectNextChannel(context.Context, string, []int64, routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (r *modelsTestRouter) SelectPreferredChannel(context.Context, string, int64, routing.DownstreamRoutingPolicy, []int64) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (r *modelsTestRouter) RecordSuccess(context.Context, int64, float64, float64, *string, *int64) error {
	return nil
}
func (r *modelsTestRouter) RecordFailure(context.Context, int64, routing.SiteRuntimeFailureContext, *int64) error {
	return nil
}

func (r *modelsTestRouter) GetAvailableModels(context.Context) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make([]string, len(r.models))
	copy(out, r.models)
	return out, nil
}

func (r *modelsTestRouter) GetAvailableModelContextLengths(context.Context) (map[string]int64, error) {
	if r.contextErr != nil {
		return nil, r.contextErr
	}
	if r.contextLengths == nil {
		return map[string]int64{}, nil
	}
	out := make(map[string]int64, len(r.contextLengths))
	for k, v := range r.contextLengths {
		out[k] = v
	}
	return out, nil
}

// selectionOnlyRouter satisfies TokenRouterInterface without AvailableModelsSource.
type selectionOnlyRouter struct{}

func (r *selectionOnlyRouter) SelectChannel(context.Context, string, routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (r *selectionOnlyRouter) SelectNextChannel(context.Context, string, []int64, routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (r *selectionOnlyRouter) SelectPreferredChannel(context.Context, string, int64, routing.DownstreamRoutingPolicy, []int64) (*routing.SelectedChannel, error) {
	return nil, nil
}
func (r *selectionOnlyRouter) RecordSuccess(context.Context, int64, float64, float64, *string, *int64) error {
	return nil
}
func (r *selectionOnlyRouter) RecordFailure(context.Context, int64, routing.SiteRuntimeFailureContext, *int64) error {
	return nil
}

// Ensure compile-time interface compliance.
var (
	_ proxy.TokenRouterInterface            = (*modelsTestRouter)(nil)
	_ AvailableModelsSource                 = (*modelsTestRouter)(nil)
	_ AvailableModelContextLengthsSource    = (*modelsTestRouter)(nil)
	_ proxy.TokenRouterInterface            = (*selectionOnlyRouter)(nil)
)

func withModelsRouter(t *testing.T, router proxy.TokenRouterInterface) {
	t.Helper()
	prev := getUpstreamConfig()
	SetUpstreamConfig(&UpstreamConfig{Router: router})
	t.Cleanup(func() {
		SetUpstreamConfig(prev)
	})
}

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
	resp := buildOpenAIModelsResponse(models, fixed, nil)

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
	resp := buildOpenAIModelsResponse([]string{}, time.Unix(0, 0).UTC(), nil)
	if resp["object"] != "list" {
		t.Errorf("empty list must still set object=list, got %v", resp["object"])
	}
	data, _ := resp["data"].([]map[string]any)
	if len(data) != 0 {
		t.Errorf("expected 0 models, got %d", len(data))
	}
}

func TestBuildOpenAIModelsResponse_UnknownModelOmitsContextLength(t *testing.T) {
	resp := buildOpenAIModelsResponse([]string{"custom-vendor/my-model"}, time.Unix(1, 0).UTC(), nil)
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

// ---- route context_length override (#327) ----

func TestBuildOpenAIModelsResponse_RouteContextLengthOverridesHeuristic(t *testing.T) {
	// gpt-4o heuristic is 128000; route metadata wins when positive.
	resp := buildOpenAIModelsResponse(
		[]string{"gpt-4o", "custom-vendor/my-model"},
		time.Unix(2, 0).UTC(),
		map[string]int64{
			"gpt-4o":                64000,
			"custom-vendor/my-model": 32000,
		},
	)
	data, _ := resp["data"].([]map[string]any)
	if len(data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(data))
	}
	if data[0]["context_length"] != int64(64000) {
		t.Errorf("gpt-4o route override = %v, want 64000", data[0]["context_length"])
	}
	if data[1]["context_length"] != int64(32000) {
		t.Errorf("custom model route context = %v, want 32000", data[1]["context_length"])
	}
}

func TestBuildOpenAIModelsResponse_NilRouteMapKeepsHeuristic(t *testing.T) {
	resp := buildOpenAIModelsResponse([]string{"gpt-4o", "custom-vendor/my-model"}, time.Unix(3, 0).UTC(), nil)
	data, _ := resp["data"].([]map[string]any)
	if data[0]["context_length"] != int64(128000) {
		t.Errorf("nil route map should keep heuristic, got %v", data[0]["context_length"])
	}
	if _, ok := data[1]["context_length"]; ok {
		t.Errorf("unknown model with nil route map should omit context_length, got %v", data[1]["context_length"])
	}
}

func TestBuildOpenAIModelsResponse_NonPositiveRouteLengthFallsBack(t *testing.T) {
	// Zero/negative route values must not override heuristics or invent a window.
	resp := buildOpenAIModelsResponse(
		[]string{"gpt-4o", "custom-vendor/my-model"},
		time.Unix(4, 0).UTC(),
		map[string]int64{
			"gpt-4o":                 0,
			"custom-vendor/my-model": -1,
		},
	)
	data, _ := resp["data"].([]map[string]any)
	if data[0]["context_length"] != int64(128000) {
		t.Errorf("non-positive route length should fall back to heuristic, got %v", data[0]["context_length"])
	}
	if _, ok := data[1]["context_length"]; ok {
		t.Errorf("non-positive route length on unknown model should omit field, got %v", data[1]["context_length"])
	}
}

func TestHandleModels_RouteContextLengthReflected(t *testing.T) {
	// AC: create/list path with route contextLength → /v1/models reflects it.
	// (Admin CRUD already stores the column; this verifies models listing consumption.)
	withModelsRouter(t, &modelsTestRouter{
		models: []string{"gpt-4o", "custom-route-model"},
		contextLengths: map[string]int64{
			"gpt-4o":             99999, // override heuristic 128000
			"custom-route-model": 424242,
		},
	})

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
	data, _ := m["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("expected 2 models, got %#v", data)
	}
	byID := map[string]map[string]any{}
	for _, raw := range data {
		item := raw.(map[string]any)
		id, _ := item["id"].(string)
		byID[id] = item
	}
	// JSON numbers decode as float64
	if byID["gpt-4o"]["context_length"] != float64(99999) {
		t.Fatalf("gpt-4o context_length = %v, want 99999 (route override)", byID["gpt-4o"]["context_length"])
	}
	if byID["custom-route-model"]["context_length"] != float64(424242) {
		t.Fatalf("custom-route-model context_length = %v, want 424242", byID["custom-route-model"]["context_length"])
	}
}

func TestHandleModels_RouteContextLengthErrorKeepsHeuristic(t *testing.T) {
	withModelsRouter(t, &modelsTestRouter{
		models:     []string{"gpt-4o"},
		contextErr: errors.New("context map unavailable"),
	})

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
	data, _ := m["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 model, got %#v", data)
	}
	first := data[0].(map[string]any)
	if first["context_length"] != float64(128000) {
		t.Fatalf("context error should keep heuristic, got %v", first["context_length"])
	}
}

func TestHandleModels_ClaudeFormatOmitsContextLengthEvenWithRouteMap(t *testing.T) {
	// Claude path must stay unchanged — no context_length field.
	withModelsRouter(t, &modelsTestRouter{
		models:         []string{"claude-sonnet-4-20250514"},
		contextLengths: map[string]int64{"claude-sonnet-4-20250514": 500000},
	})

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
	if len(data) != 1 {
		t.Fatalf("expected 1 model, got %#v", data)
	}
	first := data[0].(map[string]any)
	if _, ok := first["context_length"]; ok {
		t.Fatalf("Claude format must not emit context_length, got %v", first["context_length"])
	}
	if first["type"] != "model" {
		t.Fatalf("type = %v, want model", first["type"])
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

func TestGetAvailableModels_LastResortStubWhenUnwired(t *testing.T) {
	// TestMain enables METAPI_ENABLE_PROXY_STUB; nil upstream uses last-resort catalog.
	SetUpstreamConfig(nil)
	models := getAvailableModels(context.Background(), auth.EmptyDownstreamRoutingPolicy)
	if len(models) == 0 {
		t.Error("expected non-empty model list from last-resort stub catalog")
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

func TestGetAvailableModels_UsesRouterCatalog(t *testing.T) {
	withModelsRouter(t, &modelsTestRouter{
		models: []string{"router-model-b", "router-model-a", "router-model-a", "  "},
	})
	got := getAvailableModels(context.Background(), auth.EmptyDownstreamRoutingPolicy)
	if len(got) != 2 {
		t.Fatalf("router catalog = %v, want 2 unique sorted models", got)
	}
	if got[0] != "router-model-a" || got[1] != "router-model-b" {
		t.Fatalf("router catalog = %v, want sorted [router-model-a router-model-b]", got)
	}
}

func TestGetAvailableModels_RouterErrorReturnsEmpty(t *testing.T) {
	withModelsRouter(t, &modelsTestRouter{err: errors.New("db down")})
	got := getAvailableModels(context.Background(), auth.EmptyDownstreamRoutingPolicy)
	if len(got) != 0 {
		t.Fatalf("router error should yield empty catalog, got %v", got)
	}
}

func TestGetAvailableModels_SelectionOnlyRouterFallsBackToStub(t *testing.T) {
	// selectionOnlyRouter does not implement AvailableModelsSource.
	withModelsRouter(t, &selectionOnlyRouter{})
	got := getAvailableModels(context.Background(), auth.EmptyDownstreamRoutingPolicy)
	if len(got) == 0 {
		t.Fatal("selection-only router should fall back to last-resort stub catalog")
	}
	known := false
	for _, m := range got {
		if m == "gpt-4o" {
			known = true
			break
		}
	}
	if !known {
		t.Fatalf("expected stub gpt-4o in selection-only fallback, got %v", got)
	}
}

func TestGetAvailableModels_EmptyWhenNoRouterAndStubDisabled(t *testing.T) {
	t.Setenv("METAPI_ENABLE_PROXY_STUB", "0")
	SetUpstreamConfig(nil)
	got := getAvailableModels(context.Background(), auth.EmptyDownstreamRoutingPolicy)
	if len(got) != 0 {
		t.Fatalf("production nil-router without stub should return empty, got %v", got)
	}
}

func TestGetAvailableModelsAppliesManagedPolicy(t *testing.T) {
	withModelsRouter(t, &modelsTestRouter{
		models: []string{"gpt-4o", "claude-sonnet-4-20250514", "gemini-2.5-pro"},
	})

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

func TestHandleModels_UsesRouterCatalog(t *testing.T) {
	withModelsRouter(t, &modelsTestRouter{models: []string{"custom-route-model"}})

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
	data, _ := m["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 model from router, got %#v", data)
	}
	first := data[0].(map[string]any)
	if first["id"] != "custom-route-model" {
		t.Fatalf("id = %v, want custom-route-model", first["id"])
	}
	if first["owned_by"] != modelsOwnedBy {
		t.Fatalf("owned_by = %v", first["owned_by"])
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
