package service

import (
	"testing"
)

// ---- Platform Detection Tests ----

func TestDetectSite_OpenAI(t *testing.T) {
	result := DetectSite("https://api.openai.com")
	if result == nil {
		t.Fatal("expected detection result for openai.com, got nil")
	}
	if result.Platform != "openai" {
		t.Errorf("expected platform 'openai', got %q", result.Platform)
	}
}

func TestDetectSite_OpenAISubdomain(t *testing.T) {
	tests := []string{
		"https://api.openai.com/v1",
		"https://api.openai.com/v1/chat/completions",
		"http://openai.com",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			result := DetectSite(url)
			if result == nil {
				t.Errorf("expected detection result for %q, got nil", url)
			} else if result.Platform != "openai" {
				t.Errorf("expected platform 'openai' for %q, got %q", url, result.Platform)
			}
		})
	}
}

func TestDetectSite_Anthropic(t *testing.T) {
	result := DetectSite("https://api.anthropic.com")
	if result == nil {
		t.Fatal("expected detection for anthropic.com")
	}
	if result.Platform != "anthropic" {
		t.Errorf("expected 'anthropic', got %q", result.Platform)
	}
}

func TestDetectSite_Gemini(t *testing.T) {
	tests := []string{
		"https://generativelanguage.googleapis.com",
		"https://ai.google.dev",
		"https://aistudio.google.com",
		"https://makersuite.google.com",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			result := DetectSite(url)
			if result == nil {
				t.Errorf("expected detection for %q, got nil", url)
			} else if result.Platform != "gemini" {
				t.Errorf("expected 'gemini' for %q, got %q", url, result.Platform)
			}
		})
	}
}

func TestDetectSite_DeepSeek(t *testing.T) {
	// Official OpenAI-compatible entry is covered by initialization presets.
	result := DetectSite("https://api.deepseek.com")
	if result == nil || result.Platform != "openai" {
		t.Fatalf("expected preset platform 'openai', got %v", result)
	}
	if result.InitializationPresetID == nil || *result.InitializationPresetID != "deepseek-openai" {
		t.Fatalf("expected initializationPresetId deepseek-openai, got %v", result.InitializationPresetID)
	}

	// Non-preset deepseek host still uses vendor heuristic.
	result = DetectSite("https://platform.deepseek.com")
	if result == nil || result.Platform != "deepseek" {
		t.Fatalf("expected vendor heuristic 'deepseek', got %v", result)
	}
}

func TestDetectSite_Moonshot(t *testing.T) {
	result := DetectSite("https://api.moonshot.cn/v1")
	if result == nil || result.Platform != "openai" {
		t.Fatalf("expected preset platform 'openai', got %v", result)
	}
	if result.InitializationPresetID == nil || *result.InitializationPresetID != "moonshot-openai" {
		t.Fatalf("expected initializationPresetId moonshot-openai, got %v", result.InitializationPresetID)
	}
}

func TestDetectSite_DashScope(t *testing.T) {
	result := DetectSite("https://dashscope.aliyuncs.com/compatible-mode/v1")
	if result == nil || result.Platform != "dashscope" {
		t.Fatalf("expected 'dashscope', got %v", result)
	}
}

func TestDetectSite_Baichuan(t *testing.T) {
	result := DetectSite("https://api.baichuan-ai.com/v1")
	if result == nil || result.Platform != "baichuan" {
		t.Fatalf("expected 'baichuan', got %v", result)
	}
}

func TestDetectSite_Zhipu(t *testing.T) {
	tests := []string{
		"https://api.zhipuai.cn",
		"https://bigmodel.cn",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			result := DetectSite(url)
			if result == nil || result.Platform != "zhipu" {
				t.Fatalf("expected 'zhipu' for %q, got %v", url, result)
			}
		})
	}
}

func TestDetectSite_Minimax(t *testing.T) {
	result := DetectSite("https://api.minimax.chat")
	if result == nil || result.Platform != "minimax" {
		t.Fatalf("expected 'minimax', got %v", result)
	}
}

func TestDetectSite_StepFun(t *testing.T) {
	result := DetectSite("https://api.stepfun.com")
	if result == nil || result.Platform != "stepfun" {
		t.Fatalf("expected 'stepfun', got %v", result)
	}
}

func TestDetectSite_ByteDance(t *testing.T) {
	result := DetectSite("https://ark.cn-beijing.volces.com/api/v3")
	if result == nil || result.Platform != "bytedance" {
		t.Fatalf("expected 'bytedance', got %v", result)
	}
}

