package routing

import (
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// =============================================================================
// OAuth route unit candidate detection
// =============================================================================

func TestIsOAuthRouteUnitCandidate(t *testing.T) {
	// Has route unit set
	c := RouteChannelCandidate{
		RouteUnit: &OAuthRouteUnitSummary{ID: 1},
	}
	if !IsOAuthRouteUnitCandidate(c) {
		t.Error("expected true when RouteUnit is set")
	}

	// Has OAuthRouteUnitID on channel
	unitID := int64(5)
	c = RouteChannelCandidate{
		Channel: store.RouteChannel{OAuthRouteUnitID: &unitID},
	}
	if !IsOAuthRouteUnitCandidate(c) {
		t.Error("expected true when Channel.OAuthRouteUnitID is set")
	}

	// Neither
	c = RouteChannelCandidate{}
	if IsOAuthRouteUnitCandidate(c) {
		t.Error("expected false when neither is set")
	}

	// OAuthRouteUnitID = 0
	zeroID := int64(0)
	c = RouteChannelCandidate{
		Channel: store.RouteChannel{OAuthRouteUnitID: &zeroID},
	}
	if IsOAuthRouteUnitCandidate(c) {
		t.Error("expected false when OAuthRouteUnitID is 0")
	}
}

// =============================================================================
// Member eligibility
// =============================================================================

func makeMember(accountID int64, status string, siteStatus string, accessToken string, apiToken string) OAuthRouteUnitMemberCandidate {
	return OAuthRouteUnitMemberCandidate{
		Member: store.OAuthRouteUnitMember{
			ID:        accountID,
			AccountID: accountID,
		},
		Account: store.Account{
			ID:          accountID,
			Status:      status,
			AccessToken: accessToken,
			APIToken:    &apiToken,
		},
		Site: store.Site{
			ID:     accountID * 10,
			Status: siteStatus,
		},
	}
}

func TestGetEligibleRouteUnitMembers(t *testing.T) {
	nowISO := time.Now().UTC().Format(time.RFC3339)

	outerCandidate := RouteChannelCandidate{
		Channel: store.RouteChannel{OAuthRouteUnitID: ptrInt(1)},
		RouteUnit: &OAuthRouteUnitSummary{
			ID:       1,
			Strategy: "round_robin",
		},
		RouteUnitMembers: []OAuthRouteUnitMemberCandidate{
			makeMember(100, "active", "active", "token1", ""),         // eligible
			makeMember(101, "disabled", "active", "token2", ""),       // disabled account
			makeMember(102, "active", "disabled", "token3", ""),       // disabled site
			makeMember(103, "active", "active", "", ""),               // no token
			makeMember(104, "active", "active", "", "api-token"),      // eligible (apiToken)
		},
	}

	eligible := getEligibleRouteUnitMembers(outerCandidate, "gpt-4", nowISO)
	if len(eligible) != 2 {
		t.Errorf("expected 2 eligible members (100+104), got %d", len(eligible))
		for _, m := range eligible {
			t.Logf("  eligible: account=%d", m.Account.ID)
		}
	}

	// Non-OAuth candidate returns nil
	nonOAuth := RouteChannelCandidate{}
	if got := getEligibleRouteUnitMembers(nonOAuth, "gpt-4", nowISO); got != nil {
		t.Error("expected nil for non-OAuth candidate")
	}
}

func TestGetEligibleRouteUnitMembers_Cooldown(t *testing.T) {
	nowISO := time.Now().UTC().Format(time.RFC3339)
	futureISO := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)

	outerCandidate := RouteChannelCandidate{
		Channel: store.RouteChannel{OAuthRouteUnitID: ptrInt(1)},
		RouteUnit: &OAuthRouteUnitSummary{ID: 1, Strategy: "round_robin"},
		RouteUnitMembers: []OAuthRouteUnitMemberCandidate{
			{
				Member: store.OAuthRouteUnitMember{
					ID:            100,
					AccountID:     100,
					CooldownUntil: &futureISO,
				},
				Account: store.Account{ID: 100, Status: "active", AccessToken: "t1"},
				Site:    store.Site{ID: 1000, Status: "active"},
			},
			{
				Member: store.OAuthRouteUnitMember{
					ID:        101,
					AccountID: 101,
				},
				Account: store.Account{ID: 101, Status: "active", AccessToken: "t2"},
				Site:    store.Site{ID: 1010, Status: "active"},
			},
		},
	}

	eligible := getEligibleRouteUnitMembers(outerCandidate, "gpt-4", nowISO)
	if len(eligible) != 1 {
		t.Errorf("expected 1 eligible member (not cooling down), got %d", len(eligible))
	}
	if len(eligible) > 0 && eligible[0].Account.ID != 101 {
		t.Errorf("expected account 101 (not cooling), got %d", eligible[0].Account.ID)
	}
}

