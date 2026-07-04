package routing

import (
	"testing"
)

// =============================================================================
// Model pattern matching — 50+ test cases
// =============================================================================

func TestMatchesModelPattern_Exact(t *testing.T) {
	tests := []struct {
		model   string
		pattern string
		expect  bool
	}{
		// Exact matches
		{"gpt-4", "gpt-4", true},
		{"claude-sonnet", "claude-sonnet", true},
		{"gpt-3.5-turbo", "gpt-3.5-turbo", true},
		{"gemini-pro", "gemini-pro", true},
		// Case differences (not matching — exact only)
		{"GPT-4", "gpt-4", false},
		// Whitespace differences
		{" gpt-4", "gpt-4", true},  // matcher trims
		{"gpt-4 ", "gpt-4", true},
		// Different models
		{"gpt-4", "gpt-3.5", false},
	}

	for _, tt := range tests {
		got := MatchesModelPattern(tt.model, tt.pattern)
		if got != tt.expect {
			t.Errorf("MatchesModelPattern(%q, %q) = %v, expected %v",
				tt.model, tt.pattern, got, tt.expect)
		}
	}
}

func TestMatchesModelPattern_Glob(t *testing.T) {
	tests := []struct {
		model   string
		pattern string
		expect  bool
	}{
		// Wildcard: single *
		{"gpt-4", "gpt-*", true},
		{"gpt-3.5-turbo", "gpt-*", true},
		{"claude-sonnet", "gpt-*", false},
		{"gpt-4-32k", "gpt-*", true},
		// Wildcard: * prefix
		{"any-model-here", "*-model-*", true},
		{"nope", "*-model-*", false},
		// Wildcard: * suffix
		{"gpt-4-32k", "gpt-4*", true},
		{"gpt-40", "gpt-4*", true},
		{"gpt-3.5", "gpt-4*", false},
		// Wildcard: ? (single char)
		{"gpt-4", "gpt-?", true},
		{"gpt-40", "gpt-?", false}, // two chars after -
		{"gpt-4-turbo", "gpt-?", false},
		// Multiple wildcards
		{"gpt-4-32k-0613", "gpt-*-*-*", true},
		{"gpt-4", "gpt-*-*-*", false},
		// Wildcard in middle
		{"anthropic-claude-sonnet", "anthropic-*-sonnet", true},
		{"anthropic-claude-haiku", "anthropic-*-sonnet", false},
		// * matches empty
		{"gpt-4", "gpt-*4", true},  // * can match empty
		{"gpt-4", "gpt-*5", false},
	}

	for _, tt := range tests {
		got := MatchesModelPattern(tt.model, tt.pattern)
		if got != tt.expect {
			t.Errorf("MatchesModelPattern(%q, %q) = %v, expected %v (glob)",
				tt.model, tt.pattern, got, tt.expect)
		}
	}
}

func TestMatchesModelPattern_Regex(t *testing.T) {
	tests := []struct {
		model   string
		pattern string
		expect  bool
	}{
		// Basic regex
		{"gpt-4", "re:gpt-4", true},
		{"gpt-3.5", "re:gpt-4", false},
		// Regex with alternation
		{"gpt-4", "re:gpt-(4|4o)", true},
		{"gpt-4o", "re:gpt-(4|4o)", true},
		{"gpt-3.5", "re:gpt-(4|4o)", false},
		// Regex with character class
		{"gpt-4", "re:^gpt-[0-9]", true},
		{"gpt-40", "re:^gpt-[0-9]", true},
		{"claude-3", "re:^gpt-[0-9]", false},
		// Case-insensitive (via regex flags in pattern)
		{"GPT-4", "re:(?i)gpt-4", true},
		{"GPT-4", "re:gpt-4", false}, // Without (?i)
		// Regex with dot
		{"gpt-3.5-turbo", "re:^gpt-3\\.5", true},
		// Empty re: pattern
		{"gpt-4", "re:", false},
		// Bogus regex
		{"gpt-4", "re:[invalid", false},
	}

	for _, tt := range tests {
		got := MatchesModelPattern(tt.model, tt.pattern)
		if got != tt.expect {
			t.Errorf("MatchesModelPattern(%q, %q) = %v, expected %v (regex)",
				tt.model, tt.pattern, got, tt.expect)
		}
	}
}