func TestDetectSite_Volcengine(t *testing.T) {
	result := DetectSite("https://volcengine.com")
	if result == nil || result.Platform != "bytedance" {
		t.Fatalf("expected 'bytedance' for volcengine.com, got %v", result)
	}
}

func TestDetectSite_SiliconFlow(t *testing.T) {
	result := DetectSite("https://api.siliconflow.cn")
	if result == nil || result.Platform != "siliconflow" {
		t.Fatalf("expected 'siliconflow', got %v", result)
	}
}

func TestDetectSite_ModelScope(t *testing.T) {
	// Bare modelscope inference root maps to the Claude-compatible preset.
	result := DetectSite("https://api-inference.modelscope.cn")
	if result == nil || result.Platform != "claude" {
		t.Fatalf("expected preset platform 'claude', got %v", result)
	}
	if result.InitializationPresetID == nil || *result.InitializationPresetID != "modelscope-claude" {
		t.Fatalf("expected initializationPresetId modelscope-claude, got %v", result.InitializationPresetID)
	}

	result = DetectSite("https://api-inference.modelscope.cn/v1")
	if result == nil || result.Platform != "openai" {
		t.Fatalf("expected preset platform 'openai', got %v", result)
	}
	if result.InitializationPresetID == nil || *result.InitializationPresetID != "modelscope-openai" {
		t.Fatalf("expected initializationPresetId modelscope-openai, got %v", result.InitializationPresetID)
	}
}

func TestDetectSite_Mistral(t *testing.T) {
	result := DetectSite("https://api.mistral.ai")
	if result == nil || result.Platform != "mistral" {
		t.Fatalf("expected 'mistral', got %v", result)
	}
}

func TestDetectSite_Cohere(t *testing.T) {
	tests := []string{"https://api.cohere.ai", "https://cohere.com"}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			result := DetectSite(url)
			if result == nil || result.Platform != "cohere" {
				t.Fatalf("expected 'cohere' for %q, got %v", url, result)
			}
		})
	}
}

func TestDetectSite_Together(t *testing.T) {
	result := DetectSite("https://api.together.xyz")
	if result == nil || result.Platform != "together" {
		t.Fatalf("expected 'together', got %v", result)
	}
}

func TestDetectSite_Fireworks(t *testing.T) {
	result := DetectSite("https://api.fireworks.ai")
	if result == nil || result.Platform != "fireworks" {
		t.Fatalf("expected 'fireworks', got %v", result)
	}
}

func TestDetectSite_Groq(t *testing.T) {
	result := DetectSite("https://api.groq.com")
	if result == nil || result.Platform != "groq" {
		t.Fatalf("expected 'groq', got %v", result)
	}
}

func TestDetectSite_Perplexity(t *testing.T) {
	result := DetectSite("https://api.perplexity.ai")
	if result == nil || result.Platform != "perplexity" {
		t.Fatalf("expected 'perplexity', got %v", result)
	}
}

func TestDetectSite_XAI(t *testing.T) {
	tests := []string{"https://api.x.ai", "https://x.ai"}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			result := DetectSite(url)
			if result == nil || result.Platform != "xai" {
				t.Fatalf("expected 'xai' for %q, got %v", url, result)
			}
		})
	}
}

func TestDetectSite_HuggingFace(t *testing.T) {
	result := DetectSite("https://api-inference.huggingface.co")
	if result == nil || result.Platform != "huggingface" {
		t.Fatalf("expected 'huggingface', got %v", result)
	}
}

func TestDetectSite_Azure(t *testing.T) {
	result := DetectSite("https://my-resource.openai.azure.com")
	if result == nil || result.Platform != "azure" {
		t.Fatalf("expected 'azure', got %v", result)
	}
}

func TestDetectSite_GitHubCopilot(t *testing.T) {
	result := DetectSite("https://api.github.com/copilot/chat/completions")
	if result == nil || result.Platform != "github-copilot" {
		t.Fatalf("expected 'github-copilot', got %v", result)
	}
}

func TestDetectSite_Claude(t *testing.T) {
	result := DetectSite("https://claude.ai")
	if result == nil || result.Platform != "claude" {
		t.Fatalf("expected 'claude', got %v", result)
	}
}

func TestDetectSite_NewAPI(t *testing.T) {
	tests := []string{
		"https://oneapi.example.com",
		"https://new-api.example.com",
		"https://newapi.example.com",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			result := DetectSite(url)
			if result == nil || result.Platform != "new-api" {
				t.Fatalf("expected 'new-api' for %q, got %v", url, result)
			}
		})
	}
}

