package proxy

import "testing"

func TestResolveEndpointCandidates_ResponsesOnly(t *testing.T) {
	cands := ResolveEndpointCandidatesWithOptions("/v1/chat/completions", EndpointCandidateOptions{
		Preference: SiteProtocolPreference{ResponsesOnly: true, PreferResponses: true, PreferStream: true},
	})
	if len(cands) != 1 || cands[0] != EndpointResponses {
		t.Fatalf("cands=%v", cands)
	}
}

func TestResolveEndpointCandidates_PreferResponsesOrder(t *testing.T) {
	cands := ResolveEndpointCandidatesWithOptions("/v1/chat/completions", EndpointCandidateOptions{
		Preference: SiteProtocolPreference{PreferResponses: true},
	})
	if len(cands) < 2 || cands[0] != EndpointResponses {
		t.Fatalf("cands=%v", cands)
	}
}

func TestResolveEndpointCandidates_DefaultChatPrimary(t *testing.T) {
	cands := ResolveEndpointCandidates("/v1/chat/completions", false)
	if len(cands) == 0 || cands[0] != EndpointChat {
		t.Fatalf("cands=%v", cands)
	}
}

func TestResponsesOnlyChatUnsupportedMessage(t *testing.T) {
	msg := ResponsesOnlyChatUnsupportedMessage("/v1/chat/completions")
	if msg == "" {
		t.Fatal("empty message")
	}
}
