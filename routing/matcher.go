package routing

import (
	"regexp"
	"strings"
)

// IsRegexModelPattern returns true if the pattern is a regex pattern (starts with "re:").
func IsRegexModelPattern(pattern string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(pattern)), "re:")
}

// ParseRegexModelPattern parses a "re:..." pattern into a compiled regexp.
func ParseRegexModelPattern(pattern string) *regexp.Regexp {
	raw := strings.TrimSpace(pattern)
	if !IsRegexModelPattern(raw) {
		return nil
	}
	reBody := strings.TrimPrefix(raw, "re:")
	reBody = strings.TrimPrefix(reBody, "RE:")
	reBody = strings.TrimSpace(reBody)
	if reBody == "" {
		return nil
	}
	re, err := regexp.Compile(reBody)
	if err != nil {
		return nil
	}
	return re
}

// IsExactRouteModelPattern returns true if the pattern is an exact model name (no wildcards, no "re:" prefix).
func IsExactRouteModelPattern(pattern string) bool {
	normalized := strings.TrimSpace(pattern)
	if normalized == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(normalized), "re:") {
		return false
	}
	return !strings.ContainsAny(normalized, "*?")
}

// MatchesModelPattern checks whether a model string matches a pattern (exact, glob, or regex).
func MatchesModelPattern(model, pattern string) bool {
	normalizedPattern := strings.TrimSpace(pattern)
	if normalizedPattern == "" {
		return false
	}
	normalizedModel := strings.TrimSpace(model)
	if normalizedModel == "" {
		return false
	}

	// Exact match
	if normalizedPattern == normalizedModel {
		return true
	}

	// Regex patterns
	if IsRegexModelPattern(normalizedPattern) {
		re := ParseRegexModelPattern(normalizedPattern)
		if re != nil {
			return re.MatchString(normalizedModel)
		}
		return false
	}

	// Glob pattern matching (supports * and ? wildcards)
	return globMatch(normalizedPattern, normalizedModel)
}

// globMatch implements simple glob matching with * and ?.
func globMatch(pattern, value string) bool {
	p, v := 0, 0
	pLen, vLen := len(pattern), len(value)
	pStar, vStar := -1, 0

	for v < vLen {
		if p < pLen && (pattern[p] == '?' || pattern[p] == value[v]) {
			p++
			v++
		} else if p < pLen && pattern[p] == '*' {
			pStar = p
			vStar = v
			p++
		} else if pStar != -1 {
			p = pStar + 1
			vStar++
			v = vStar
		} else {
			return false
		}
	}

	for p < pLen && pattern[p] == '*' {
		p++
	}
	return p == pLen
}

// NormalizeModelAlias strips the vendor prefix and lowercases.
// E.g. "anthropic/claude-sonnet" → "claude-sonnet"
func NormalizeModelAlias(modelName string) string {
	normalized := strings.ToLower(strings.TrimSpace(modelName))
	if normalized == "" {
		return ""
	}
	slashIndex := strings.LastIndex(normalized, "/")
	if slashIndex >= 0 && slashIndex < len(normalized)-1 {
		return normalized[slashIndex+1:]
	}
	return normalized
}

// IsModelAliasEquivalent checks if two model names are equivalent after alias normalization.
func IsModelAliasEquivalent(left, right string) bool {
	a := NormalizeModelAlias(left)
	b := NormalizeModelAlias(right)
	return a != "" && b != "" && a == b
}

// ChannelSupportsRequestedModel checks if a channel's source model supports the requested model.
func ChannelSupportsRequestedModel(channelSourceModel *string, requestedModel string) bool {
	if channelSourceModel == nil {
		return true
	}
	source := strings.TrimSpace(*channelSourceModel)
	if source == "" {
		return true
	}
	if source == requestedModel {
		return true
	}
	if IsModelAliasEquivalent(source, requestedModel) {
		return true
	}
	if MatchesModelPattern(requestedModel, source) {
		return true
	}
	return false
}

// NormalizeRouteDisplayName trims whitespace from a display name.
func NormalizeRouteDisplayName(displayName *string) string {
	if displayName == nil {
		return ""
	}
	return strings.TrimSpace(*displayName)
}

// IsRouteDisplayNameMatch checks if a model matches a route's display name exactly.
func IsRouteDisplayNameMatch(model string, displayName *string) bool {
	alias := NormalizeRouteDisplayName(displayName)
	return alias != "" && alias == model
}

// IsExplicitGroupRoute checks if a route is an explicit_group route.
func IsExplicitGroupRoute(routeMode string) bool {
	return routeMode == "explicit_group"
}