func TestMatchesModelPattern_EdgeCases(t *testing.T) {
	tests := []struct {
		model   string
		pattern string
		expect  bool
	}{
		{"", "", false},
		{"gpt-4", "", false},
		{"", "gpt-4", false},
		{"gpt-4", "  gpt-4  ", true}, // trimmed
		{"  gpt-4  ", "gpt-4", true},
		{"gpt-4", "*", true},
		{"", "*", false},   // empty model never matches
		{"gpt-4", "?", false}, // single-char pattern, multi-char model
		{"a", "?", true},
	}

	for _, tt := range tests {
		got := MatchesModelPattern(tt.model, tt.pattern)
		if got != tt.expect {
			t.Errorf("MatchesModelPattern(%q, %q) = %v, expected %v",
				tt.model, tt.pattern, got, tt.expect)
		}
	}
}

// =============================================================================
// Regex/Glob helpers
// =============================================================================

func TestIsRegexModelPattern(t *testing.T) {
	tests := []struct {
		pattern string
		expect  bool
	}{
		{"re:.*", true},
		{"RE:.*", true},
		{" Re:.* ", true},
		{"gpt-*", false},
		{"", false},
		{"regex:.*", false}, // must be re: prefix
	}

	for _, tt := range tests {
		got := IsRegexModelPattern(tt.pattern)
		if got != tt.expect {
			t.Errorf("IsRegexModelPattern(%q) = %v, expected %v",
				tt.pattern, got, tt.expect)
		}
	}
}

func TestParseRegexModelPattern(t *testing.T) {
	if re := ParseRegexModelPattern("re:^gpt-"); re == nil {
		t.Error("expected non-nil regex for valid pattern")
	}
	if re := ParseRegexModelPattern("gpt-*"); re != nil {
		t.Error("expected nil regex for glob pattern")
	}
	if re := ParseRegexModelPattern("re:"); re != nil {
		t.Error("expected nil regex for empty re body")
	}
	if re := ParseRegexModelPattern(""); re != nil {
		t.Error("expected nil regex for empty pattern")
	}
}

func TestIsExactRouteModelPattern(t *testing.T) {
	tests := []struct {
		pattern string
		expect  bool
	}{
		{"gpt-4", true},
		{"claude-3.5-sonnet", true},
		{"re:gpt-4", false},
		{"gpt-*", false},
		{"gpt-?", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsExactRouteModelPattern(tt.pattern)
		if got != tt.expect {
			t.Errorf("IsExactRouteModelPattern(%q) = %v, expected %v",
				tt.pattern, got, tt.expect)
		}
	}
}

// =============================================================================
// Model alias normalization
// =============================================================================

func TestNormalizeModelAlias(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"anthropic/claude-sonnet", "claude-sonnet"},
		{"openai/gpt-4", "gpt-4"},
		{"gpt-4", "gpt-4"},
		{"GPT-4", "gpt-4"},
		{" Anthropic/Claude-Opus ", "claude-opus"},
		{"no/slash/here", "here"},
		{"", ""},
		{"trailing/slash/", "trailing/slash/"}, // last / at end → slashIndex=14, len=15, 14<14 is false → keep whole string
	}

	for _, tt := range tests {
		got := NormalizeModelAlias(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeModelAlias(%q) = %q, expected %q",
				tt.input, got, tt.expected)
		}
	}
}

func TestIsModelAliasEquivalent(t *testing.T) {
	tests := []struct {
		left, right string
		expect      bool
	}{
		{"anthropic/claude-sonnet", "openai/claude-sonnet", true},
		{"gpt-4", "openai/gpt-4", true},
		{"GPT-4", "gpt-4", true},
		{"gpt-4", "gpt-3.5", false},
		{"", "gpt-4", false},
		{"gpt-4", "", false},
	}

	for _, tt := range tests {
		got := IsModelAliasEquivalent(tt.left, tt.right)
		if got != tt.expect {
			t.Errorf("IsModelAliasEquivalent(%q, %q) = %v, expected %v",
				tt.left, tt.right, got, tt.expect)
		}
	}
}

// =============================================================================
// ChannelSupportsRequestedModel
// =============================================================================

func TestChannelSupportsRequestedModel(t *testing.T) {
	// nil source model = supports everything
	if !ChannelSupportsRequestedModel(nil, "gpt-4") {
		t.Error("expected nil source model to support any model")
	}

	emptyModel := ""
	if !ChannelSupportsRequestedModel(&emptyModel, "gpt-4") {
		t.Error("expected empty source model to support any model")
	}

	// Exact match
	srcModel := "gpt-4"
	if !ChannelSupportsRequestedModel(&srcModel, "gpt-4") {
		t.Error("expected exact match to support")
	}

	// Alias match
	srcModel = "openai/gpt-4"
	if !ChannelSupportsRequestedModel(&srcModel, "gpt-4") {
		t.Error("expected alias match to support")
	}

	// Glob match
	srcModel = "gpt-*"
	if !ChannelSupportsRequestedModel(&srcModel, "gpt-4") {
		t.Error("expected glob match to support")
	}

	// No match
	srcModel = "claude-*"
	if ChannelSupportsRequestedModel(&srcModel, "gpt-4") {
		t.Error("expected non-matching pattern to not support")
	}
}

