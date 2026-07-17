package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/tokendancelab/metapi-go/routing"
)

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