// HasCustomDisplayName checks if a route has a non-default display name.
func HasCustomDisplayName(modelPattern string, displayName *string) bool {
	dn := NormalizeRouteDisplayName(displayName)
	mp := strings.TrimSpace(modelPattern)
	return dn != "" && dn != mp
}

// GetExposedModelNameForRoute returns the display name or model pattern.
func GetExposedModelNameForRoute(displayName *string, modelPattern string) string {
	name := NormalizeRouteDisplayName(displayName)
	if name != "" {
		return name
	}
	return modelPattern
}

// IsModelAllowedByDownstreamPolicy checks whether a model is allowed by the downstream routing policy.
func IsModelAllowedByDownstreamPolicy(requestedModel string, policy DownstreamRoutingPolicy) bool {
	hasSupportedPatterns := len(policy.SupportedModels) > 0
	hasAllowedRoutes := len(policy.AllowedRouteIDs) > 0

	if !hasSupportedPatterns && !hasAllowedRoutes {
		return !policy.DenyAllWhenEmpty
	}

	matchedSupportedPattern := false
	for _, pattern := range policy.SupportedModels {
		if MatchesModelPattern(requestedModel, pattern) {
			matchedSupportedPattern = true
			break
		}
	}
	if matchedSupportedPattern {
		return true
	}
	if hasAllowedRoutes {
		return true
	}
	return false
}

// ParseModelMappingRecord parses a model mapping JSON string into a map.
func ParseModelMappingRecord(raw *string) map[string]string {
	if raw == nil {
		return nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" || s[0] != '{' {
		return nil
	}
	// Simple JSON parse for string-to-string mapping.
	result := make(map[string]string)
	// Strip outer braces
	inner := s[1 : len(s)-1]
	if inner == "" {
		return nil
	}
	parts := splitJSONPairs(inner)
	for _, part := range parts {
		kv := splitJSONPair(part)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// splitJSONPairs splits a JSON object body into top-level key:value pairs.
// Simple implementation for model mapping JSON.
func splitJSONPairs(s string) []string {
	var result []string
	depth := 0
	start := 0
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
		}
		if inString {
			continue
		}
		if c == '{' || c == '[' {
			depth++
		} else if c == '}' || c == ']' {
			depth--
		} else if c == ',' && depth == 0 {
			part := strings.TrimSpace(s[start:i])
			if part != "" {
				result = append(result, part)
			}
			start = i + 1
		}
	}
	part := strings.TrimSpace(s[start:])
	if part != "" {
		result = append(result, part)
	}
	return result
}

// splitJSONPair splits a single "key":"value" pair.
func splitJSONPair(s string) []string {
	// Find the first colon outside strings
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
		}
		if !inString && c == ':' {
			key := unquoteJSON(strings.TrimSpace(s[:i]))
			val := unquoteJSON(strings.TrimSpace(s[i+1:]))
			return []string{key, val}
		}
	}
	return nil
}

// unquoteJSON removes surrounding double quotes and unescapes.
func unquoteJSON(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		inner = strings.ReplaceAll(inner, "\\\"", "\"")
		inner = strings.ReplaceAll(inner, "\\\\", "\\")
		inner = strings.ReplaceAll(inner, "\\n", "\n")
		inner = strings.ReplaceAll(inner, "\\t", "\t")
		inner = strings.ReplaceAll(inner, "\\/", "/")
		return inner
	}
	return s
}

// ResolveMappedModel resolves the target model name from a model mapping.
func ResolveMappedModel(requestedModel string, modelMapping *string) string {
	parsed := ParseModelMappingRecord(modelMapping)
	if parsed == nil {
		return requestedModel
	}

	// Exact key match first
	if target, ok := parsed[requestedModel]; ok {
		return strings.TrimSpace(target)
	}

	// Pattern match fallback
	for pattern, target := range parsed {
		if MatchesModelPattern(requestedModel, pattern) {
			return strings.TrimSpace(target)
		}
	}

	return requestedModel
}

// NormalizeChannelSourceModel trims whitespace from a channel source model.
func NormalizeChannelSourceModel(channelSourceModel *string) string {
	if channelSourceModel == nil {
		return ""
	}
	return strings.TrimSpace(*channelSourceModel)
}

// ResolveActualModelForSelectedChannel resolves the actual model to forward.
func ResolveActualModelForSelectedChannel(
	requestedModel string,
	routeDisplayName *string,
	mappedModel string,
	channelSourceModel *string,
) string {
	sourceModel := NormalizeChannelSourceModel(channelSourceModel)
	if IsRouteDisplayNameMatch(requestedModel, routeDisplayName) && sourceModel != "" {
		return sourceModel
	}
	return mappedModel
}
