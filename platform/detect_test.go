package platform

import (
	"strings"
	"testing"
)

// --- Detect integration tests ---

func TestDetectPlatform_4StepPipeline_Step1_URLHint(t *testing.T) {
	// Step 1: URL hint should match known hosts
	result := DetectPlatform("https://api.openai.com/v1/chat/completions")
	if result == nil || result.PlatformName() != "openai" {
		t.Errorf("Step 1 URL hint: %v", result)
	}
}

func TestDetectPlatform_4StepPipeline_Step3_SequentialProbe(t *testing.T) {
	// Step 3: Sequential probe on keyword-matching adapters
	// AnyRouter should match via Detect (URL keyword)
	result := DetectPlatform("https://some-anyrouter-site.example.com")
	if result == nil || result.PlatformName() != "anyrouter" {
		t.Errorf("Step 3 sequential probe (AnyRouter): %v", result)
	}
}

func TestDetectPlatform_OrderOfProbing(t *testing.T) {
	// Verify that more specific adapters are probed first
	// CliProxyApi (port 8317) should match before NewApi
	// This can't be easily tested without mock HTTP servers,
	// but we verify the order is correct at the registry level.
	adapters := ListAdapters()
	names := make([]string, len(adapters))
	for i, a := range adapters {
		names[i] = a.PlatformName()
	}

	// Verify CliProxyApi is before NewApi in registry
	cliIdx := indexOfString(names, "cliproxyapi")
	newapiIdx := indexOfString(names, "new-api")
	if cliIdx >= 0 && newapiIdx >= 0 && cliIdx > newapiIdx {
		t.Errorf("cliproxyapi (%d) should come before new-api (%d)", cliIdx, newapiIdx)
	}
}

// --- DetectPlatformByTitle ---

func TestDetectPlatformByTitle_Retry(t *testing.T) {
	// Should handle unreachable URLs gracefully (return empty string)
	result := DetectPlatformByTitle("http://127.0.0.1:1")
	if result != "" {
		t.Errorf("expected empty for unreachable URL, got %q", result)
	}
}

// --- URL Hint edge cases ---

func TestDetectPlatformByURLHint_EdgeCases(t *testing.T) {
	// Empty URL
	if r := DetectPlatformByURLHint(""); r != "" {
		t.Errorf("empty URL: %q", r)
	}

	// Invalid URL
	if r := DetectPlatformByURLHint("://invalid"); r != "" {
		t.Errorf("invalid URL: %q", r)
	}

	// No scheme - should be normalized
	if r := DetectPlatformByURLHint("api.openai.com"); r != "openai" {
		t.Errorf("no scheme: %q, want openai", r)
	}
}

// --- Title Rule detail tests ---

func TestTitleRule_NewApi_FiveForks(t *testing.T) {
	// Verify all 5 NewApi fork title patterns
	titles := map[string]bool{
		"New-API Dashboard": true,
		"Vo-API Admin":      true,
		"Super-API Portal":  true,
		"Rix-API Hub":       true,
		"Neo-API Gateway":   true,
	}

	for title := range titles {
		found := false
		lower := strings.ToLower(title)
		for _, rule := range titleRules {
			if rule.platform == TitleNewApi && rule.regex.MatchString(lower) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("title %q should match NewApi title rule", title)
		}
	}
}

func TestTitleRule_WongGongYi(t *testing.T) {
	// wong 公益站
	found := false
	for _, rule := range titleRules {
		if rule.platform == TitleNewApi && rule.regex.MatchString("wong 公益站") {
			found = true
			break
		}
	}
	if !found {
		t.Error("'wong 公益站' should match NewApi title rule")
	}
}

// --- Detection order with conflicting keywords ---

func TestDetectPlatform_Priority_AntigravityOverClaude(t *testing.T) {
	// A URL containing "antigravity" AND "anthropic" is less likely,
	// but if it did, antigravity (more specific) should match via URL hint
	result := DetectPlatform("https://antigravity.example.com/anthropic")
	if result == nil {
		t.Fatal("expected detection result")
	}
	// antigravity host matches URL hint first
	if result.PlatformName() != "antigravity" {
		t.Errorf("expected antigravity, got %q", result.PlatformName())
	}
}

func TestDetectPlatform_ClaudeAnthropicDotCom_V1(t *testing.T) {
	result := DetectPlatform("https://anthropic.com/v1/messages")
	if result == nil {
		t.Fatal("expected detection result")
	}
	if result.PlatformName() != "claude" {
		t.Errorf("expected claude, got %q", result.PlatformName())
	}
}

// --- Title hint does NOT short-circuit new-api/one-api ---

func TestTitleHint_NonShortCircuit_NewApi(t *testing.T) {
	// new-api title hint should NOT be in titleFirstPlatforms
	if titleFirstPlatforms[TitleNewApi] {
		t.Error("NewApi should not short-circuit on title hint alone")
	}
}

func TestTitleHint_NonShortCircuit_OneApi(t *testing.T) {
	// one-api title hint should NOT be in titleFirstPlatforms
	if titleFirstPlatforms[TitleOneApi] {
		t.Error("OneApi should not short-circuit on title hint alone")
	}
}
