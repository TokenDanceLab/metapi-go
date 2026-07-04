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
// Stub implementation: P4 platform adapters will provide real detection.
// Currently returns nil (unknown platform) for all URLs.
func DetectSite(rawURL string) *DetectResult {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil
	}

	// CanonicalURL: normalize the URL for detection purposes
	canonicalURL := parsed.String()
	host := strings.ToLower(parsed.Hostname())

	// Heuristic detection based on hostname patterns
	// P4 will replace this with real adapter-based detection.
	switch {
	case strings.Contains(host, "api.openai.com") || strings.Contains(host, "openai.com"):
		return &DetectResult{URL: canonicalURL, Platform: "openai"}
	case strings.Contains(host, "api.anthropic.com") || strings.Contains(host, "anthropic.com"):
		return &DetectResult{URL: canonicalURL, Platform: "anthropic"}
	case strings.Contains(host, "generativelanguage.googleapis.com") || strings.Contains(host, "ai.google.dev"):
		return &DetectResult{URL: canonicalURL, Platform: "gemini"}
	case strings.Contains(host, "api.deepseek.com") || strings.Contains(host, "deepseek.com"):
		return &DetectResult{URL: canonicalURL, Platform: "deepseek"}
	case strings.Contains(host, "api.moonshot.cn") || strings.Contains(host, "moonshot.cn"):
		return &DetectResult{URL: canonicalURL, Platform: "moonshot"}
	case strings.Contains(host, "dashscope.aliyuncs.com") || strings.Contains(host, "dashscope"):
		return &DetectResult{URL: canonicalURL, Platform: "dashscope"}
	case strings.Contains(host, "api.baichuan-ai.com"):
		return &DetectResult{URL: canonicalURL, Platform: "baichuan"}
	case strings.Contains(host, "api.zhipuai.cn") || strings.Contains(host, "bigmodel.cn"):
		return &DetectResult{URL: canonicalURL, Platform: "zhipu"}
	case strings.Contains(host, "api.minimax.chat") || strings.Contains(host, "minimax"):
		return &DetectResult{URL: canonicalURL, Platform: "minimax"}
	case strings.Contains(host, "api.stepfun.com") || strings.Contains(host, "stepfun"):
		return &DetectResult{URL: canonicalURL, Platform: "stepfun"}
	case strings.Contains(host, "ark.cn-beijing.volces.com") || strings.Contains(host, "volcengine.com"):
		return &DetectResult{URL: canonicalURL, Platform: "bytedance"}
	case strings.Contains(host, "api.siliconflow.cn") || strings.Contains(host, "siliconflow"):
		return &DetectResult{URL: canonicalURL, Platform: "siliconflow"}
	case strings.Contains(host, "api-inference.modelscope.cn"):
		return &DetectResult{URL: canonicalURL, Platform: "modelscope"}
	case strings.Contains(host, "api.mistral.ai"):
		return &DetectResult{URL: canonicalURL, Platform: "mistral"}
	case strings.Contains(host, "api.cohere.ai") || strings.Contains(host, "cohere.com"):
		return &DetectResult{URL: canonicalURL, Platform: "cohere"}
	case strings.Contains(host, "api.together.xyz") || strings.Contains(host, "together.xyz"):
		return &DetectResult{URL: canonicalURL, Platform: "together"}
	case strings.Contains(host, "api.fireworks.ai"):
		return &DetectResult{URL: canonicalURL, Platform: "fireworks"}
	case strings.Contains(host, "api.groq.com"):
		return &DetectResult{URL: canonicalURL, Platform: "groq"}
	case strings.Contains(host, "api.perplexity.ai"):
		return &DetectResult{URL: canonicalURL, Platform: "perplexity"}
	case strings.Contains(host, "api.x.ai") || strings.Contains(host, "x.ai"):
		return &DetectResult{URL: canonicalURL, Platform: "xai"}
	case strings.Contains(host, "api-inference.huggingface.co") || strings.Contains(host, "hf.space"):
		return &DetectResult{URL: canonicalURL, Platform: "huggingface"}
	case strings.Contains(host, "azure.com") || strings.Contains(host, "openai.azure.com"):
		return &DetectResult{URL: canonicalURL, Platform: "azure"}
	case strings.Contains(host, ".github.com") && (strings.Contains(parsed.Path, "openai") || strings.Contains(parsed.Path, "copilot")):
		return &DetectResult{URL: canonicalURL, Platform: "github-copilot"}
	case strings.Contains(host, "claude.ai"):
		return &DetectResult{URL: canonicalURL, Platform: "claude"}
	case strings.Contains(host, "aistudio.google.com") || strings.Contains(host, "makersuite.google.com"):
		return &DetectResult{URL: canonicalURL, Platform: "gemini"}
	default:
		// Check for common NewAPI/OneAPI patterns
		if strings.Contains(host, "oneapi") || strings.Contains(host, "new-api") || strings.Contains(host, "newapi") {
			return &DetectResult{URL: canonicalURL, Platform: "new-api"}
		}
	}

	return nil
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