// =============================================================================
// Route unit member selection strategies
// =============================================================================

func TestSelectRouteUnitMember_RoundRobin(t *testing.T) {
	nowISO := time.Now().UTC().Format(time.RFC3339)
	nowMs := time.Now().UnixMilli()

	earlier := "2024-01-01T00:00:00Z"
	later := "2024-06-01T00:00:00Z"

	outerCandidate := RouteChannelCandidate{
		Channel: store.RouteChannel{OAuthRouteUnitID: ptrInt(1)},
		RouteUnit: &OAuthRouteUnitSummary{
			ID:       1,
			Strategy: "round_robin",
			Enabled:  true,
		},
		RouteUnitMembers: []OAuthRouteUnitMemberCandidate{
			{
				Member:  store.OAuthRouteUnitMember{ID: 1, AccountID: 100, LastSelectedAt: &later, LastUsedAt: &later},
				Account: store.Account{ID: 100, Status: "active", AccessToken: "t1"},
				Site:    store.Site{ID: 1000, Status: "active"},
			},
			{
				Member:  store.OAuthRouteUnitMember{ID: 2, AccountID: 101, LastSelectedAt: &earlier, LastUsedAt: &earlier},
				Account: store.Account{ID: 101, Status: "active", AccessToken: "t2"},
				Site:    store.Site{ID: 1010, Status: "active"},
			},
		},
	}

	member := SelectRouteUnitMember(outerCandidate, "gpt-4", nowISO, nowMs, 0, nil)
	if member == nil {
		t.Fatal("expected a selected member")
	}
	// Round-robin: earliest lastSelectedAt first → account 101 (earlier)
	if member.Account.ID != 101 {
		t.Errorf("expected account 101 (earliest selected), got %d", member.Account.ID)
	}
}

func TestSelectRouteUnitMember_StickUntilUnavailable(t *testing.T) {
	nowISO := time.Now().UTC().Format(time.RFC3339)
	nowMs := time.Now().UnixMilli()

	earlier := "2024-01-01T00:00:00Z"
	later := "2024-06-01T00:00:00Z"

	outerCandidate := RouteChannelCandidate{
		Channel: store.RouteChannel{OAuthRouteUnitID: ptrInt(1)},
		RouteUnit: &OAuthRouteUnitSummary{
			ID:       1,
			Strategy: "stick_until_unavailable",
			Enabled:  true,
		},
		RouteUnitMembers: []OAuthRouteUnitMemberCandidate{
			{
				Member:  store.OAuthRouteUnitMember{ID: 1, AccountID: 100, LastSelectedAt: &later, LastUsedAt: &later},
				Account: store.Account{ID: 100, Status: "active", AccessToken: "t1"},
				Site:    store.Site{ID: 1000, Status: "active"},
			},
			{
				Member:  store.OAuthRouteUnitMember{ID: 2, AccountID: 101, LastSelectedAt: &earlier, LastUsedAt: &earlier},
				Account: store.Account{ID: 101, Status: "active", AccessToken: "t2"},
				Site:    store.Site{ID: 1010, Status: "active"},
			},
		},
	}

	member := SelectRouteUnitMember(outerCandidate, "gpt-4", nowISO, nowMs, 0, nil)
	if member == nil {
		t.Fatal("expected a selected member")
	}
	// Stick until unavailable: most recently used first → account 100 (later)
	if member.Account.ID != 100 {
		t.Errorf("expected account 100 (most recent), got %d", member.Account.ID)
	}
}

func TestSelectRouteUnitMember_NoEligible(t *testing.T) {
	outerCandidate := RouteChannelCandidate{
		Channel:   store.RouteChannel{},
		RouteUnit: nil,
	}
	member := SelectRouteUnitMember(outerCandidate, "gpt-4", "", 0, 0, nil)
	if member != nil {
		t.Error("expected nil for non-OAuth candidate")
	}
}

func TestSelectRouteUnitMember_Empty(t *testing.T) {
	nowISO := time.Now().UTC().Format(time.RFC3339)
	nowMs := time.Now().UnixMilli()

	outerCandidate := RouteChannelCandidate{
		Channel:          store.RouteChannel{OAuthRouteUnitID: ptrInt(1)},
		RouteUnit:        &OAuthRouteUnitSummary{ID: 1, Strategy: "round_robin"},
		RouteUnitMembers: []OAuthRouteUnitMemberCandidate{},
	}

	member := SelectRouteUnitMember(outerCandidate, "gpt-4", nowISO, nowMs, 0, nil)
	if member != nil {
		t.Error("expected nil for empty members")
	}
}

