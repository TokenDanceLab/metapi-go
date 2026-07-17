package routing

import (
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

func int64Ptr(v int64) *int64 { return &v }

func TestBuildAvailableModelContextLengths(t *testing.T) {
	// Multi-route same exposed id → max; NULL/non-positive omitted; display_name wins.
	// Production FindAllEnabledRoutes only feeds enabled rows; this helper maps what it is given.
	dnCustom := "custom-model"
	routes := []store.TokenRoute{
		{ID: 1, ModelPattern: "gpt-4o", Enabled: true, ContextLength: int64Ptr(64000)},
		{ID: 2, ModelPattern: "gpt-4o", Enabled: true, ContextLength: int64Ptr(128000)}, // max wins
		{ID: 3, ModelPattern: "gpt-4o", Enabled: true, ContextLength: nil},             // ignored
		{ID: 4, ModelPattern: "gpt-4o", Enabled: true, ContextLength: int64Ptr(0)},     // ignored
		{ID: 5, ModelPattern: "raw-id", DisplayName: &dnCustom, Enabled: true, ContextLength: int64Ptr(32000)},
		{ID: 6, ModelPattern: "no-ctx", Enabled: true, ContextLength: nil},
	}

	got := buildAvailableModelContextLengths(routes)
	if got["gpt-4o"] != 128000 {
		t.Fatalf("gpt-4o = %d, want max positive 128000", got["gpt-4o"])
	}
	if got["custom-model"] != 32000 {
		t.Fatalf("custom-model = %d, want 32000 from display_name", got["custom-model"])
	}
	if _, ok := got["raw-id"]; ok {
		t.Fatalf("raw-id should not be exposed when display_name is set, map=%v", got)
	}
	if _, ok := got["no-ctx"]; ok {
		t.Fatalf("NULL context_length should omit entry, map=%v", got)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 map entries, got %d: %v", len(got), got)
	}
}

func TestBuildAvailableModelContextLengths_Empty(t *testing.T) {
	if got := buildAvailableModelContextLengths(nil); len(got) != 0 {
		t.Fatalf("nil routes → empty map, got %v", got)
	}
	if got := buildAvailableModelContextLengths([]store.TokenRoute{}); len(got) != 0 {
		t.Fatalf("empty routes → empty map, got %v", got)
	}
}
