package service

import (
	"strings"
	"testing"
)

func TestListSiteInitializationPresets_ParityCount(t *testing.T) {
	presets := ListSiteInitializationPresets()
	if len(presets) != 13 {
		t.Fatalf("expected 13 presets, got %d", len(presets))
	}

	// Defensive copy: mutating returned slice must not affect registry.
	presets[0].ID = "mutated"
	if GetSiteInitializationPreset("mutated") != nil {
		t.Fatal("ListSiteInitializationPresets should return clones")
	}
	if GetSiteInitializationPreset("codingplan-openai") == nil {
		t.Fatal("expected codingplan-openai still present after list mutation")
	}
}

func TestGetSiteInitializationPreset(t *testing.T) {
	got := GetSiteInitializationPreset("  deepseek-openai ")
	if got == nil {
		t.Fatal("expected preset")
	}
	if got.Platform != "openai" {
		t.Fatalf("platform=%q", got.Platform)
	}
	if got.DefaultURL != "https://api.deepseek.com/v1" {
		t.Fatalf("defaultUrl=%q", got.DefaultURL)
	}
	if GetSiteInitializationPreset("no-such-preset") != nil {
		t.Fatal("expected nil for unknown id")
	}
	if GetSiteInitializationPreset("") != nil {
		t.Fatal("expected nil for empty id")
	}
}

func TestDetectSiteInitializationPreset_HostPath(t *testing.T) {
	cases := []struct {
		url      string
		platform string
		wantID   string
	}{
		{"https://coding.dashscope.aliyuncs.com/v1", "", "codingplan-openai"},
		{"https://coding.dashscope.aliyuncs.com/apps/anthropic", "", "codingplan-claude"},
		{"https://open.bigmodel.cn/api/coding/paas/v4", "openai", "zhipu-coding-plan-openai"},
		{"https://api.deepseek.com/v1", "", "deepseek-openai"},
		{"https://api.deepseek.com/anthropic", "", "deepseek-claude"},
		{"https://api.moonshot.cn", "", "moonshot-openai"},
		{"https://api.moonshot.cn/anthropic", "claude", "moonshot-claude"},
		{"https://api.minimaxi.com/v1", "", "minimax-openai"},
		{"https://api-inference.modelscope.cn/v1", "", "modelscope-openai"},
		{"https://api-inference.modelscope.cn", "", "modelscope-claude"},
		{"https://ark.cn-beijing.volces.com/api/coding/v3", "", "doubao-coding-openai"},
	}
	for _, tc := range cases {
		t.Run(tc.wantID+"/"+tc.url, func(t *testing.T) {
			got := DetectSiteInitializationPreset(tc.url, tc.platform)
			if got == nil {
				t.Fatalf("expected preset %s", tc.wantID)
			}
			if got.ID != tc.wantID {
				t.Fatalf("got %q want %q", got.ID, tc.wantID)
			}
		})
	}
}

func TestDetectSiteInitializationPreset_ManualOnlyZhipuClaude(t *testing.T) {
	// zhipu-coding-plan-claude intentionally does not auto-match by URL.
	if got := DetectSiteInitializationPreset("https://open.bigmodel.cn/api/anthropic", ""); got != nil {
		t.Fatalf("expected no auto-detect without platform, got %q", got.ID)
	}
	// Platform-filtered defaultUrl fallback should still resolve it.
	got := DetectSiteInitializationPreset("https://open.bigmodel.cn/api/anthropic", "claude")
	if got == nil || got.ID != "zhipu-coding-plan-claude" {
		t.Fatalf("expected zhipu-coding-plan-claude via defaultUrl fallback, got %#v", got)
	}
}

func TestDetectSiteInitializationPreset_PlatformFilter(t *testing.T) {
	// Same host, claude path should not match openai filter.
	if got := DetectSiteInitializationPreset("https://api.deepseek.com/anthropic", "openai"); got != nil {
		t.Fatalf("expected nil for openai filter on anthropic path, got %q", got.ID)
	}
	got := DetectSiteInitializationPreset("https://api.deepseek.com/anthropic", "claude")
	if got == nil || got.ID != "deepseek-claude" {
		t.Fatalf("got %#v", got)
	}
}

func TestValidateSiteInitializationPreset(t *testing.T) {
	if err := ValidateSiteInitializationPreset("", "openai", "https://api.openai.com"); err != nil {
		t.Fatalf("empty id should be optional: %v", err)
	}

	if err := ValidateSiteInitializationPreset("nope", "openai", "https://api.deepseek.com/v1"); err == nil {
		t.Fatal("expected unknown preset error")
	} else if !strings.Contains(err.Error(), "Unknown initializationPresetId") {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ValidateSiteInitializationPreset("deepseek-openai", "claude", "https://api.deepseek.com/v1"); err == nil {
		t.Fatal("expected platform mismatch")
	} else if !strings.Contains(err.Error(), "does not match platform") {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ValidateSiteInitializationPreset("deepseek-openai", "openai", "https://api.openai.com"); err == nil {
		t.Fatal("expected URL mismatch")
	} else if !strings.Contains(err.Error(), "does not match site URL") {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ValidateSiteInitializationPreset("deepseek-openai", "openai", "https://api.deepseek.com/v1"); err != nil {
		t.Fatalf("valid preset: %v", err)
	}

	// Manual-only preset: platform match is enough; URL is not enforced.
	if err := ValidateSiteInitializationPreset("zhipu-coding-plan-claude", "claude", "https://example.com"); err != nil {
		t.Fatalf("manual-only preset should skip URL rules: %v", err)
	}
}

func TestAnalyzePrimarySiteURL_StripAndSemantic(t *testing.T) {
	stripped := AnalyzePrimarySiteURL("https://api.deepseek.com/v1")
	if stripped.Action != "auto_strip_known_api_suffix" {
		t.Fatalf("action=%q", stripped.Action)
	}
	if stripped.PersistedURL != "https://api.deepseek.com" {
		t.Fatalf("persisted=%q", stripped.PersistedURL)
	}

	semantic := AnalyzePrimarySiteURL("https://coding.dashscope.aliyuncs.com/apps/anthropic")
	if semantic.Action != "preserve_semantic_path" {
		t.Fatalf("action=%q", semantic.Action)
	}
	if semantic.PersistedURL != "https://coding.dashscope.aliyuncs.com/apps/anthropic" {
		t.Fatalf("persisted=%q", semantic.PersistedURL)
	}
}