// =============================================================================
// Display name matching
// =============================================================================

func TestIsRouteDisplayNameMatch(t *testing.T) {
	dn := "My GPT-4 Route"
	if !IsRouteDisplayNameMatch("My GPT-4 Route", &dn) {
		t.Error("expected display name match")
	}
	if IsRouteDisplayNameMatch("gpt-4", &dn) {
		t.Error("expected no match for different display name")
	}
	if IsRouteDisplayNameMatch("gpt-4", nil) {
		t.Error("expected no match for nil display name")
	}

	emptyDN := ""
	if IsRouteDisplayNameMatch("gpt-4", &emptyDN) {
		t.Error("expected no match for empty display name")
	}
}

func TestNormalizeRouteDisplayName(t *testing.T) {
	if got := NormalizeRouteDisplayName(nil); got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
	val := " Hello "
	if got := NormalizeRouteDisplayName(&val); got != "Hello" {
		t.Errorf("expected 'Hello', got %q", got)
	}
}

// =============================================================================
// Route mode detection
// =============================================================================

func TestIsExplicitGroupRoute(t *testing.T) {
	if !IsExplicitGroupRoute("explicit_group") {
		t.Error("expected true for 'explicit_group'")
	}
	if IsExplicitGroupRoute("weighted") {
		t.Error("expected false for 'weighted'")
	}
}

func TestHasCustomDisplayName(t *testing.T) {
	dn := "Custom Name"
	pattern := "gpt-4"
	if !HasCustomDisplayName(pattern, &dn) {
		t.Error("expected custom display name")
	}
	if HasCustomDisplayName(pattern, nil) {
		t.Error("expected no custom display name for nil")
	}
	sameDN := "gpt-4"
	if HasCustomDisplayName(pattern, &sameDN) {
		t.Error("expected no custom display name when same as pattern")
	}
}

func TestGetExposedModelNameForRoute(t *testing.T) {
	dn := "My Model"
	if got := GetExposedModelNameForRoute(&dn, "gpt-4"); got != "My Model" {
		t.Errorf("expected 'My Model', got %q", got)
	}
	if got := GetExposedModelNameForRoute(nil, "gpt-4"); got != "gpt-4" {
		t.Errorf("expected 'gpt-4', got %q", got)
	}
}

// =============================================================================
// Downstream policy model check
// =============================================================================

func TestIsModelAllowedByDownstreamPolicy(t *testing.T) {
	// Empty policy (no restrictions) with denyAllWhenEmpty=false
	policy := DownstreamRoutingPolicy{DenyAllWhenEmpty: false}
	if !IsModelAllowedByDownstreamPolicy("gpt-4", policy) {
		t.Error("expected allowed for empty permissive policy")
	}

	// Empty policy with denyAllWhenEmpty=true
	policy = DownstreamRoutingPolicy{DenyAllWhenEmpty: true}
	if IsModelAllowedByDownstreamPolicy("gpt-4", policy) {
		t.Error("expected denied for empty restrictive policy")
	}

	// With supported models
	policy = DownstreamRoutingPolicy{
		SupportedModels: []string{"gpt-*", "claude-*"},
	}
	if !IsModelAllowedByDownstreamPolicy("gpt-4", policy) {
		t.Error("expected allowed for matching supported model")
	}
	if IsModelAllowedByDownstreamPolicy("gemini-pro", policy) {
		t.Error("expected denied for non-matching model")
	}

	// With allowed routes (no supported models matched, but has allowed routes)
	policy = DownstreamRoutingPolicy{
		SupportedModels: []string{"claude-*"},
		AllowedRouteIDs: []int64{1, 2, 3},
	}
	if !IsModelAllowedByDownstreamPolicy("gpt-4", policy) {
		t.Error("expected allowed: no supported pattern matched but allowedRouteIds non-empty")
	}
}

// =============================================================================
// Model mapping
// =============================================================================

