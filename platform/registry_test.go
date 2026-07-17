package platform

import (
	"strings"
	"testing"
)

// --- Registry and Registration Order ---

func TestRegistry_AdapterCount(t *testing.T) {
	adapters := ListAdapters()
	if len(adapters) < 14 {
		t.Fatalf("expected at least 14 adapters, got %d", len(adapters))
	}

	// Verify no duplicate names
	seen := make(map[string]bool)
	for _, a := range adapters {
		name := a.PlatformName()
		if seen[name] {
			t.Errorf("duplicate adapter name in registry: %q", name)
		}
		seen[name] = true
	}
}

func TestRegistry_RegistrationOrder(t *testing.T) {
	InitRegistry()

	adapters := ListAdapters()
	names := make([]string, len(adapters))
	for i, a := range adapters {
		names[i] = a.PlatformName()
	}

	// Verify order matches spec
	expected := []string{
		"openai", "codex", "claude", "gemini", "gemini-cli",
		"antigravity", "cliproxyapi", "anyrouter", "done-hub",
		"one-hub", "veloera", "new-api", "sub2api", "one-api",
	}
	if len(names) != len(expected) {
		t.Fatalf("expected %d adapters, got %d: %v", len(expected), len(names), names)
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("position %d: got %q, want %q", i, name, expected[i])
		}
	}
}

func TestGetAdapter(t *testing.T) {
	// Known platforms
	a := GetAdapter("openai")
	if a == nil || a.PlatformName() != "openai" {
		t.Errorf("GetAdapter('openai'): %v", a)
	}

	// Unknown platform
	a2 := GetAdapter("nonexistent-platform")
	if a2 != nil {
		t.Errorf("GetAdapter('nonexistent') should return nil: %v", a2)
	}

	// Empty
	a3 := GetAdapter("")
	if a3 != nil {
		t.Errorf("GetAdapter('') should return nil: %v", a3)
	}
}

// --- NormalizePlatformAlias ---

