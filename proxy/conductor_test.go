package proxy

import (
	"context"
	"errors"
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
		MaxAttempts: 5,
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
		MaxAttempts: 5,
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
		MaxAttempts: 5,
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
		MaxAttempts: 5,
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

func conductorChannel(id int64) *routing.SelectedChannel {
	ch := makeChannel(id, 1, 100+id)
	return &ch
}

func TestConductor_NChannel5xxStormStopsAtBudget(t *testing.T) {
	// Many channels available; every attempt is 5xx. Conductor must stop at MaxAttempts
	// even though SelectNextChannel would keep returning siblings.
	const budget = 3
	const channelCount = 10

	channels := make([]*routing.SelectedChannel, channelCount)
	for i := 0; i < channelCount; i++ {
		channels[i] = conductorChannel(int64(i + 1))
	}

	selectCalls := 0
	nextCalls := 0
	attemptCalls := 0
	var seenChannelIDs []int64
	var lastExclude []int64

	c := NewDefaultProxyConductor(ConductorDependencies{
		MaxAttempts: budget,
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			selectCalls++
			return channels[0], nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			nextCalls++
			lastExclude = append([]int64(nil), excludeChannelIDs...)
			excluded := map[int64]bool{}
			for _, id := range excludeChannelIDs {
				excluded[id] = true
			}
			for _, ch := range channels {
				if !excluded[ch.Channel.ID] {
					return ch, nil
				}
			}
			return nil, nil
		},
	})

	result, err := c.Execute(context.Background(), ExecuteInput{
		RequestedModel: "gpt-4",
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			attemptCalls++
			seenChannelIDs = append(seenChannelIDs, input.Selected.Channel.ID)
			return AttemptResult{
				OK:           false,
				Status:       503,
				RawErrorText: "service unavailable",
			}, nil
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("expected failure under 5xx storm")
	}
	if result.Reason != "failed" {
		t.Fatalf("reason = %q, want failed", result.Reason)
	}
	if result.Status != 503 {
		t.Fatalf("status = %d, want 503", result.Status)
	}
	if result.Attempts != budget {
		t.Fatalf("attempts = %d, want budget %d", result.Attempts, budget)
	}
	if attemptCalls != budget {
		t.Fatalf("attempt callbacks = %d, want %d", attemptCalls, budget)
	}
	if selectCalls != 1 {
		t.Fatalf("SelectChannel calls = %d, want 1", selectCalls)
	}
	// budget-1 failovers after failed attempts that still have budget remaining
	if nextCalls != budget-1 {
		t.Fatalf("SelectNextChannel calls = %d, want %d", nextCalls, budget-1)
	}
	if len(seenChannelIDs) != budget {
		t.Fatalf("seen channels = %v, want %d entries", seenChannelIDs, budget)
	}
	// Failover must exclude prior channel IDs
	if len(lastExclude) == 0 {
		t.Fatal("expected non-empty exclude list on last failover")
	}
	// No infinite walk: never contacted more channels than the budget allows
	if len(seenChannelIDs) > budget {
		t.Fatalf("walked too many channels: %v", seenChannelIDs)
	}
}

func TestConductor_401RefreshAuthNilTriesSibling(t *testing.T) {
	ch1 := conductorChannel(1)
	ch2 := conductorChannel(2)

	var attemptChannelIDs []int64
	var excludeOnNext []int64
	refreshCalled := false

	c := NewDefaultProxyConductor(ConductorDependencies{
		MaxAttempts: 3,
		// RefreshAuth intentionally nil — 401 must still failover to sibling.
		RefreshAuth: nil,
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return ch1, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			excludeOnNext = append([]int64(nil), excludeChannelIDs...)
			return ch2, nil
		},
	})

	result, err := c.Execute(context.Background(), ExecuteInput{
		RequestedModel: "gpt-4",
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			attemptChannelIDs = append(attemptChannelIDs, input.Selected.Channel.ID)
			if input.Selected.Channel.ID == 1 {
				return AttemptResult{OK: false, Status: 401, RawErrorText: "unauthorized"}, nil
			}
			return AttemptResult{OK: true, Response: "ok-from-sibling"}, nil
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected success on sibling, got reason=%s status=%d", result.Reason, result.Status)
	}
	if result.Response != "ok-from-sibling" {
		t.Fatalf("response = %v", result.Response)
	}
	if refreshCalled {
		t.Fatal("RefreshAuth should not have been called (nil)")
	}
	if len(attemptChannelIDs) != 2 || attemptChannelIDs[0] != 1 || attemptChannelIDs[1] != 2 {
		t.Fatalf("attempt channels = %v, want [1 2]", attemptChannelIDs)
	}
	if len(excludeOnNext) != 1 || excludeOnNext[0] != 1 {
		t.Fatalf("exclude on failover = %v, want [1]", excludeOnNext)
	}
	if result.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", result.Attempts)
	}
}