func TestDetectSite_Unknown(t *testing.T) {
	tests := []string{
		"https://unknown-llm-provider.example.com",
		"https://random-site.com",
		"",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			result := DetectSite(url)
			if result != nil {
				t.Errorf("expected nil for unknown url %q, got platform=%q", url, result.Platform)
			}
		})
	}
}

func TestDetectSite_EmptyString(t *testing.T) {
	result := DetectSite("")
	if result != nil {
		t.Errorf("expected nil for empty string, got %v", result)
	}
}

func TestDetectSite_InvalidURL(t *testing.T) {
	result := DetectSite("not-a-valid-url://///")
	if result != nil {
		t.Errorf("expected nil for invalid URL, got %v", result)
	}
}

// ---- URL Canonicalization Tests ----

func TestCanonicalizeSiteURL_Normal(t *testing.T) {
	result := CanonicalizeSiteURL("https://api.openai.com/v1/")
	if result != "https://api.openai.com/v1" {
		t.Errorf("expected trailing slash stripped, got %q", result)
	}
}

func TestCanonicalizeSiteURL_StripsQuery(t *testing.T) {
	result := CanonicalizeSiteURL("https://api.openai.com/v1?key=value")
	if result != "https://api.openai.com/v1" {
		t.Errorf("expected query stripped, got %q", result)
	}
}

func TestCanonicalizeSiteURL_StripsFragment(t *testing.T) {
	result := CanonicalizeSiteURL("https://api.openai.com/v1#section")
	if result != "https://api.openai.com/v1" {
		t.Errorf("expected fragment stripped, got %q", result)
	}
}

func TestCanonicalizeSiteURL_Empty(t *testing.T) {
	result := CanonicalizeSiteURL("")
	if result != "" {
		t.Errorf("expected empty for empty input, got %q", result)
	}
}

func TestCanonicalizeSiteURL_MultipleSlashes(t *testing.T) {
	result := CanonicalizeSiteURL("https://api.openai.com/v1///")
	if result != "https://api.openai.com/v1" {
		t.Errorf("expected single trailing slash stripped, got %q", result)
	}
}

// ---- API Endpoint URL Tests ----

func TestNormalizeSiteAPIEndpointBaseUrl_Normal(t *testing.T) {
	result := NormalizeSiteAPIEndpointBaseUrl("https://api.openai.com/v1/")
	if result != "https://api.openai.com/v1" {
		t.Errorf("expected trailing slash stripped, got %q", result)
	}
}

func TestNormalizeSiteAPIEndpointBaseUrl_StripsQuery(t *testing.T) {
	result := NormalizeSiteAPIEndpointBaseUrl("https://example.com/v1?api-version=2024")
	if result != "https://example.com/v1" {
		t.Errorf("expected query stripped, got %q", result)
	}
}

func TestNormalizeSiteAPIEndpointBaseUrl_StripsFragment(t *testing.T) {
	result := NormalizeSiteAPIEndpointBaseUrl("https://example.com/v1#docs")
	if result != "https://example.com/v1" {
		t.Errorf("expected fragment stripped, got %q", result)
	}
}

func TestNormalizeSiteAPIEndpointBaseUrl_Empty(t *testing.T) {
	result := NormalizeSiteAPIEndpointBaseUrl("")
	if result != "" {
		t.Errorf("expected empty for empty input, got %q", result)
	}
}

func TestIsValidAPIEndpointURL_HTTPS(t *testing.T) {
	if !IsValidAPIEndpointURL("https://api.openai.com/v1") {
		t.Error("expected valid for https URL")
	}
}

func TestIsValidAPIEndpointURL_HTTP(t *testing.T) {
	if !IsValidAPIEndpointURL("http://localhost:8080") {
		t.Error("expected valid for http URL")
	}
}

func TestIsValidAPIEndpointURL_Invalid(t *testing.T) {
	tests := []string{"", "ftp://example.com", "not-a-url"}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			if IsValidAPIEndpointURL(url) {
				t.Errorf("expected invalid for %q", url)
			}
		})
	}
}

func TestIsValidProxyURL_Valid(t *testing.T) {
	tests := []string{"http://proxy:8080", "https://proxy:8443", "socks5://proxy:1080", "socks5h://proxy:1080"}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			if !IsValidProxyURL(url) {
				t.Errorf("expected valid for %q", url)
			}
		})
	}
}

func TestIsValidProxyURL_Empty(t *testing.T) {
	if !IsValidProxyURL("") {
		t.Error("empty should be valid (no proxy)")
	}
}

