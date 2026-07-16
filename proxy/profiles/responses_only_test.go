package profiles

import "testing"

func TestDetectSiteProtocolPreference_HeaderResponsesOnly(t *testing.T) {
	pref := DetectSiteProtocolPreference("openai", "https://example.com", map[string]string{
		"x-metapi-responses-only": "true",
	})
	if !pref.ResponsesOnly || !pref.PreferResponses || !pref.PreferStream {
		t.Fatalf("pref=%+v", pref)
	}
	if pref.Source != "header" {
		t.Fatalf("source=%q", pref.Source)
	}
}

func TestDetectSiteProtocolPreference_PlatformCodex(t *testing.T) {
	pref := DetectSiteProtocolPreference("codex", "https://api.openai.com", nil)
	if !pref.PreferResponses && !pref.ResponsesOnly {
		// codex platform should at least prefer responses
		t.Fatalf("expected prefer-responses for codex platform, got %+v", pref)
	}
}

func TestIsMetapiControlHeader(t *testing.T) {
	if !IsMetapiControlHeader("X-Metapi-Responses-Only") {
		t.Fatal("expected control header")
	}
	if IsMetapiControlHeader("Authorization") {
		t.Fatal("Authorization is not control")
	}
}
