package routing

import (
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

func TestResolveChannelTokenValue_RequiresAPITokenForNonOAuthAccounts(t *testing.T) {
	selector := &ChannelSelector{}

	candidate := RouteChannelCandidate{
		Account: store.Account{
			AccessToken: "session-or-raw-access-token",
			APIToken:    nil,
		},
	}
	if got := selector.resolveChannelTokenValue(candidate); got != "" {
		t.Fatalf("non-OAuth account with only access_token resolved %q, want empty", got)
	}

	candidate.Account.APIToken = ptrStr("api-token-for-routing")
	if got := selector.resolveChannelTokenValue(candidate); got != "api-token-for-routing" {
		t.Fatalf("non-OAuth account token = %q, want api-token-for-routing", got)
	}
}
