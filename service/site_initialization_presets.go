package service

import (
	"fmt"
	"net/url"
	"strings"
)

// SiteInitializationPreset mirrors web/shared/siteInitializationPresets.js.
// Platform values are protocol families (openai/claude), not vendor brand tags.
type SiteInitializationPreset struct {
	ID                         string
	Label                      string
	ProviderLabel              string
	Description                string
	Platform                   string
	DefaultURL                 string
	InitialSegment             string
	RecommendedSkipModelFetch  bool
	RecommendedModels          []string
	DocsURL                    string
	// MatchHost + MatchPaths define host/path auto-detect rules.
	// Empty MatchHost means the preset is manual-only (matches always false).
	MatchHost  string
	MatchPaths []string
}

var codingPlanRecommendedModels = []string{
	"qwen3-coder-plus",
	"qwen3-coder-next",
	"qwen3.5-plus",
	"glm-5",
}

var zhipuCodingPlanRecommendedModels = []string{
	"glm-4.7",
	"glm-4.6",
	"glm-4.5",
	"glm-4.5-air",
}

var deepseekRecommendedModels = []string{
	"deepseek-chat",
	"deepseek-reasoner",
}

var moonshotRecommendedModels = []string{
	"kimi-k2.5",
	"kimi-k2",
	"kimi-k2-thinking",
}

var minimaxRecommendedModels = []string{
	"MiniMax-M2.7",
	"MiniMax-M2.5",
	"MiniMax-M2.1",
}

var modelscopeRecommendedModels = []string{
	"Qwen/Qwen3-32B",
	"Qwen/Qwen2.5-Coder-32B-Instruct",
	"deepseek-ai/DeepSeek-V3.2",
}

var doubaoCodingRecommendedModels = []string{
	"ark-code-latest",
	"doubao-seed-2.0-code",
	"doubao-seed-2.0-pro",
}