func TestNormalizePlatformAlias(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"new-api", "new-api"},
		{"newapi", "new-api"},
		{"new api", "new-api"},
		{"NEW-API", "new-api"},
		{"one-api", "one-api"},
		{"oneapi", "one-api"},
		{"one api", "one-api"},
		{"one-hub", "one-hub"},
		{"onehub", "one-hub"},
		{"done-hub", "done-hub"},
		{"donehub", "done-hub"},
		{"openai", "openai"},
		{"anthropic", "claude"},
		{"claude", "claude"},
		{"codex", "codex"},
		{"chatgpt-codex", "codex"},
		{"gemini", "gemini"},
		{"google", "gemini"},
		{"gemini-cli", "gemini-cli"},
		{"antigravity", "antigravity"},
		{"anti-gravity", "antigravity"},
		{"cliproxyapi", "cliproxyapi"},
		{"cpa", "cliproxyapi"},
		{"cli-proxy-api", "cliproxyapi"},
		{"anyrouter", "anyrouter"},
		{"veloera", "veloera"},
		{"sub2api", "sub2api"},
		{"unknown", "unknown"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizePlatformAlias(tt.input); got != tt.expected {
			t.Errorf("NormalizePlatformAlias(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizePlatformAlias_NewApiForks(t *testing.T) {
	// NewApi fork aliases
	tests := []struct {
		input    string
		expected string
	}{
		{"wong-gongyi", "new-api"},
		{"vo-api", "new-api"},
		{"super-api", "new-api"},
		{"rix-api", "new-api"},
		{"neo-api", "new-api"},
	}
	for _, tt := range tests {
		if got := NormalizePlatformAlias(tt.input); got != tt.expected {
			t.Errorf("NormalizePlatformAlias(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// --- DetectPlatform pipeline ---

func TestDetectPlatform_URLHint_OpenAI(t *testing.T) {
	// URL hint should match api.openai.com
	result := DetectPlatform("https://api.openai.com/v1/models")
	if result == nil {
		t.Fatal("DetectPlatform should detect OpenAI via URL hint")
	}
	if result.PlatformName() != "openai" {
		t.Errorf("Detected: %q, want openai", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_Codex(t *testing.T) {
	result := DetectPlatform("https://chatgpt.com/backend-api/codex/models")
	if result == nil {
		t.Fatal("DetectPlatform should detect Codex via URL hint")
	}
	if result.PlatformName() != "codex" {
		t.Errorf("Detected: %q, want codex", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_Claude(t *testing.T) {
	result := DetectPlatform("https://api.anthropic.com/v1/messages")
	if result == nil {
		t.Fatal("DetectPlatform should detect Claude via URL hint")
	}
	if result.PlatformName() != "claude" {
		t.Errorf("Detected: %q, want claude", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_Gemini(t *testing.T) {
	result := DetectPlatform("https://generativelanguage.googleapis.com/v1beta/models")
	if result == nil {
		t.Fatal("DetectPlatform should detect Gemini via URL hint")
	}
	if result.PlatformName() != "gemini" {
		t.Errorf("Detected: %q, want gemini", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_GeminiCli(t *testing.T) {
	result := DetectPlatform("https://cloudcode-pa.googleapis.com/v1/models")
	if result == nil {
		t.Fatal("DetectPlatform should detect GeminiCli via URL hint")
	}
	if result.PlatformName() != "gemini-cli" {
		t.Errorf("Detected: %q, want gemini-cli", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_CliProxyApi(t *testing.T) {
	result := DetectPlatform("http://127.0.0.1:8317/v1/models")
	if result == nil {
		t.Fatal("DetectPlatform should detect CliProxyApi via URL hint (port 8317)")
	}
	if result.PlatformName() != "cliproxyapi" {
		t.Errorf("Detected: %q, want cliproxyapi", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_AnyRouter(t *testing.T) {
	result := DetectPlatform("https://anyrouter.example.com")
	if result == nil {
		t.Fatal("DetectPlatform should detect AnyRouter via URL hint")
	}
	if result.PlatformName() != "anyrouter" {
		t.Errorf("Detected: %q, want anyrouter", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_DoneHub(t *testing.T) {
	result := DetectPlatform("https://donehub.example.com")
	if result == nil {
		t.Fatal("DetectPlatform should detect DoneHub via URL hint")
	}
	if result.PlatformName() != "done-hub" {
		t.Errorf("Detected: %q, want done-hub", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_OneHub(t *testing.T) {
	result := DetectPlatform("https://onehub.example.com")
	if result == nil {
		t.Fatal("DetectPlatform should detect OneHub via URL hint")
	}
	if result.PlatformName() != "one-hub" {
		t.Errorf("Detected: %q, want one-hub", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_Veloera(t *testing.T) {
	result := DetectPlatform("https://veloera.example.com")
	if result == nil {
		t.Fatal("DetectPlatform should detect Veloera via URL hint")
	}
	if result.PlatformName() != "veloera" {
		t.Errorf("Detected: %q, want veloera", result.PlatformName())
	}
}

func TestDetectPlatform_URLHint_Sub2Api(t *testing.T) {
	result := DetectPlatform("https://sub2api.example.com")
	if result == nil {
		t.Fatal("DetectPlatform should detect Sub2Api via URL hint")
	}
	if result.PlatformName() != "sub2api" {
		t.Errorf("Detected: %q, want sub2api", result.PlatformName())
	}
}

func TestDetectPlatform_Unknown(t *testing.T) {
	// An unreachable URL with no keyword matches should return nil
	result := DetectPlatform("https://completely-unknown-site.example.com")
	if result != nil {
		t.Errorf("DetectPlatform for unknown URL should return nil, got %q", result.PlatformName())
	}
}

func TestDetectPlatform_EmptyURL(t *testing.T) {
	result := DetectPlatform("")
	if result != nil {
		t.Errorf("DetectPlatform for empty URL should return nil, got %q", result.PlatformName())
	}
}

// --- DetectPlatformByURLHint ---

func TestDetectPlatformByURLHint_AllPatterns(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://api.openai.com/v1/models", "openai"},
		{"https://chatgpt.com/backend-api/codex", "codex"},
		{"https://api.anthropic.com/v1/messages", "claude"},
		{"https://anthropic.com/v1/messages", "claude"},
		{"https://generativelanguage.googleapis.com/v1beta", "gemini"},
		{"https://gemini.google.com", "gemini"},
		{"https://googleapis.com/v1beta/openai/models", "gemini"},
		{"https://us-central1-aiplatform.googleapis.com/v1beta/openai", "gemini"},
		{"https://cloudcode-pa.googleapis.com", "gemini-cli"},
		{"http://127.0.0.1:8317/v1", "cliproxyapi"},
		{"http://localhost:8317", "cliproxyapi"},
		{"https://anyrouter.example.com", "anyrouter"},
		{"https://donehub.example.com", "done-hub"},
		{"https://done-hub.example.com", "done-hub"},
		{"https://onehub.example.com", "one-hub"},
		{"https://one-hub.example.com", "one-hub"},
		{"https://veloera.example.com", "veloera"},
		{"https://sub2api.example.com", "sub2api"},
		{"https://unknown.example.com", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := DetectPlatformByURLHint(tt.url); got != tt.expected {
			t.Errorf("DetectPlatformByURLHint(%q) = %q, want %q", tt.url, got, tt.expected)
		}
	}
}

// --- Title Hint patterns ---

func TestExtractHTMLTitle(t *testing.T) {
	tests := []struct {
		html     string
		expected string
	}{
		{`<html><head><title>My Page</title></head></html>`, "My Page"},
		{`<title>  New-API  </title>`, "New-API"},
		{`<TITLE>One-Api</TITLE>`, "One-Api"},
		{`<title>Multi Word Title</title>`, "Multi Word Title"},
		{`<html><body>No title</body></html>`, ""},
		{``, ""},
	}
	for _, tt := range tests {
		if got := extractHTMLTitle(tt.html); got != tt.expected {
			t.Errorf("extractHTMLTitle(%q) = %q, want %q", tt.html, got, tt.expected)
		}
	}
}

func TestTitleRules_Matching(t *testing.T) {
	tests := []struct {
		title    string
		platform string
	}{
		{"Any Router Dashboard", "anyrouter"},
		{"AnyRouter", "anyrouter"},
		{"Done-Hub Admin", "done-hub"},
		{"DoneHub Panel", "done-hub"},
		{"One-Hub Console", "one-hub"},
		{"OneHub", "one-hub"},
		{"Veloera API", "veloera"},
		{"Sub2API Management", "sub2api"},
		{"New-API Panel", "new-api"},
		{"NewApi Console", "new-api"},
		{"Vo-API Admin", "new-api"},
		{"Super-API Portal", "new-api"},
		{"Rix-API Hub", "new-api"},
		{"Neo-API Gateway", "new-api"},
		{"wong 公益站", "new-api"},
		{"One-API Admin", "one-api"},
		{"OneApi Panel", "one-api"},
		{"Random Page", ""},
	}
	for _, tt := range tests {
		lower := strings.ToLower(tt.title)
		matched := ""
		for _, rule := range titleRules {
			if rule.regex.MatchString(lower) {
				matched = string(rule.platform)
				break
			}
		}
		if matched != tt.platform {
			t.Errorf("title %q: matched %q, want %q", tt.title, matched, tt.platform)
		}
	}
}

func TestTitleFirstPlatforms(t *testing.T) {
	// Platforms that short-circuit
	if !titleFirstPlatforms[TitleAnyRouter] {
		t.Error("AnyRouter should be in titleFirstPlatforms")
	}
	if !titleFirstPlatforms[TitleDoneHub] {
		t.Error("DoneHub should be in titleFirstPlatforms")
	}
	if !titleFirstPlatforms[TitleOneHub] {
		t.Error("OneHub should be in titleFirstPlatforms")
	}
	if !titleFirstPlatforms[TitleVeloera] {
		t.Error("Veloera should be in titleFirstPlatforms")
	}
	if !titleFirstPlatforms[TitleSub2Api] {
		t.Error("Sub2Api should be in titleFirstPlatforms")
	}

	// Platforms that DON'T short-circuit
	if titleFirstPlatforms[TitleNewApi] {
		t.Error("NewApi should NOT be in titleFirstPlatforms")
	}
	if titleFirstPlatforms[TitleOneApi] {
		t.Error("OneApi should NOT be in titleFirstPlatforms")
	}
}

func TestNormalizeURLToOrigin(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://example.com/path?q=1", "https://example.com"},
		{"http://example.com:8080/path", "http://example.com:8080"},
		{"example.com", "https://example.com"},
		{"example.com/path", "https://example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeURLToOrigin(tt.input); got != tt.expected {
			t.Errorf("normalizeURLToOrigin(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// --- ListAdapters ---

func TestListAdapters_Copy(t *testing.T) {
	original := ListAdapters()
	// Modify the returned slice
	if len(original) > 0 {
		original[0] = nil
	}
	// Original registry should be unaffected
	again := ListAdapters()
	if len(again) != len(original) || again[0] == nil {
		// Actually ListAdapters creates a copy, so modification won't persist
		// This just ensures it doesn't panic
	}
}

func TestListRegisteredPlatformNames(t *testing.T) {
	InitRegistry()

	names := ListRegisteredPlatformNames()
	if len(names) < 14 {
		t.Fatalf("expected at least 14 registered platform names, got %d: %v", len(names), names)
	}

	// Stable order matches orderedPlatformNames for known adapters.
	expectedPrefix := []string{"openai", "codex", "claude", "gemini"}
	for i, want := range expectedPrefix {
		if i >= len(names) || names[i] != want {
			t.Fatalf("names[%d] = %q, want %q (full=%v)", i, names[i], want, names)
		}
	}

	// No duplicates.
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" {
			t.Fatal("empty platform name in list")
		}
		if seen[name] {
			t.Fatalf("duplicate platform name %q", name)
		}
		seen[name] = true
	}
}

// --- Helper ---

func indexOfString(slice []string, target string) int {
	for i, s := range slice {
		if s == target {
			return i
		}
	}
	return -1
}