// =============================================================================
// Token value resolution
// =============================================================================

func TestResolveRouteUnitMemberTokenValue(t *testing.T) {
	// Access token preferred
	acct := store.Account{AccessToken: "access-token", APIToken: ptrStr("api-token")}
	if got := ResolveRouteUnitMemberTokenValue(acct); got != "access-token" {
		t.Errorf("expected 'access-token', got %q", got)
	}

	// API token fallback
	acct = store.Account{AccessToken: "", APIToken: ptrStr("api-token")}
	if got := ResolveRouteUnitMemberTokenValue(acct); got != "api-token" {
		t.Errorf("expected 'api-token', got %q", got)
	}

	// Empty API token
	emptyToken := ""
	acct = store.Account{AccessToken: "", APIToken: &emptyToken}
	if got := ResolveRouteUnitMemberTokenValue(acct); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// No tokens
	acct = store.Account{AccessToken: "", APIToken: nil}
	if got := ResolveRouteUnitMemberTokenValue(acct); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// =============================================================================
// IsExplicitTokenChannel
// =============================================================================

func TestIsExplicitTokenChannel(t *testing.T) {
	tokenID := int64(5)
	c := RouteChannelCandidate{
		Channel: store.RouteChannel{TokenID: &tokenID},
	}
	if !IsExplicitTokenChannel(c) {
		t.Error("expected true for channel with token ID")
	}

	tokenID = 0
	c = RouteChannelCandidate{
		Channel: store.RouteChannel{TokenID: &tokenID},
	}
	if IsExplicitTokenChannel(c) {
		t.Error("expected false for token ID 0")
	}

	c = RouteChannelCandidate{
		Channel: store.RouteChannel{TokenID: nil},
	}
	if IsExplicitTokenChannel(c) {
		t.Error("expected false for nil token ID")
	}
}

// =============================================================================
// Member ordering helpers
// =============================================================================

func TestGetRoundRobinRouteUnitMembers_Ordering(t *testing.T) {
	earliest := "2024-01-01T00:00:00Z"
	middle := "2024-03-01T00:00:00Z"
	latest := "2024-06-01T00:00:00Z"

	members := []OAuthRouteUnitMemberCandidate{
		{
			Member:  store.OAuthRouteUnitMember{ID: 1, AccountID: 100, LastSelectedAt: &latest},
			Account: store.Account{ID: 100},
		},
		{
			Member:  store.OAuthRouteUnitMember{ID: 2, AccountID: 101, LastSelectedAt: &earliest},
			Account: store.Account{ID: 101},
		},
		{
			Member:  store.OAuthRouteUnitMember{ID: 3, AccountID: 102, LastSelectedAt: &middle},
			Account: store.Account{ID: 102},
		},
	}

	ordered := getRoundRobinRouteUnitMembers(members)
	if len(ordered) != 3 {
		t.Fatalf("expected 3 members, got %d", len(ordered))
	}
	// Earliest first
	if ordered[0].Account.ID != 101 {
		t.Errorf("expected account 101 first (earliest), got %d", ordered[0].Account.ID)
	}
	if ordered[1].Account.ID != 102 {
		t.Errorf("expected account 102 second, got %d", ordered[1].Account.ID)
	}
	if ordered[2].Account.ID != 100 {
		t.Errorf("expected account 100 last (latest), got %d", ordered[2].Account.ID)
	}
}

func TestGetRoundRobinRouteUnitMembers_SortOrderTiebreaker(t *testing.T) {
	sameTime := "2024-01-01T00:00:00Z"
	members := []OAuthRouteUnitMemberCandidate{
		{
			Member:  store.OAuthRouteUnitMember{ID: 1, AccountID: 300, LastSelectedAt: &sameTime, SortOrder: 5},
			Account: store.Account{ID: 300},
		},
		{
			Member:  store.OAuthRouteUnitMember{ID: 2, AccountID: 100, LastSelectedAt: &sameTime, SortOrder: 1},
			Account: store.Account{ID: 100},
		},
		{
			Member:  store.OAuthRouteUnitMember{ID: 3, AccountID: 200, LastSelectedAt: &sameTime, SortOrder: 3},
			Account: store.Account{ID: 200},
		},
	}

	ordered := getRoundRobinRouteUnitMembers(members)
	// Same time → order by SortOrder ascending
	if ordered[0].Account.ID != 100 {
		t.Errorf("expected account 100 first (sortOrder=1), got %d", ordered[0].Account.ID)
	}
	if ordered[1].Account.ID != 200 {
		t.Errorf("expected account 200 second (sortOrder=3), got %d", ordered[1].Account.ID)
	}
}

func TestGetStickyPreferredRouteUnitMember(t *testing.T) {
	earlier := "2024-01-01T00:00:00Z"
	later := "2024-03-01T00:00:00Z"

	members := []OAuthRouteUnitMemberCandidate{
		{
			Member:  store.OAuthRouteUnitMember{ID: 1, AccountID: 100, LastSelectedAt: &earlier},
			Account: store.Account{ID: 100},
		},
		{
			Member:  store.OAuthRouteUnitMember{ID: 2, AccountID: 101, LastSelectedAt: &later},
			Account: store.Account{ID: 101},
		},
	}

	got := getStickyPreferredRouteUnitMember(members)
	if got == nil {
		t.Fatal("expected a member")
	}
	// Most recent first (descending)
	if got.Account.ID != 101 {
		t.Errorf("expected account 101 (most recent), got %d", got.Account.ID)
	}

	// Empty members
	got = getStickyPreferredRouteUnitMember(nil)
	if got != nil {
		t.Error("expected nil for empty")
	}
}

// =============================================================================
// SelectRouteUnitMember failover path
// =============================================================================

func TestSelectRouteUnitMember_Failover(t *testing.T) {
	nowISO := time.Now().UTC().Format(time.RFC3339)
	nowMs := time.Now().UnixMilli()

	t.Run("failover filters recently failed members", func(t *testing.T) {
		// Create a member that recently failed
		recentFail := time.UnixMilli(nowMs - 5000).UTC().Format(time.RFC3339)
		fc := int64(1)

		outerCandidate := RouteChannelCandidate{
			Channel: store.RouteChannel{
				ID:               999,
				OAuthRouteUnitID: ptrInt(1),
			},
			RouteUnit: &OAuthRouteUnitSummary{ID: 1, Strategy: "round_robin"},
			RouteUnitMembers: []OAuthRouteUnitMemberCandidate{
				{
					Member: store.OAuthRouteUnitMember{
						ID: 1, AccountID: 100, FailCount: fc, LastFailAt: &recentFail,
					},
					Account: store.Account{ID: 100, Status: "active", AccessToken: "t1"},
					Site:    store.Site{ID: 1000, Status: "active"},
				},
				{
					Member: store.OAuthRouteUnitMember{
						ID: 2, AccountID: 101, FailCount: 0,
					},
					Account: store.Account{ID: 101, Status: "active", AccessToken: "t2"},
					Site:    store.Site{ID: 1010, Status: "active"},
				},
			},
		}

		// Failover: exclude channel 999
		member := SelectRouteUnitMember(outerCandidate, "gpt-4", nowISO, nowMs, 0, []int64{999})
		if member == nil {
			t.Fatal("expected a member during failover")
		}
		// Should pick the healthy member (account 101), not the recently failed one
		if member.Account.ID != 101 {
			t.Errorf("expected account 101 (healthy), got %d", member.Account.ID)
		}
	})

	t.Run("failover with all members failed returns nil", func(t *testing.T) {
		recentFail := time.UnixMilli(nowMs - 5000).UTC().Format(time.RFC3339)
		fc := int64(1)

		outerCandidate := RouteChannelCandidate{
			Channel: store.RouteChannel{
				ID:               999,
				OAuthRouteUnitID: ptrInt(1),
			},
			RouteUnit: &OAuthRouteUnitSummary{ID: 1, Strategy: "round_robin"},
			RouteUnitMembers: []OAuthRouteUnitMemberCandidate{
				{
					Member: store.OAuthRouteUnitMember{
						ID: 1, AccountID: 100, FailCount: fc, LastFailAt: &recentFail,
					},
					Account: store.Account{ID: 100, Status: "active", AccessToken: "t1"},
					Site:    store.Site{ID: 1000, Status: "active"},
				},
			},
		}

		member := SelectRouteUnitMember(outerCandidate, "gpt-4", nowISO, nowMs, 0, []int64{999})
		if member != nil {
			t.Error("expected nil when all members recently failed during failover")
		}
	})
}

// =============================================================================
// Helpers
// =============================================================================

func ptrStr(s string) *string { return &s }