// siteInitializationPresets is the SSOT ported from siteInitializationPresets.js.
// Order matters for DetectSiteInitializationPreset first-match behavior.
var siteInitializationPresets = []SiteInitializationPreset{
	{
		ID:                        "codingplan-openai",
		Label:                     "阿里云 CodingPlan / OpenAI",
		ProviderLabel:             "阿里云 CodingPlan",
		Description:               "适合阿里云 CodingPlan 的 OpenAI 兼容入口，建议先添加 API Key，再补入推荐模型完成初始化。",
		Platform:                  "openai",
		DefaultURL:                "https://coding.dashscope.aliyuncs.com/v1",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         codingPlanRecommendedModels,
		DocsURL:                   "https://help.aliyun.com/zh/model-studio/coding-plan-faq",
		MatchHost:                 "coding.dashscope.aliyuncs.com",
		MatchPaths:                []string{"/v1"},
	},
	{
		ID:                        "codingplan-claude",
		Label:                     "阿里云 CodingPlan / Claude",
		ProviderLabel:             "阿里云 CodingPlan",
		Description:               "适合阿里云 CodingPlan 的 Claude 兼容入口，建议先添加 API Key，再补入推荐模型完成初始化。",
		Platform:                  "claude",
		DefaultURL:                "https://coding.dashscope.aliyuncs.com/apps/anthropic",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         codingPlanRecommendedModels,
		DocsURL:                   "https://help.aliyun.com/zh/model-studio/coding-plan-faq",
		MatchHost:                 "coding.dashscope.aliyuncs.com",
		MatchPaths:                []string{"/apps/anthropic"},
	},
	{
		ID:                        "zhipu-coding-plan-openai",
		Label:                     "智谱 Coding Plan / OpenAI",
		ProviderLabel:             "智谱 Coding Plan",
		Description:               "适合智谱 Coding Plan 的 OpenAI 兼容入口，建议先添加 API Key，再补入常用 GLM 编程模型。",
		Platform:                  "openai",
		DefaultURL:                "https://open.bigmodel.cn/api/coding/paas/v4",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         zhipuCodingPlanRecommendedModels,
		DocsURL:                   "https://docs.bigmodel.cn/cn/coding-plan/faq",
		MatchHost:                 "open.bigmodel.cn",
		MatchPaths:                []string{"/api/coding/paas/v4"},
	},
	{
		ID:                        "zhipu-coding-plan-claude",
		Label:                     "智谱 Coding Plan / Claude",
		ProviderLabel:             "智谱 Coding Plan",
		Description:               "适合智谱 Coding Plan 的 Claude 兼容入口。由于该地址也可作为通用兼容入口，这里默认只提供手动预设，不按 URL 强制自动识别。",
		Platform:                  "claude",
		DefaultURL:                "https://open.bigmodel.cn/api/anthropic",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         zhipuCodingPlanRecommendedModels,
		DocsURL:                   "https://docs.bigmodel.cn/cn/coding-plan/faq",
		// Manual-only: matches() always returns false in the JS registry.
	},
	{
		ID:                        "deepseek-openai",
		Label:                     "DeepSeek / OpenAI",
		ProviderLabel:             "DeepSeek",
		Description:               "适合 DeepSeek 官方 OpenAI 兼容入口，建议直接添加 API Key，并优先补入官方常用编程模型。",
		Platform:                  "openai",
		DefaultURL:                "https://api.deepseek.com/v1",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         deepseekRecommendedModels,
		DocsURL:                   "https://api-docs.deepseek.com/",
		MatchHost:                 "api.deepseek.com",
		MatchPaths:                []string{"/", "/v1"},
	},
	{
		ID:                        "deepseek-claude",
		Label:                     "DeepSeek / Claude",
		ProviderLabel:             "DeepSeek",
		Description:               "适合 DeepSeek 官方 Anthropic 兼容入口，便于 Claude Code 一类工具直接接入。",
		Platform:                  "claude",
		DefaultURL:                "https://api.deepseek.com/anthropic",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         deepseekRecommendedModels,
		DocsURL:                   "https://api-docs.deepseek.com/guides/anthropic_api",
		MatchHost:                 "api.deepseek.com",
		MatchPaths:                []string{"/anthropic"},
	},
	{
		ID:                        "moonshot-openai",
		Label:                     "Moonshot(Kimi) / OpenAI",
		ProviderLabel:             "Moonshot / Kimi",
		Description:               "适合 Moonshot 官方 OpenAI 兼容入口，推荐优先使用 Kimi 系列编程与 Agent 模型。",
		Platform:                  "openai",
		DefaultURL:                "https://api.moonshot.cn/v1",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         moonshotRecommendedModels,
		DocsURL:                   "https://platform.moonshot.cn/",
		MatchHost:                 "api.moonshot.cn",
		MatchPaths:                []string{"/", "/v1"},
	},
	{
		ID:                        "moonshot-claude",
		Label:                     "Moonshot(Kimi) / Claude",
		ProviderLabel:             "Moonshot / Kimi",
		Description:               "适合 Moonshot 官方 Anthropic 兼容入口，便于 Claude Code 与同类工具接入 Kimi。",
		Platform:                  "claude",
		DefaultURL:                "https://api.moonshot.cn/anthropic",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         moonshotRecommendedModels,
		DocsURL:                   "https://platform.moonshot.cn/blog/posts/kimi-k2-0905",
		MatchHost:                 "api.moonshot.cn",
		MatchPaths:                []string{"/anthropic"},
	},
	{
		ID:                        "minimax-openai",
		Label:                     "MiniMax / OpenAI",
		ProviderLabel:             "MiniMax",
		Description:               "适合 MiniMax 官方 OpenAI 兼容入口，建议直接添加 API Key 后补入常用 M2 编程模型。",
		Platform:                  "openai",
		DefaultURL:                "https://api.minimaxi.com/v1",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         minimaxRecommendedModels,
		DocsURL:                   "https://platform.minimaxi.com/docs/api-reference/api-overview",
		MatchHost:                 "api.minimaxi.com",
		MatchPaths:                []string{"/", "/v1"},
	},
	{
		ID:                        "minimax-claude",
		Label:                     "MiniMax / Claude",
		ProviderLabel:             "MiniMax",
		Description:               "适合 MiniMax 官方 Anthropic 兼容入口，适配 Claude Code 等编程工具场景。",
		Platform:                  "claude",
		DefaultURL:                "https://api.minimaxi.com/anthropic",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         minimaxRecommendedModels,
		DocsURL:                   "https://platform.minimaxi.com/docs/api-reference/text-anthropic-api",
		MatchHost:                 "api.minimaxi.com",
		MatchPaths:                []string{"/anthropic"},
	},
	{
		ID:                        "modelscope-openai",
		Label:                     "ModelScope / OpenAI",
		ProviderLabel:             "ModelScope",
		Description:               "适合 ModelScope API-Inference 的 OpenAI 兼容入口，适合直接接入常用开源编程模型。",
		Platform:                  "openai",
		DefaultURL:                "https://api-inference.modelscope.cn/v1",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         modelscopeRecommendedModels,
		DocsURL:                   "https://www.modelscope.cn/docs/model-service/API-Inference/intro",
		MatchHost:                 "api-inference.modelscope.cn",
		MatchPaths:                []string{"/v1"},
	},
	{
		ID:                        "modelscope-claude",
		Label:                     "ModelScope / Claude",
		ProviderLabel:             "ModelScope",
		Description:               "适合 ModelScope API-Inference 的 Claude 兼容入口，便于接入 Claude Code 一类工具。",
		Platform:                  "claude",
		DefaultURL:                "https://api-inference.modelscope.cn",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         modelscopeRecommendedModels,
		DocsURL:                   "https://www.modelscope.cn/docs/model-service/API-Inference/intro",
		MatchHost:                 "api-inference.modelscope.cn",
		MatchPaths:                []string{"/"},
	},
	{
		ID:                        "doubao-coding-openai",
		Label:                     "豆包 Coding Plan / OpenAI",
		ProviderLabel:             "豆包 Coding Plan",
		Description:               "适合火山方舟 Coding Plan 的 OpenAI 兼容入口，推荐优先使用 ark-code 与豆包编程模型。",
		Platform:                  "openai",
		DefaultURL:                "https://ark.cn-beijing.volces.com/api/coding/v3",
		InitialSegment:            "apikey",
		RecommendedSkipModelFetch: true,
		RecommendedModels:         doubaoCodingRecommendedModels,
		DocsURL:                   "https://www.volcengine.com/docs/82379/2205646?lang=zh",
		MatchHost:                 "ark.cn-beijing.volces.com",
		MatchPaths:                []string{"/api/coding/v3"},
	},
}

