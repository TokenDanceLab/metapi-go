package proxy

import (
	"context"
	"testing"

	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

// sameSiteChannel builds a SelectedChannel pair sharing one site, different channel IDs.
func sameSiteChannel(channelID, accountID, siteID int64) *routing.SelectedChannel {
	return &routing.SelectedChannel{
		ActualModel: "gpt-test",
		Channel: store.RouteChannel{
			ID:      channelID,
			RouteID: 1,
		},
		Account: store.Account{
			ID:     accountID,
			SiteID: siteID,
		},
		Site: store.Site{
			ID:       siteID,
			Name:     "same-site",
			URL:      "https://api.example.com",
			Platform: "openai",
			Status:   "active",
		},
	}
}

// TestConductor_FailoverExcludeIsChannelScopedNotSiteWide proves that after a
// 5xx failure the conductor excludes only the failed channel ID and still
// selects a healthy same-site sibling. This is the request-path half of #585
// cascade isolation (#299).
func TestConductor_FailoverExcludeIsChannelScopedNotSiteWide(t *testing.T) {
	ctx := context.Background()
	siteID := int64(10)
	failed := sameSiteChannel(101, 1001, siteID)
	sibling := sameSiteChannel(102, 1002, siteID)

	var selectNextExclude []int64
	var selectNextCalls int
	attemptChannels := make([]int64, 0, 2)

	conductor := NewDefaultProxyConductor(ConductorDependencies{
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return failed, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			selectNextCalls++
			selectNextExclude = append([]int64(nil), excludeChannelIDs...)
			// Sibling remains eligible even though it shares the failed site.
			if containsInt64(excludeChannelIDs, sibling.Channel.ID) {
				t.Fatalf("same-site sibling %d must not be excluded; got %v", sibling.Channel.ID, excludeChannelIDs)
			}
			if !containsInt64(excludeChannelIDs, failed.Channel.ID) {
				t.Fatalf("failed channel %d must be excluded; got %v", failed.Channel.ID, excludeChannelIDs)
			}
			// Only channel IDs — never site IDs or account IDs.
			for _, id := range excludeChannelIDs {
				if id == siteID || id == failed.Account.ID || id == sibling.Account.ID {
					t.Fatalf("exclude list must be channel-scoped only, got %v", excludeChannelIDs)
				}
			}
			return sibling, nil
		},
	})

	result, err := conductor.Execute(ctx, ExecuteInput{
		RequestedModel:   "gpt-test",
		DownstreamPolicy: routing.EmptyDownstreamRoutingPolicy,
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			attemptChannels = append(attemptChannels, input.Selected.Channel.ID)
			if input.Selected.Channel.ID == failed.Channel.ID {
				// Failover path: 502 → exclude failed channel only.
				return AttemptResult{OK: false, Status: 502, RawErrorText: "bad gateway"}, nil
			}
			return AttemptResult{OK: true, Response: "ok", Status: 200}, nil
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected success via sibling, got ok=false reason=%s status=%d", result.Reason, result.Status)
	}
	if selectNextCalls != 1 {
		t.Fatalf("expected 1 SelectNextChannel call, got %d", selectNextCalls)
	}
	if len(selectNextExclude) != 1 || selectNextExclude[0] != 101 {
		t.Fatalf("expected exclude=[101], got %v", selectNextExclude)
	}
	if len(attemptChannels) != 2 || attemptChannels[0] != 101 || attemptChannels[1] != 102 {
		t.Fatalf("expected attempts [101,102], got %v", attemptChannels)
	}
	if result.Selected == nil || result.Selected.Channel.ID != 102 {
		t.Fatalf("expected selected sibling 102, got %+v", result.Selected)
	}
	if result.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", result.Attempts)
	}
}

