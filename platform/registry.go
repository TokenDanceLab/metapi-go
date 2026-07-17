package platform

import (
	"context"
	"strings"
)

// PLATFORM_ALIASES maps user-facing platform names to canonical identifiers.
var PlatformAliases = map[string]string{
	"anyrouter":     "anyrouter",
	"wong-gongyi":   "new-api",
	"vo-api":        "new-api",
	"super-api":     "new-api",
	"rix-api":       "new-api",
	"neo-api":       "new-api",
	"newapi":        "new-api",
	"new api":       "new-api",
	"new-api":       "new-api",
	"oneapi":        "one-api",
	"one api":       "one-api",
	"one-api":       "one-api",
	"onehub":        "one-hub",
	"one-hub":       "one-hub",
	"donehub":       "done-hub",
	"done-hub":      "done-hub",
	"veloera":       "veloera",
	"sub2api":       "sub2api",
	"openai":        "openai",
	"codex":         "codex",
	"chatgpt-codex": "codex",
	"chatgpt codex": "codex",
	"anthropic":     "claude",
	"claude":        "claude",
	"gemini":        "gemini",
	"gemini-cli":    "gemini-cli",
	"antigravity":   "antigravity",
	"anti-gravity":  "antigravity",
	"google":        "gemini",
	"cliproxyapi":   "cliproxyapi",
	"cpa":           "cliproxyapi",
	"cli-proxy-api": "cliproxyapi",
}

// registry holds all platform adapters in detection order.
// Populated via Register() calls from each adapter's init().
var registry []PlatformAdapter

// orderedPlatformNames defines the spec-required adapter registration order.
// "Specific forks before generic adapters for better auto-detection."
// OneApi last (its HTTP probe is the broadest condition, serving as catch-all).
var orderedPlatformNames = []string{
	"openai",
	"codex",
	"claude",
	"gemini",
	"gemini-cli",
	"antigravity",
	"cliproxyapi",
	"anyrouter",
	"done-hub",
	"one-hub",
	"veloera",
	"new-api",
	"sub2api",
	"one-api",
}

// Register adds an adapter to the global registry.
// Called from adapter init() functions.
func Register(a PlatformAdapter) {
	registry = append(registry, a)
}

// InitRegistry reorders the adapter registry to match the spec-required detection sequence.
// Call this once at startup after all packages have been imported.
// The order ensures: specific forks before generic adapters, OneApi last as catch-all.
func InitRegistry() {
	// Collect adapters registered by init() functions
	byName := make(map[string]PlatformAdapter, len(registry))
	for _, a := range registry {
		byName[a.PlatformName()] = a
	}

	// Re-register in spec order
	registry = nil
	for _, name := range orderedPlatformNames {
		if a, ok := byName[name]; ok {
			Register(a)
		}
	}
}

// NormalizePlatformAlias maps a raw platform string to its canonical form.
func NormalizePlatformAlias(platform string) string {
	raw := strings.ToLower(strings.TrimSpace(platform))
	if raw == "" {
		return ""
	}
	if canonical, ok := PlatformAliases[raw]; ok {
		return canonical
	}
	return raw
}

// GetAdapter returns the registered adapter for a given canonical platform name.
func GetAdapter(platform string) PlatformAdapter {
	normalized := NormalizePlatformAlias(platform)
	for _, a := range registry {
		if a.PlatformName() == normalized {
			return a
		}
	}
	return nil
}

// DetectPlatform runs the 4-step detection pipeline to identify a platform from a URL.
func DetectPlatform(rawURL string) PlatformAdapter {
	// Step 1: URL Hint (direct match)
	if hint := DetectPlatformByURLHint(rawURL); hint != "" {
		if a := GetAdapter(hint); a != nil {
			return a
		}
	}

	// Step 2: Title Hint with titleFirstPlatforms short-circuit
	if hint := DetectPlatformByTitle(rawURL); hint != "" {
		if titleFirstPlatforms[TitleHintPlatform(hint)] {
			if a := GetAdapter(hint); a != nil {
				return a
			}
		}
		// new-api and one-api are NOT in titleFirstPlatforms — they need step 3 probe
	}

	// Step 3: Sequential probe (iterate adapters in registration order)
	ctx := context.Background()
	for _, a := range registry {
		ok, _ := a.Detect(ctx, rawURL)
		if ok {
			return a
		}
	}

	// Step 4: Title Hint fallback (for new-api/one-api that didn't match probe)
	if hint := DetectPlatformByTitle(rawURL); hint != "" {
		if a := GetAdapter(hint); a != nil {
			return a
		}
	}

	return nil
}

// ListAdapters returns all registered adapters (for diagnostics).
func ListAdapters() []PlatformAdapter {
	return append([]PlatformAdapter{}, registry...)
}

// ListRegisteredPlatformNames returns the canonical platform names currently
// present in the adapter registry. Names follow orderedPlatformNames when
// possible so callers (e.g. settings brand-list) get a stable order.
func ListRegisteredPlatformNames() []string {
	byName := make(map[string]struct{}, len(registry))
	for _, a := range registry {
		if a == nil {
			continue
		}
		name := strings.TrimSpace(a.PlatformName())
		if name == "" {
			continue
		}
		byName[name] = struct{}{}
	}

	names := make([]string, 0, len(byName))
	seen := make(map[string]struct{}, len(byName))
	for _, name := range orderedPlatformNames {
		if _, ok := byName[name]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	// Include any adapters not listed in orderedPlatformNames (future-proof).
	for name := range byName {
		if _, ok := seen[name]; ok {
			continue
		}
		names = append(names, name)
	}
	return names
}