func cloneSiteInitializationPreset(preset SiteInitializationPreset) SiteInitializationPreset {
	out := preset
	if len(preset.RecommendedModels) > 0 {
		out.RecommendedModels = append([]string(nil), preset.RecommendedModels...)
	}
	if len(preset.MatchPaths) > 0 {
		out.MatchPaths = append([]string(nil), preset.MatchPaths...)
	}
	return out
}

// ListSiteInitializationPresets returns a defensive copy of all known presets.
func ListSiteInitializationPresets() []SiteInitializationPreset {
	out := make([]SiteInitializationPreset, 0, len(siteInitializationPresets))
	for _, preset := range siteInitializationPresets {
		out = append(out, cloneSiteInitializationPreset(preset))
	}
	return out
}

// GetSiteInitializationPreset returns a preset by id, or nil when unknown.
func GetSiteInitializationPreset(id string) *SiteInitializationPreset {
	normalizedID := strings.TrimSpace(id)
	if normalizedID == "" {
		return nil
	}
	for _, preset := range siteInitializationPresets {
		if preset.ID == normalizedID {
			cloned := cloneSiteInitializationPreset(preset)
			return &cloned
		}
	}
	return nil
}

// DetectSiteInitializationPreset mirrors the JS detectSiteInitializationPreset(url, platform).
// When platform is non-empty, only presets for that protocol family are considered.
func DetectSiteInitializationPreset(rawURL, platform string) *SiteInitializationPreset {
	normalizedPlatform := strings.TrimSpace(strings.ToLower(platform))

	for _, preset := range siteInitializationPresets {
		if normalizedPlatform != "" && preset.Platform != normalizedPlatform {
			continue
		}
		if preset.MatchesURL(rawURL) {
			cloned := cloneSiteInitializationPreset(preset)
			return &cloned
		}
	}

	if normalizedPlatform == "" {
		return nil
	}

	analyzed := AnalyzePrimarySiteURL(rawURL)
	if analyzed.PersistedURL == "" {
		return nil
	}
	for _, preset := range siteInitializationPresets {
		if preset.Platform != normalizedPlatform {
			continue
		}
		if strings.TrimSpace(preset.DefaultURL) == "" {
			continue
		}
		presetAnalyzed := AnalyzePrimarySiteURL(preset.DefaultURL)
		if presetAnalyzed.PersistedURL == "" {
			continue
		}
		if presetAnalyzed.PersistedURL == analyzed.PersistedURL {
			cloned := cloneSiteInitializationPreset(preset)
			return &cloned
		}
	}
	return nil
}

// HasURLMatchRules reports whether the preset defines host/path auto-detect rules.
func (p SiteInitializationPreset) HasURLMatchRules() bool {
	return strings.TrimSpace(p.MatchHost) != "" && len(p.MatchPaths) > 0
}

// MatchesURL implements the JS preset.matches(url) host+paths check.
func (p SiteInitializationPreset) MatchesURL(rawURL string) bool {
	if !p.HasURLMatchRules() {
		return false
	}
	parsed := parseURLCandidate(rawURL)
	if parsed == nil {
		return false
	}
	if !strings.EqualFold(parsed.Hostname(), p.MatchHost) {
		return false
	}
	path := normalizePresetPathname(parsed.Path)
	for _, allowed := range p.MatchPaths {
		if path == normalizePresetPathname(allowed) {
			return true
		}
	}
	return false
}