func TestIsValidProxyURL_Invalid(t *testing.T) {
	if IsValidProxyURL("not-a-proxy-url") {
		t.Error("expected invalid for garbage input")
	}
}

func TestIsValidHTTPURL_Valid(t *testing.T) {
	tests := []string{"https://example.com", "http://localhost:8080"}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			if !IsValidHTTPURL(url) {
				t.Errorf("expected valid for %q", url)
			}
		})
	}
}

func TestIsValidHTTPURL_SocksInvalid(t *testing.T) {
	if IsValidHTTPURL("socks5://proxy:1080") {
		t.Error("expected socks URL to be invalid for IsValidHTTPURL")
	}
}

// ---- Normalize helpers ----

func TestNormalizeSortOrder_Valid(t *testing.T) {
	v := 5
	result := NormalizeSortOrder(&v)
	if result == nil || *result != 5 {
		t.Errorf("expected 5, got %v", result)
	}
}

func TestNormalizeSortOrder_Negative(t *testing.T) {
	v := -1
	result := NormalizeSortOrder(&v)
	if result == nil || *result != 0 {
		t.Errorf("expected 0 for negative input, got %v", result)
	}
}

func TestNormalizeSortOrder_Nil(t *testing.T) {
	result := NormalizeSortOrder(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestNormalizeGlobalWeight_Valid(t *testing.T) {
	v := 1.0
	result := NormalizeGlobalWeight(&v)
	if result == nil || *result != 1.0 {
		t.Errorf("expected 1.0, got %v", result)
	}
}

func TestNormalizeGlobalWeight_ClampMin(t *testing.T) {
	v := 0.001
	result := NormalizeGlobalWeight(&v)
	if result == nil || *result != 0.01 {
		t.Errorf("expected clamp to 0.01, got %v", *result)
	}
}

func TestNormalizeGlobalWeight_ClampMax(t *testing.T) {
	v := 200.0
	result := NormalizeGlobalWeight(&v)
	if result == nil || *result != 100.0 {
		t.Errorf("expected clamp to 100, got %v", *result)
	}
}

func TestNormalizeGlobalWeight_Zero(t *testing.T) {
	v := 0.0
	result := NormalizeGlobalWeight(&v)
	if result != nil {
		t.Errorf("expected nil for zero, got %v", *result)
	}
}

func TestNormalizeGlobalWeight_Negative(t *testing.T) {
	v := -5.0
	result := NormalizeGlobalWeight(&v)
	if result != nil {
		t.Errorf("expected nil for negative, got %v", *result)
	}
}

func TestNormalizeNullable_Value(t *testing.T) {
	s := "hello"
	result := NormalizeNullable(&s)
	if result == nil || *result != "hello" {
		t.Errorf("expected 'hello', got %v", result)
	}
}

func TestNormalizeNullable_Empty(t *testing.T) {
	s := ""
	result := NormalizeNullable(&s)
	if result != nil {
		t.Errorf("expected nil for empty string, got %v", result)
	}
}

func TestNormalizeNullable_Nil(t *testing.T) {
	result := NormalizeNullable(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestIsForbiddenSiteTargetURL(t *testing.T) {
	forbid := []string{
		"http://169.254.169.254/latest/meta-data",
		"https://169.254.169.254/",
		"http://metadata.google.internal/",
		"http://[fe80::1]/",
	}
	allow := []string{
		"https://api.openai.com/v1",
		"http://10.0.0.5:8080",
		"http://192.168.1.10",
		"http://127.0.0.1:4000",
		"http://localhost:8080",
		"",
	}
	for _, u := range forbid {
		if !IsForbiddenSiteTargetURL(u) {
			t.Errorf("expected forbidden: %q", u)
		}
	}
	for _, u := range allow {
		if IsForbiddenSiteTargetURL(u) {
			t.Errorf("expected allowed: %q", u)
		}
	}
}

func TestIsValidHTTPURL_RejectsMetadata(t *testing.T) {
	forbid := []string{
		"http://169.254.169.254/latest/meta-data",
		"https://metadata.google.internal/",
		"http://[fe80::1]/",
	}
	for _, u := range forbid {
		if IsValidHTTPURL(u) {
			t.Errorf("expected invalid for metadata URL %q", u)
		}
	}
	if !IsValidHTTPURL("https://example.com/checkin") {
		t.Error("public URL should stay valid")
	}
	if !IsValidHTTPURL("") {
		t.Error("empty should stay valid")
	}
	if !IsValidHTTPURL("http://10.0.0.5/checkin") {
		t.Error("RFC1918 should remain valid for lab external checkin")
	}
}
