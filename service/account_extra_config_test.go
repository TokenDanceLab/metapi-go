package service

import (
	"encoding/json"
	"testing"
)

func TestNormalizeManagedRefreshToken(t *testing.T) {
	t.Parallel()
	if _, ok := NormalizeManagedRefreshToken(nil); ok {
		t.Fatal("nil should be invalid")
	}
	if _, ok := NormalizeManagedRefreshToken("  "); ok {
		t.Fatal("blank should be invalid")
	}
	if _, ok := NormalizeManagedRefreshToken(123); ok {
		t.Fatal("non-string should be invalid")
	}
	got, ok := NormalizeManagedRefreshToken("  rt_abc  ")
	if !ok || got != "rt_abc" {
		t.Fatalf("got (%q, %v), want (rt_abc, true)", got, ok)
	}
}

func TestNormalizeManagedTokenExpiresAt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		want int64
		ok   bool
	}{
		{"nil", nil, 0, false},
		{"zero", 0, 0, false},
		{"negative", -1, 0, false},
		{"int", 1712345678, 1712345678, true},
		{"float", float64(1712345678), 1712345678, true},
		{"string", "1712345678", 1712345678, true},
		{"jsonNumber", json.Number("1712345678"), 1712345678, true},
		{"blank", "  ", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizeManagedTokenExpiresAt(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got (%d, %v), want (%d, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestMergeSub2ApiAuth_PreserveAndOverwrite(t *testing.T) {
	t.Parallel()
	existing := map[string]any{
		"refreshToken":   "rt_old",
		"tokenExpiresAt": int64(100),
		"custom":         "keep-me",
	}

	// Partial overwrite of refreshToken only.
	merged := MergeSub2ApiAuth(existing, "rt_new", nil)
	if merged["refreshToken"] != "rt_new" {
		t.Fatalf("refreshToken = %v, want rt_new", merged["refreshToken"])
	}
	if merged["tokenExpiresAt"] != int64(100) {
		t.Fatalf("tokenExpiresAt = %v, want preserved 100", merged["tokenExpiresAt"])
	}
	if merged["custom"] != "keep-me" {
		t.Fatalf("custom = %v, want keep-me", merged["custom"])
	}

	// Invalid incoming values preserve existing.
	merged = MergeSub2ApiAuth(existing, "  ", -5)
	if merged["refreshToken"] != "rt_old" {
		t.Fatalf("invalid refresh should preserve old, got %v", merged["refreshToken"])
	}
	if merged["tokenExpiresAt"] != int64(100) {
		t.Fatalf("invalid expires should preserve old, got %v", merged["tokenExpiresAt"])
	}
}

func TestBuildMergedSub2ApiAuth_TopLevelAndNested(t *testing.T) {
	t.Parallel()
	existing := `{"credentialMode":"session","proxyUrl":"http://proxy","sub2apiAuth":{"refreshToken":"rt_old","tokenExpiresAt":100,"custom":"keep-me"}}`
	rt := "rt_top"
	exp := int64(200)
	patch := map[string]any{
		"sub2apiAuth": map[string]any{
			"refreshToken":   "rt_nested",
			"tokenExpiresAt": int64(150),
		},
	}

	// Top-level wins over nested patch.
	merged := BuildMergedSub2ApiAuth(&existing, &rt, &exp, patch)
	if merged == nil {
		t.Fatal("expected merged auth")
	}
	if merged["refreshToken"] != "rt_top" {
		t.Fatalf("refreshToken = %v, want rt_top", merged["refreshToken"])
	}
	if merged["tokenExpiresAt"] != int64(200) {
		t.Fatalf("tokenExpiresAt = %v, want 200", merged["tokenExpiresAt"])
	}
	if merged["custom"] != "keep-me" {
		t.Fatalf("custom = %v, want keep-me", merged["custom"])
	}

	// Nested-only partial update preserves other auth fields.
	merged = BuildMergedSub2ApiAuth(&existing, nil, nil, map[string]any{
		"sub2apiAuth": map[string]any{"tokenExpiresAt": int64(333)},
	})
	if merged["refreshToken"] != "rt_old" {
		t.Fatalf("refreshToken = %v, want rt_old", merged["refreshToken"])
	}
	if merged["tokenExpiresAt"] != int64(333) {
		t.Fatalf("tokenExpiresAt = %v, want 333", merged["tokenExpiresAt"])
	}

	// No incoming fields => nil (leave alone).
	if got := BuildMergedSub2ApiAuth(&existing, nil, nil, nil); got != nil {
		t.Fatalf("expected nil when no incoming fields, got %#v", got)
	}

	// Invalid blank top-level should not write.
	blank := "  "
	if got := BuildMergedSub2ApiAuth(&existing, &blank, nil, nil); got != nil {
		t.Fatalf("expected nil for blank refresh, got %#v", got)
	}
}

func TestIsSub2ApiPlatform(t *testing.T) {
	t.Parallel()
	if !IsSub2ApiPlatform("sub2api") || !IsSub2ApiPlatform(" Sub2API ") {
		t.Fatal("expected sub2api platform match")
	}
	if IsSub2ApiPlatform("anyrouter") {
		t.Fatal("anyrouter should not match")
	}
}