func TestParseModelMappingRecord(t *testing.T) {
	// Valid mapping
	raw := `{"gpt-4":"gpt-4-0613","gpt-3.5-turbo":"gpt-3.5-turbo-0125"}`
	mapping := ParseModelMappingRecord(&raw)
	if mapping == nil {
		t.Fatal("expected non-nil mapping")
	}
	if mapping["gpt-4"] != "gpt-4-0613" {
		t.Errorf("expected 'gpt-4-0613', got %q", mapping["gpt-4"])
	}
	if mapping["gpt-3.5-turbo"] != "gpt-3.5-turbo-0125" {
		t.Errorf("expected 'gpt-3.5-turbo-0125', got %q", mapping["gpt-3.5-turbo"])
	}

	// Nil
	if ParseModelMappingRecord(nil) != nil {
		t.Error("expected nil for nil input")
	}

	// Empty
	empty := ""
	if ParseModelMappingRecord(&empty) != nil {
		t.Error("expected nil for empty string")
	}
}

func TestResolveMappedModel(t *testing.T) {
	// No mapping
	result := ResolveMappedModel("gpt-4", nil)
	if result != "gpt-4" {
		t.Errorf("expected 'gpt-4', got %q", result)
	}

	// Exact key match
	mapping := `{"gpt-4":"gpt-4-0613"}`
	result = ResolveMappedModel("gpt-4", &mapping)
	if result != "gpt-4-0613" {
		t.Errorf("expected 'gpt-4-0613', got %q", result)
	}

	// Pattern match
	mapping = `{"gpt-*":"gpt-4o"}`
	result = ResolveMappedModel("gpt-4", &mapping)
	if result != "gpt-4o" {
		t.Errorf("expected 'gpt-4o', got %q", result)
	}

	// Exact takes priority over pattern
	mapping = `{"gpt-4":"gpt-4-specific","gpt-*":"gpt-4o"}`
	result = ResolveMappedModel("gpt-4", &mapping)
	if result != "gpt-4-specific" {
		t.Errorf("expected 'gpt-4-specific', got %q", result)
	}
}

func TestResolveActualModelForSelectedChannel(t *testing.T) {
	dn := "My Display Name"
	srcModel := "claude-3-opus"

	// Display name match → use source model
	result := ResolveActualModelForSelectedChannel("My Display Name", &dn, "claude-sonnet", &srcModel)
	if result != "claude-3-opus" {
		t.Errorf("expected source model 'claude-3-opus', got %q", result)
	}

	// Non-display-name match → use mapped model
	result = ResolveActualModelForSelectedChannel("gpt-4", &dn, "gpt-4-0613", &srcModel)
	if result != "gpt-4-0613" {
		t.Errorf("expected mapped model 'gpt-4-0613', got %q", result)
	}
}

// =============================================================================
// Route visibility
// =============================================================================

func TestNormalizeRouteRoutingStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected RouteRoutingStrategy
	}{
		{"weighted", StrategyWeighted},
		{"round_robin", StrategyRoundRobin},
		{"stable_first", StrategyStableFirst},
		{"", StrategyWeighted},           // unknown → weighted
		{"unknown", StrategyWeighted},    // unknown → weighted
		{"WEIGHTED", StrategyWeighted},   // case sensitive? Let's check
	}

	for _, tt := range tests {
		got := NormalizeRouteRoutingStrategy(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeRouteRoutingStrategy(%q) = %q, expected %q",
				tt.input, got, tt.expected)
		}
	}
}

func TestIsRoundRobinRouteRoutingStrategy(t *testing.T) {
	if !IsRoundRobinRouteRoutingStrategy("round_robin") {
		t.Error("expected true for round_robin")
	}
	if IsRoundRobinRouteRoutingStrategy("weighted") {
		t.Error("expected false for weighted")
	}
}

// =============================================================================
// Glob matching edge cases (additional)
// =============================================================================

func TestGlobMatch_EdgeCases(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		expect  bool
	}{
		{"*a*b*", "abc", true},
		{"*a*b*", "acb", true},
		{"*a*b*", "xxxayyybzzz", true},
		{"a*b", "ab", true},     // * can match empty
		{"a*b", "aXXb", true},
		{"a*b", "ac", false},
		{"a?b", "aXb", true},
		{"a?b", "ab", false},    // ? must match exactly one char
		{"a?b", "aXXb", false},
		{"?", "x", true},
		{"?", "", false},
		{"*", "", true},
		{"****", "anything", true}, // multiple consecutive stars
		{"a\\*b", "a*b", false},    // backslash not special in our glob
	}

	for _, tt := range tests {
		got := globMatch(tt.pattern, tt.value)
		if got != tt.expect {
			t.Errorf("globMatch(%q, %q) = %v, expected %v",
				tt.pattern, tt.value, got, tt.expect)
		}
	}
}