func TestConductor_401RefreshAuthErrorFailsOver(t *testing.T) {
	ch1 := conductorChannel(1)
	ch2 := conductorChannel(2)
	refreshCalls := 0

	c := NewDefaultProxyConductor(ConductorDependencies{
		MaxAttempts: 3,
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return ch1, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return ch2, nil
		},
		RefreshAuth: func(ctx context.Context, selected *routing.SelectedChannel, failureCtx struct {
			Status       int
			RawErrorText string
		}) (*routing.SelectedChannel, error) {
			refreshCalls++
			return nil, errors.New("refresh failed")
		},
	})

	var attemptIDs []int64
	result, err := c.Execute(context.Background(), ExecuteInput{
		RequestedModel: "gpt-4",
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			attemptIDs = append(attemptIDs, input.Selected.Channel.ID)
			if input.Selected.Channel.ID == 1 {
				return AttemptResult{OK: false, Status: 403, RawErrorText: "forbidden"}, nil
			}
			return AttemptResult{OK: true, Response: "sibling"}, nil
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected sibling success, got %+v", result)
	}
	if refreshCalls != 1 {
		t.Fatalf("refreshCalls = %d, want 1", refreshCalls)
	}
	if len(attemptIDs) != 2 || attemptIDs[0] != 1 || attemptIDs[1] != 2 {
		t.Fatalf("attemptIDs = %v, want [1 2]", attemptIDs)
	}
}

func TestConductor_RefreshAuthSuccessCappedThenFailover(t *testing.T) {
	ch1 := conductorChannel(1)
	ch2 := conductorChannel(2)
	refreshCalls := 0
	var attemptIDs []int64

	c := NewDefaultProxyConductor(ConductorDependencies{
		MaxAttempts:             5,
		MaxRefreshAuthSuccesses: 1,
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return ch1, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			if len(excludeChannelIDs) == 0 || excludeChannelIDs[len(excludeChannelIDs)-1] != 1 {
				t.Fatalf("expected channel 1 excluded, got %v", excludeChannelIDs)
			}
			return ch2, nil
		},
		RefreshAuth: func(ctx context.Context, selected *routing.SelectedChannel, failureCtx struct {
			Status       int
			RawErrorText string
		}) (*routing.SelectedChannel, error) {
			refreshCalls++
			// Return same channel with "refreshed" token semantics.
			return selected, nil
		},
	})

	result, err := c.Execute(context.Background(), ExecuteInput{
		RequestedModel: "gpt-4",
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			attemptIDs = append(attemptIDs, input.Selected.Channel.ID)
			// Channel 1 always 401; channel 2 succeeds.
			if input.Selected.Channel.ID == 1 {
				return AttemptResult{OK: false, Status: 401, RawErrorText: "unauthorized"}, nil
			}
			return AttemptResult{OK: true, Response: "ok"}, nil
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected success, got %+v", result)
	}
	// First 401 → refresh once → second 401 → refresh cap → failover to ch2 → success.
	if refreshCalls != 1 {
		t.Fatalf("refreshCalls = %d, want 1 (capped)", refreshCalls)
	}
	if len(attemptIDs) != 3 || attemptIDs[0] != 1 || attemptIDs[1] != 1 || attemptIDs[2] != 2 {
		t.Fatalf("attemptIDs = %v, want [1 1 2]", attemptIDs)
	}
	if result.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", result.Attempts)
	}
}