// ValidateSiteInitializationPreset checks createSite initializationPresetId.
// Empty id is allowed (optional field). When set:
//  1. preset must exist
//  2. preset.platform must equal the resolved site platform
//  3. when the preset has host/path match rules, the URL must match those rules
//     (or the defaultUrl persisted-url fallback when platform-filtered).
func ValidateSiteInitializationPreset(presetID, platform, rawURL string) error {
	normalizedID := strings.TrimSpace(presetID)
	if normalizedID == "" {
		return nil
	}
	preset := GetSiteInitializationPreset(normalizedID)
	if preset == nil {
		return fmt.Errorf("Unknown initializationPresetId %q.", normalizedID)
	}

	normalizedPlatform := strings.TrimSpace(strings.ToLower(platform))
	if normalizedPlatform == "" || preset.Platform != normalizedPlatform {
		return fmt.Errorf("initializationPresetId %q does not match platform %q.", normalizedID, normalizedPlatform)
	}

	if !preset.HasURLMatchRules() {
		return nil
	}

	// Direct host/path match.
	if preset.MatchesURL(rawURL) {
		return nil
	}

	// Fallback: same persisted primary URL as the preset default (platform-filtered).
	analyzed := AnalyzePrimarySiteURL(rawURL)
	presetAnalyzed := AnalyzePrimarySiteURL(preset.DefaultURL)
	if analyzed.PersistedURL != "" && presetAnalyzed.PersistedURL != "" && analyzed.PersistedURL == presetAnalyzed.PersistedURL {
		return nil
	}

	return fmt.Errorf("initializationPresetId %q does not match site URL.", normalizedID)
}

func parseURLCandidate(rawURL string) *url.URL {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil
	}
	candidates := []string{trimmed}
	if !strings.Contains(trimmed, "://") {
		candidates = []string{"https://" + trimmed}
	}
	for _, candidate := range candidates {
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Host == "" {
			continue
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme != "http" && scheme != "https" {
			continue
		}
		return parsed
	}
	return nil
}

func normalizePresetPathname(pathname string) string {
	normalized := strings.TrimSpace(pathname)
	if normalized == "" {
		return "/"
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	for len(normalized) > 1 && strings.HasSuffix(normalized, "/") {
		normalized = strings.TrimSuffix(normalized, "/")
	}
	return normalized
}

// PrimarySiteURLAnalysis mirrors web/shared/sitePrimaryUrl.js analyzePrimarySiteUrl.
type PrimarySiteURLAnalysis struct {
	CanonicalURL string
	PersistedURL string
	MatchedPath  string
	Action       string
}

var autoStripPrimarySitePaths = map[string]struct{}{
	"/v1":                   {},
	"/v1beta":               {},
	"/v1/models":            {},
	"/v1/chat/completions":  {},
	"/v1/responses":         {},
	"/v1/messages":          {},
	"/v1beta/models":        {},
}

var semanticPrimarySitePaths = map[string]struct{}{
	"/backend-api/codex":    {},
	"/anthropic":            {},
	"/apps/anthropic":       {},
	"/api/anthropic":        {},
	"/api/coding/paas/v4":   {},
	"/v1beta/openai":        {},
}

// AnalyzePrimarySiteURL ports analyzePrimarySiteUrl for preset fallback matching.
func AnalyzePrimarySiteURL(rawURL string) PrimarySiteURLAnalysis {
	parsed := parseURLCandidate(rawURL)
	if parsed == nil {
		return PrimarySiteURLAnalysis{Action: "invalid_url"}
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	matchedPath := normalizePresetPathname(parsed.Path)
	origin := originFromURL(parsed)
	canonicalURL := origin
	if matchedPath != "/" {
		canonicalURL = origin + matchedPath
	}

	if matchedPath == "/" {
		return PrimarySiteURLAnalysis{
			CanonicalURL: canonicalURL,
			PersistedURL: canonicalURL,
			MatchedPath:  matchedPath,
			Action:       "unchanged",
		}
	}
	if _, ok := semanticPrimarySitePaths[matchedPath]; ok {
		return PrimarySiteURLAnalysis{
			CanonicalURL: canonicalURL,
			PersistedURL: canonicalURL,
			MatchedPath:  matchedPath,
			Action:       "preserve_semantic_path",
		}
	}
	if _, ok := autoStripPrimarySitePaths[matchedPath]; ok {
		return PrimarySiteURLAnalysis{
			CanonicalURL: canonicalURL,
			PersistedURL: origin,
			MatchedPath:  matchedPath,
			Action:       "auto_strip_known_api_suffix",
		}
	}
	if strings.HasPrefix(matchedPath, "/api") {
		return PrimarySiteURLAnalysis{
			CanonicalURL: canonicalURL,
			PersistedURL: canonicalURL,
			MatchedPath:  matchedPath,
			Action:       "preserve_api_path",
		}
	}
	return PrimarySiteURLAnalysis{
		CanonicalURL: canonicalURL,
		PersistedURL: canonicalURL,
		MatchedPath:  matchedPath,
		Action:       "preserve_unknown_path",
	}
}

func originFromURL(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + parsed.Host
}
