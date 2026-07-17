package service

import (
	"net/url"
	"strings"
)

// DetectResult is the result of platform detection.
type DetectResult struct {
	URL                    string  `json:"url"`
	Platform               string  `json:"platform"`
	InitializationPresetID *string `json:"initializationPresetId,omitempty"`
}

// DetectSite attempts to detect the platform for a given URL.
// When a site initialization preset matches the URL, the result uses the
// preset protocol family (openai/claude) and includes initializationPresetId.
// Otherwise falls back to hostname heuristics for vendor/platform tags.
func DetectSite(rawURL string) *DetectResult {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		// Allow scheme-less inputs such as "api.deepseek.com/v1".
		if withScheme, schemeErr := url.Parse("https://" + trimmed); schemeErr == nil {
			parsed = withScheme
			err = nil
		}
	}
	if err != nil || parsed == nil || parsed.Host == "" {
		return nil
	}

	// CanonicalURL: normalize the URL for detection purposes
	canonicalURL := parsed.String()
	host := strings.ToLower(parsed.Hostname())

	// Prefer initialization presets (protocol family + preset id) when the
	// host/path rules match. This keeps createSite/detectSite aligned with the
	// frontend siteInitializationPresets registry.
	if preset := DetectSiteInitializationPreset(trimmed, ""); preset != nil {
		id := preset.ID
		return &DetectResult{
			URL:                    canonicalURL,
			Platform:               preset.Platform,
			InitializationPresetID: &id,
		}
	}

	// Heuristic detection based on hostname patterns
	// P4 will replace this with real adapter-based detection.
	var platform string
	switch {
	case strings.Contains(host, "api.openai.com") || strings.Contains(host, "openai.com"):
		platform = "openai"
	case strings.Contains(host, "api.anthropic.com") || strings.Contains(host, "anthropic.com"):
		platform = "anthropic"
	case strings.Contains(host, "generativelanguage.googleapis.com") || strings.Contains(host, "ai.google.dev"):
		platform = "gemini"
	case strings.Contains(host, "api.deepseek.com") || strings.Contains(host, "deepseek.com"):
		platform = "deepseek"
	case strings.Contains(host, "api.moonshot.cn") || strings.Contains(host, "moonshot.cn"):
		platform = "moonshot"
	case strings.Contains(host, "dashscope.aliyuncs.com") || strings.Contains(host, "dashscope"):
		platform = "dashscope"
	case strings.Contains(host, "api.baichuan-ai.com"):
		platform = "baichuan"
	case strings.Contains(host, "api.zhipuai.cn") || strings.Contains(host, "bigmodel.cn"):
		platform = "zhipu"
	case strings.Contains(host, "api.minimax.chat") || strings.Contains(host, "minimax"):
		platform = "minimax"
	case strings.Contains(host, "api.stepfun.com") || strings.Contains(host, "stepfun"):
		platform = "stepfun"
	case strings.Contains(host, "ark.cn-beijing.volces.com") || strings.Contains(host, "volcengine.com"):
		platform = "bytedance"
	case strings.Contains(host, "api.siliconflow.cn") || strings.Contains(host, "siliconflow"):
		platform = "siliconflow"
	case strings.Contains(host, "api-inference.modelscope.cn"):
		platform = "modelscope"
	case strings.Contains(host, "api.mistral.ai"):
		platform = "mistral"
	case strings.Contains(host, "api.cohere.ai") || strings.Contains(host, "cohere.com"):
		platform = "cohere"
	case strings.Contains(host, "api.together.xyz") || strings.Contains(host, "together.xyz"):
		platform = "together"
	case strings.Contains(host, "api.fireworks.ai"):
		platform = "fireworks"
	case strings.Contains(host, "api.groq.com"):
		platform = "groq"
	case strings.Contains(host, "api.perplexity.ai"):
		platform = "perplexity"
	case strings.Contains(host, "api.x.ai") || strings.Contains(host, "x.ai"):
		platform = "xai"
	case strings.Contains(host, "api-inference.huggingface.co") || strings.Contains(host, "hf.space"):
		platform = "huggingface"
	case strings.Contains(host, "azure.com") || strings.Contains(host, "openai.azure.com"):
		platform = "azure"
	case strings.Contains(host, ".github.com") && (strings.Contains(parsed.Path, "openai") || strings.Contains(parsed.Path, "copilot")):
		platform = "github-copilot"
	case strings.Contains(host, "claude.ai"):
		platform = "claude"
	case strings.Contains(host, "aistudio.google.com") || strings.Contains(host, "makersuite.google.com"):
		platform = "gemini"
	default:
		// Check for common NewAPI/OneAPI patterns
		if strings.Contains(host, "anyrouter") || strings.Contains(parsed.Path, "anyrouter") {
			platform = "anyrouter"
		} else if strings.Contains(host, "oneapi") || strings.Contains(host, "new-api") || strings.Contains(host, "newapi") {
			platform = "new-api"
		}
	}

	if platform == "" {
		return nil
	}

	result := &DetectResult{URL: canonicalURL, Platform: platform}
	// Optional: attach a preset when hostname heuristics already chose a vendor
	// platform and a defaultUrl fallback matches under that protocol family.
	// Prefer the detected protocol family when the heuristic platform itself is
	// already openai/claude; otherwise leave preset empty (frontend can still
	// call DetectSiteInitializationPreset client-side).
	if platform == "openai" || platform == "claude" {
		if preset := DetectSiteInitializationPreset(trimmed, platform); preset != nil {
			id := preset.ID
			result.InitializationPresetID = &id
		}
	}
	return result
}

// CanonicalizeSiteURL returns a canonical URL for persistence.
// Mirrors TS analyzePrimarySiteUrl().persistedUrl behavior.
func CanonicalizeSiteURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return strings.TrimRight(trimmed, "/")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}
