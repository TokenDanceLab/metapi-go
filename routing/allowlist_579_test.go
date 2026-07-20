package routing

import (
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

func TestResolveDownstreamExclusionReason_AllowedSiteList(t *testing.T) {
	t.Parallel()
	s := &ChannelSelector{}
	cand := RouteChannelCandidate{
		Site:    store.Site{ID: 10},
		Account: store.Account{ID: 1},
		Channel: store.RouteChannel{},
	}
	// allow-list present and site not listed
	reason := s.resolveDownstreamExclusionReason(cand, DownstreamRoutingPolicy{
		AllowedSiteIDs: []int64{99},
	})
	if reason == "" {
		t.Fatal("expected site not in allow-list")
	}
	// listed site passes allow gate (no excludes)
	reason = s.resolveDownstreamExclusionReason(cand, DownstreamRoutingPolicy{
		AllowedSiteIDs: []int64{10},
	})
	if reason != "" {
		t.Fatalf("listed site should be eligible, got %q", reason)
	}
	// empty allow-list unrestricted
	reason = s.resolveDownstreamExclusionReason(cand, DownstreamRoutingPolicy{})
	if reason != "" {
		t.Fatalf("empty allow-list should be unrestricted, got %q", reason)
	}
	// exclusion still wins after allow
	reason = s.resolveDownstreamExclusionReason(cand, DownstreamRoutingPolicy{
		AllowedSiteIDs:  []int64{10},
		ExcludedSiteIDs: []int64{10},
	})
	if reason == "" {
		t.Fatal("exclude should still reject allowed site")
	}
}

func TestResolveDownstreamExclusionReason_AllowedCredentialList(t *testing.T) {
	t.Parallel()
	s := &ChannelSelector{}
	tokenID := int64(7)
	cand := RouteChannelCandidate{
		Site:    store.Site{ID: 2},
		Account: store.Account{ID: 3},
		Channel: store.RouteChannel{TokenID: &tokenID},
		Token:   &store.AccountToken{ID: 7, Token: "tok", Enabled: true},
	}
	// allow-list of different token rejects
	reason := s.resolveDownstreamExclusionReason(cand, DownstreamRoutingPolicy{
		AllowedCredentialRefs: []CredentialRef{
			{Kind: "account_token", SiteID: 2, AccountID: 3, TokenID: 99},
		},
	})
	if reason == "" {
		t.Fatal("expected credential not in allow-list")
	}
	// matching allow accepts
	reason = s.resolveDownstreamExclusionReason(cand, DownstreamRoutingPolicy{
		AllowedCredentialRefs: []CredentialRef{
			{Kind: "account_token", SiteID: 2, AccountID: 3, TokenID: 7},
		},
	})
	if reason != "" {
		t.Fatalf("matching allow should pass, got %q", reason)
	}
}