func TestConductor_SameChannelRetryRespectsBudget(t *testing.T) {
	ch1 := conductorChannel(1)
	nextCalls := 0
	attemptCalls := 0

	c := NewDefaultProxyConductor(ConductorDependencies{
		MaxAttempts: 2,
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return ch1, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			nextCalls++
			return conductorChannel(2), nil
		},
	})

	result, err := c.Execute(context.Background(), ExecuteInput{
		RequestedModel: "gpt-4",
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			attemptCalls++
			// 408 with retryable classification → same-channel retry until budget.
			return AttemptResult{OK: false, Status: 408, RawErrorText: "request timed out"}, nil
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK || result.Reason != "failed" {
		t.Fatalf("expected budget-exhausted failure, got %+v", result)
	}
	if result.Attempts != 2 || attemptCalls != 2 {
		t.Fatalf("attempts=%d attemptCalls=%d, want 2", result.Attempts, attemptCalls)
	}
	if nextCalls != 0 {
		t.Fatalf("same-channel path should not failover before budget stop, nextCalls=%d", nextCalls)
	}
	if result.Status != 408 {
		t.Fatalf("status = %d, want 408", result.Status)
	}
}

func TestConductor_TerminalDoesNotFailover(t *testing.T) {
	ch1 := conductorChannel(1)
	nextCalls := 0

	c := NewDefaultProxyConductor(ConductorDependencies{
		MaxAttempts: 5,
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return ch1, nil
		},
		SelectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			nextCalls++
			return conductorChannel(2), nil
		},
	})

	result, err := c.Execute(context.Background(), ExecuteInput{
		RequestedModel: "gpt-4",
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			return AttemptResult{OK: false, Status: 400, RawErrorText: "bad request"}, nil
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != "terminal" {
		t.Fatalf("reason = %q, want terminal", result.Reason)
	}
	if nextCalls != 0 {
		t.Fatalf("terminal must not failover, nextCalls=%d", nextCalls)
	}
	if result.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", result.Attempts)
	}
}

func TestConductor_NoChannel(t *testing.T) {
	c := NewDefaultProxyConductor(ConductorDependencies{
		MaxAttempts: 3,
		SelectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			return nil, nil
		},
	})
	result, err := c.Execute(context.Background(), ExecuteInput{
		RequestedModel: "gpt-4",
		Attempt: func(ctx context.Context, input AttemptInput) (AttemptResult, error) {
			t.Fatal("Attempt should not be called")
			return AttemptResult{}, nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != "no_channel" || result.Attempts != 0 {
		t.Fatalf("got %+v", result)
	}
}

func TestNewDefaultProxyConductor_Defaults(t *testing.T) {
	c := NewDefaultProxyConductor(ConductorDependencies{})
	if c.maxAttempts != 1 {
		// GetProxyMaxChannelAttempts(0) → 1
		t.Fatalf("maxAttempts = %d, want 1", c.maxAttempts)
	}
	if c.maxRefreshAuthSuccesses != DefaultMaxRefreshAuthSuccesses {
		t.Fatalf("maxRefreshAuthSuccesses = %d, want %d", c.maxRefreshAuthSuccesses, DefaultMaxRefreshAuthSuccesses)
	}

	c2 := NewDefaultProxyConductor(ConductorDependencies{MaxAttempts: 7, MaxRefreshAuthSuccesses: 2})
	if c2.maxAttempts != 7 {
		t.Fatalf("maxAttempts = %d, want 7", c2.maxAttempts)
	}
	if c2.maxRefreshAuthSuccesses != 2 {
		t.Fatalf("maxRefreshAuthSuccesses = %d, want 2", c2.maxRefreshAuthSuccesses)
	}
}