// TestConductor_RateLimitFailsOverToSameSiteSibling proves 429 does not pin the
// same channel forever and does not expand exclude to the whole site.
func TestConductor_RateLimitFailsOverToSameSiteSibling(t *testing.T) {
	ctx := context.Background()
	siteID := int64(10)
	failed := sameSiteChannel(201, 2001, siteID)
	sibling := sameSiteChannel(202, 2002, siteID)

	var capturedExclude []int64
	conductor := NewDefaultProxyConductor(ConductorDependencies{
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return failed, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			capturedExclude = append([]int64(nil), excludeChannelIDs...)
			if containsInt64(excludeChannelIDs, sibling.Channel.ID) {
				t.Fatalf("sibling must remain eligible after peer 429; exclude=%v", excludeChannelIDs)
			}
			return sibling, nil
		},
	})

	result, err := conductor.Execute(ctx, ExecuteInput{
		RequestedModel:   "gpt-test",
		DownstreamPolicy: routing.EmptyDownstreamRoutingPolicy,
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			if input.Selected.Channel.ID == failed.Channel.ID {
				return AttemptResult{OK: false, Status: 429, RawErrorText: "rate limit exceeded"}, nil
			}
			return AttemptResult{OK: true, Response: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected sibling success after 429, got reason=%s", result.Reason)
	}
	if len(capturedExclude) != 1 || capturedExclude[0] != 201 {
		t.Fatalf("expected channel-scoped exclude [201], got %v", capturedExclude)
	}
	if result.Selected.Channel.ID != 202 {
		t.Fatalf("expected sibling 202, got %d", result.Selected.Channel.ID)
	}
}

// TestConductor_TimeoutRetriesSameChannelThenFailsOver bounds same-channel
// timeout retries so a sticky dead channel cannot starve same-site siblings.
func TestConductor_TimeoutRetriesSameChannelThenFailsOver(t *testing.T) {
	ctx := context.Background()
	siteID := int64(10)
	failed := sameSiteChannel(301, 3001, siteID)
	sibling := sameSiteChannel(302, 3002, siteID)

	var attemptOrder []int64
	var excludeSeen []int64
	conductor := NewDefaultProxyConductor(ConductorDependencies{
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return failed, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			excludeSeen = append([]int64(nil), excludeChannelIDs...)
			return sibling, nil
		},
	})

	result, err := conductor.Execute(ctx, ExecuteInput{
		RequestedModel:   "gpt-test",
		DownstreamPolicy: routing.EmptyDownstreamRoutingPolicy,
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			attemptOrder = append(attemptOrder, input.Selected.Channel.ID)
			if input.Selected.Channel.ID == failed.Channel.ID {
				return AttemptResult{OK: false, Status: 408, RawErrorText: "request timed out"}, nil
			}
			return AttemptResult{OK: true, Response: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected success after bounded same-channel retries, got reason=%s", result.Reason)
	}
	// First attempt + one same-channel retry + sibling = 3 attempts, order [301,301,302]
	if len(attemptOrder) != 3 || attemptOrder[0] != 301 || attemptOrder[1] != 301 || attemptOrder[2] != 302 {
		t.Fatalf("expected attempt order [301,301,302], got %v", attemptOrder)
	}
	if len(excludeSeen) != 1 || excludeSeen[0] != 301 {
		t.Fatalf("expected exclude [301] only, got %v", excludeSeen)
	}
}

// TestConductor_MultiChannelSameSiteFailureIsolation walks two sequential
// channel failures on the same site and proves exclude accumulates only those
// channel IDs (never site-wide) while a third same-site sibling still wins.
func TestConductor_MultiChannelSameSiteFailureIsolation(t *testing.T) {
	ctx := context.Background()
	siteID := int64(10)
	ch1 := sameSiteChannel(401, 4001, siteID)
	ch2 := sameSiteChannel(402, 4002, siteID)
	ch3 := sameSiteChannel(403, 4003, siteID)

	var excludeSnapshots [][]int64
	conductor := NewDefaultProxyConductor(ConductorDependencies{
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return ch1, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			snap := append([]int64(nil), excludeChannelIDs...)
			excludeSnapshots = append(excludeSnapshots, snap)
			if containsInt64(excludeChannelIDs, ch3.Channel.ID) {
				t.Fatalf("healthy same-site sibling 403 must never appear in exclude: %v", excludeChannelIDs)
			}
			switch len(excludeChannelIDs) {
			case 1:
				if excludeChannelIDs[0] != 401 {
					t.Fatalf("first failover exclude want [401], got %v", excludeChannelIDs)
				}
				return ch2, nil
			case 2:
				if !containsInt64(excludeChannelIDs, 401) || !containsInt64(excludeChannelIDs, 402) {
					t.Fatalf("second failover exclude want 401+402, got %v", excludeChannelIDs)
				}
				return ch3, nil
			default:
				t.Fatalf("unexpected exclude len=%d ids=%v", len(excludeChannelIDs), excludeChannelIDs)
				return nil, nil
			}
		},
	})

	result, err := conductor.Execute(ctx, ExecuteInput{
		RequestedModel:   "gpt-test",
		DownstreamPolicy: routing.EmptyDownstreamRoutingPolicy,
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			switch input.Selected.Channel.ID {
			case 401, 402:
				return AttemptResult{OK: false, Status: 503, RawErrorText: "service unavailable"}, nil
			case 403:
				return AttemptResult{OK: true, Response: "ok"}, nil
			default:
				t.Fatalf("unexpected channel %d", input.Selected.Channel.ID)
				return AttemptResult{}, nil
			}
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.OK || result.Selected == nil || result.Selected.Channel.ID != 403 {
		t.Fatalf("expected success on channel 403, got ok=%v selected=%+v", result.OK, result.Selected)
	}
	if len(excludeSnapshots) != 2 {
		t.Fatalf("expected 2 SelectNextChannel calls, got %d", len(excludeSnapshots))
	}
	if result.Attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", result.Attempts)
	}
}

func TestAppendExcludedChannelID_ChannelScopedDedup(t *testing.T) {
	got := appendExcludedChannelID(nil, 10)
	got = appendExcludedChannelID(got, 10)
	got = appendExcludedChannelID(got, 11)
	got = appendExcludedChannelID(got, 0)
	if len(got) != 2 || got[0] != 10 || got[1] != 11 {
		t.Fatalf("expected [10,11], got %v", got)
	}
}

func TestFailureActionOf_DoesNotPinRateLimitOnSameChannel(t *testing.T) {
	if got := failureActionOf(AttemptResult{Status: 429, RawErrorText: "rate limit"}); got != ActionFailover {
		t.Fatalf("429 want failover, got %s", got)
	}
	if got := failureActionOf(AttemptResult{Status: 408, RawErrorText: "request timed out"}); got != ActionRetrySameChannel {
		t.Fatalf("timeout-like 408 want retry_same_channel, got %s", got)
	}
	if got := failureActionOf(AttemptResult{Status: 408, RawErrorText: ""}); got != ActionFailover {
		t.Fatalf("bare 408 want failover, got %s", got)
	}
	if got := failureActionOf(AttemptResult{Status: 502, RawErrorText: "bad gateway"}); got != ActionFailover {
		t.Fatalf("502 want failover, got %s", got)
	}
	if got := failureActionOf(AttemptResult{Status: 400, RawErrorText: "invalid request body"}); got != ActionTerminal {
		t.Fatalf("400 want terminal, got %s", got)
	}
}
