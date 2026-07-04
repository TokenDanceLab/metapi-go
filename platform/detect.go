package platform

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// TitleHintPlatform is a platform identifier from page title detection.
type TitleHintPlatform string

const (
	TitleAnyRouter TitleHintPlatform = "anyrouter"
	TitleDoneHub   TitleHintPlatform = "done-hub"
	TitleOneHub    TitleHintPlatform = "one-hub"
	TitleVeloera   TitleHintPlatform = "veloera"
	TitleSub2Api   TitleHintPlatform = "sub2api"
	TitleNewApi    TitleHintPlatform = "new-api"
	TitleOneApi    TitleHintPlatform = "one-api"
)

type titleRule struct {
	platform TitleHintPlatform
	regex    *regexp.Regexp
}

var titleRules = []titleRule{
	{TitleAnyRouter, regexp.MustCompile(`\bany\s*router\b`)},
	{TitleDoneHub, regexp.MustCompile(`\bdone[-_ ]?hub\b`)},
	{TitleOneHub, regexp.MustCompile(`\bone[-_ ]?hub\b`)},
	{TitleVeloera, regexp.MustCompile(`\bveloera\b`)},
	{TitleSub2Api, regexp.MustCompile(`\bsub2api\b`)},
	{TitleNewApi, regexp.MustCompile(`\bnew[-_ ]?api\b`)},
	{TitleNewApi, regexp.MustCompile(`\bvo[-_ ]?api\b`)},
	{TitleNewApi, regexp.MustCompile(`\bsuper[-_ ]?api\b`)},
	{TitleNewApi, regexp.MustCompile(`\brix[-_ ]?api\b`)},
	{TitleNewApi, regexp.MustCompile(`\bneo[-_ ]?api\b`)},
	{TitleNewApi, regexp.MustCompile(`wong\s*公益站`)},
	{TitleOneApi, regexp.MustCompile(`\bone[-_ ]?api\b`)},
}

// titleFirstPlatforms are platforms that short-circuit during step 2 of detection.
var titleFirstPlatforms = map[TitleHintPlatform]bool{
	TitleAnyRouter: true,
	TitleDoneHub:   true,
	TitleOneHub:    true,
	TitleVeloera:   true,
	TitleSub2Api:   true,
}

// normalizeURLToOrigin extracts protocol://host from a URL.
func normalizeURLToOrigin(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		s := strings.TrimSuffix(strings.TrimSuffix(raw, "/"), "\\")
		return s
	}
	return parsed.Scheme + "://" + parsed.Host
}

// DetectPlatformByURLHint detects platform from known URL patterns.
func DetectPlatformByURLHint(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return ""
	}
	normalized := strings.ToLower(u)
	if !strings.Contains(normalized, "://") {
		normalized = "https://" + normalized
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	port := parsed.Port()
	path := strings.ToLower(strings.TrimSpace(parsed.Path))

	if host == "api.openai.com" {
		return "openai"
	}
	if host == "chatgpt.com" && strings.HasPrefix(path, "/backend-api/codex") {
		return "codex"
	}
	if host == "api.anthropic.com" || (host == "anthropic.com" && strings.HasPrefix(path, "/v1")) {
		return "claude"
	}
	if host == "generativelanguage.googleapis.com" || host == "gemini.google.com" ||
		((host == "googleapis.com" || strings.HasSuffix(host, ".googleapis.com")) && strings.HasPrefix(path, "/v1beta/openai")) {
		return "gemini"
	}
	if host == "cloudcode-pa.googleapis.com" {
		return "gemini-cli"
	}
	if (host == "127.0.0.1" || host == "localhost") && port == "8317" {
		return "cliproxyapi"
	}
	if strings.Contains(host, "anyrouter") {
		return "anyrouter"
	}
	if strings.Contains(host, "donehub") || strings.Contains(host, "done-hub") {
		return "done-hub"
	}
	if strings.Contains(host, "onehub") || strings.Contains(host, "one-hub") {
		return "one-hub"
	}
	if strings.Contains(host, "veloera") {
		return "veloera"
	}
	if strings.Contains(host, "sub2api") {
		return "sub2api"
	}

	return ""
}

// DetectPlatformByTitle fetches the page title and matches it against known patterns.
// Returns the canonical platform name if a match is found.
func DetectPlatformByTitle(rawURL string) string {
	base := normalizeURLToOrigin(rawURL)
	if base == "" {
		return ""
	}

	result := detectPlatformByTitleOnce(base)
	if result != "" {
		return result
	}

	// Retry once after 50ms delay (handle race conditions with ephemeral servers)
	time.Sleep(50 * time.Millisecond)
	return detectPlatformByTitleOnce(base)
}

func detectPlatformByTitleOnce(base string) string {
	text, contentType, err := fetchTextWithTimeout(base + "/")
	if err != nil {
		return ""
	}

	ct := strings.ToLower(contentType)
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml+xml") {
		return ""
	}

	title := extractHTMLTitle(text)
	if title == "" {
		return ""
	}

	titleLower := strings.ToLower(title)
	for _, rule := range titleRules {
		if rule.regex.MatchString(titleLower) {
			return string(rule.platform)
		}
	}

	return ""
}

func fetchTextWithTimeout(urlStr string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return fetchText(ctx, urlStr, nil)
}

var htmlTitleRE = regexp.MustCompile(`(?is)<title[^>]*>\s*(.+?)\s*</title>`)

func extractHTMLTitle(html string) string {
	match := htmlTitleRE.FindStringSubmatch(html)
	if len(match) < 2 {
		return ""
	}
	// Collapse whitespace
	return strings.Join(strings.Fields(match[1]), " ")
}
