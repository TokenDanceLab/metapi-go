package proxy

import (
	"context"
	"log/slog"

	"github.com/tokendancelab/metapi-go/routing"
)

// AttemptFailureAction indicates what the conductor should do after a failed attempt.
type AttemptFailureAction string

const (
	ActionRetrySameChannel AttemptFailureAction = "retry_same_channel"
	ActionRefreshAuth      AttemptFailureAction = "refresh_auth"
	ActionFailover         AttemptFailureAction = "failover"
	ActionTerminal         AttemptFailureAction = "terminal"
	ActionStop             AttemptFailureAction = "stop"
)

// DefaultMaxRefreshAuthSuccesses caps successful RefreshAuth calls per channel
// before the conductor fails over with a channel-scoped exclude.
const DefaultMaxRefreshAuthSuccesses = 1

// AttemptResult is the result of an attempt callback.
type AttemptResult struct {
	OK           bool
	Response     any
	Status       int
	RawErrorText string
	LatencyMs    int64
	Cost         float64
}

// AttemptInput is the input for an attempt callback.
type AttemptInput struct {
	Selected          *routing.SelectedChannel
	AttemptIndex      int
	ExcludeChannelIDs []int64
}

// ConductorDependencies are the injected dependencies for DefaultProxyConductor.
type ConductorDependencies struct {
	SelectChannel          func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error)
	SelectNextChannel      func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error)
	PreviewSelectedChannel func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error)
	RefreshAuth            func(ctx context.Context, selected *routing.SelectedChannel, failureCtx struct {
		Status       int
		RawErrorText string
	}) (*routing.SelectedChannel, error)
	// MaxAttempts is the hard total attempt budget covering same-channel retries,
	// auth refresh retries, and cross-channel failover. Values <= 0 resolve via
	// GetProxyMaxChannelAttempts (minimum 1). Prefer wiring config.ProxyMaxChannelAttempts.
	MaxAttempts int
	// MaxRefreshAuthSuccesses caps successful RefreshAuth calls per selected channel
	// before failover. Values <= 0 use DefaultMaxRefreshAuthSuccesses.
	MaxRefreshAuthSuccesses int
}

// ExecuteInput is the input for the Execute method.
type ExecuteInput struct {
	RequestedModel    string
	DownstreamPolicy  routing.DownstreamRoutingPolicy
	Attempt           func(ctx context.Context, input AttemptInput) (AttemptResult, error)
	OnTerminalFailure func(ctx context.Context, selected *routing.SelectedChannel, failureCtx struct {
		Status       int
		RawErrorText string
	}) error
}

// ExecuteResult is the result of the Execute method.
type ExecuteResult struct {
	OK           bool
	Reason       string // "no_channel", "terminal", "failed"
	Selected     *routing.SelectedChannel
	Response     any
	Status       int
	RawErrorText string
	Attempts     int
}

// DefaultProxyConductor implements action-based retry for non-surface flows.
type DefaultProxyConductor struct {
	deps                    ConductorDependencies
	maxAttempts             int
	maxRefreshAuthSuccesses int
}

// NewDefaultProxyConductor creates a new conductor.
func NewDefaultProxyConductor(deps ConductorDependencies) *DefaultProxyConductor {
	maxRefresh := deps.MaxRefreshAuthSuccesses
	if maxRefresh <= 0 {
		maxRefresh = DefaultMaxRefreshAuthSuccesses
	}
	return &DefaultProxyConductor{
		deps:                    deps,
		maxAttempts:             GetProxyMaxChannelAttempts(deps.MaxAttempts),
		maxRefreshAuthSuccesses: maxRefresh,
	}
}

// PreviewSelectedChannel returns the selected channel without executing a request.
func (c *DefaultProxyConductor) PreviewSelectedChannel(
	ctx context.Context,
	requestedModel string,
	downstreamPolicy routing.DownstreamRoutingPolicy,
) (*routing.SelectedChannel, error) {
	if c.deps.PreviewSelectedChannel != nil {
		return c.deps.PreviewSelectedChannel(ctx, requestedModel, downstreamPolicy)
	}
	return c.deps.SelectChannel(ctx, requestedModel, downstreamPolicy)
}

// Execute runs the action-based retry conductor loop.
//
// Hard budget: attempts never exceed maxAttempts (same-channel + refresh + failover).
// RefreshAuth successes are capped per channel; nil/error RefreshAuth fails over.
func (c *DefaultProxyConductor) Execute(ctx context.Context, input ExecuteInput) (ExecuteResult, error) {
	excludeChannelIDs := make([]int64, 0)
	attempts := 0
	refreshAuthSuccesses := 0

	selected, err := c.deps.SelectChannel(ctx, input.RequestedModel, input.DownstreamPolicy)
	if err != nil {
		return ExecuteResult{OK: false, Reason: "no_channel", Attempts: 0}, err
	}
	if selected == nil {
		return ExecuteResult{OK: false, Reason: "no_channel", Attempts: 0}, nil
	}

	for selected != nil {
		result, err := input.Attempt(ctx, AttemptInput{
			Selected:          selected,
			AttemptIndex:      attempts,
			ExcludeChannelIDs: excludeChannelIDs,
		})
		attempts++

		if err != nil {
			return ExecuteResult{OK: false, Reason: "failed", Selected: selected, Attempts: attempts}, err
		}

		if result.OK {
			// recordSuccessfulAttempt is a no-op in the conductor;
			// surface handling does its own success recording.
			return ExecuteResult{
				OK:       true,
				Selected: selected,
				Response: result.Response,
				Attempts: attempts,
			}, nil
		}

		// Budget covers every finished attempt (success already returned above).
		if attempts >= c.maxAttempts {
			return ExecuteResult{
				OK:           false,
				Reason:       "failed",
				Selected:     selected,
				Status:       result.Status,
				RawErrorText: result.RawErrorText,
				Attempts:     attempts,
			}, nil
		}

		action := failureActionOf(result)

		if isTerminalFailure(action) {
			if input.OnTerminalFailure != nil {
				if err := input.OnTerminalFailure(ctx, selected, struct {
					Status       int
					RawErrorText string
				}{Status: result.Status, RawErrorText: result.RawErrorText}); err != nil {
					slog.Warn("OnTerminalFailure callback failed", "err", err)
				}
			}
			return ExecuteResult{
				OK:           false,
				Reason:       "terminal",
				Selected:     selected,
				Status:       result.Status,
				RawErrorText: result.RawErrorText,
				Attempts:     attempts,
			}, nil
		}

		if shouldRetrySameChannel(action) {
			continue
		}

		if shouldRefreshAuth(action) {
			canRefresh := c.deps.RefreshAuth != nil && refreshAuthSuccesses < c.maxRefreshAuthSuccesses
			if canRefresh {
				refreshed, refreshErr := c.deps.RefreshAuth(ctx, selected, struct {
					Status       int
					RawErrorText string
				}{Status: result.Status, RawErrorText: result.RawErrorText})
				if refreshErr == nil && refreshed != nil {
					selected = refreshed
					refreshAuthSuccesses++
					continue
				}
			}
			// nil RefreshAuth, refresh error/nil result, or refresh success cap → failover.
			next, nextErr, nextExclude := c.failover(ctx, input, selected, excludeChannelIDs)
			excludeChannelIDs = nextExclude
			if nextErr != nil || next == nil {
				return ExecuteResult{
					OK:           false,
					Reason:       "failed",
					Selected:     selected,
					Status:       result.Status,
					RawErrorText: result.RawErrorText,
					Attempts:     attempts,
				}, nextErr
			}
			selected = next
			refreshAuthSuccesses = 0
			continue
		}

		if shouldFailover(action) {
			next, nextErr, nextExclude := c.failover(ctx, input, selected, excludeChannelIDs)
			excludeChannelIDs = nextExclude
			if nextErr != nil || next == nil {
				return ExecuteResult{
					OK:           false,
					Reason:       "failed",
					Selected:     selected,
					Status:       result.Status,
					RawErrorText: result.RawErrorText,
					Attempts:     attempts,
				}, nextErr
			}
			selected = next
			refreshAuthSuccesses = 0
			continue
		}

		return ExecuteResult{
			OK:           false,
			Reason:       "failed",
			Selected:     selected,
			Status:       result.Status,
			RawErrorText: result.RawErrorText,
			Attempts:     attempts,
		}, nil
	}

	return ExecuteResult{OK: false, Reason: "failed", Attempts: attempts}, nil
}

func (c *DefaultProxyConductor) failover(
	ctx context.Context,
	input ExecuteInput,
	selected *routing.SelectedChannel,
	excludeChannelIDs []int64,
) (*routing.SelectedChannel, error, []int64) {
	excludeChannelIDs = append(excludeChannelIDs, selected.Channel.ID)
	if c.deps.SelectNextChannel == nil {
		return nil, nil, excludeChannelIDs
	}
	next, nextErr := c.deps.SelectNextChannel(
		ctx,
		input.RequestedModel,
		excludeChannelIDs,
		input.DownstreamPolicy,
	)
	return next, nextErr, excludeChannelIDs
}

// failureActionOf determines the action for a failed attempt.
// Uses HTTP status classification matching TS behavior:
//   - >= 500: failover
//   - 408/429 + retryable pattern: retry_same_channel
//   - 408/429: failover
//   - 401/403: refresh_auth
//   - Other: terminal
func failureActionOf(result AttemptResult) AttemptFailureAction {
	if result.Status >= 500 {
		return ActionFailover
	}
	if result.Status == 408 || result.Status == 429 || result.Status == 425 {
		// If error text matches certain patterns, retry same channel
		if ShouldRetryProxyRequest(result.Status, result.RawErrorText) {
			return ActionRetrySameChannel
		}
		return ActionFailover
	}
	if result.Status == 401 || result.Status == 403 {
		return ActionRefreshAuth
	}
	return ActionTerminal
}

func isTerminalFailure(action AttemptFailureAction) bool {
	return action == ActionTerminal || action == ActionStop
}

func shouldRetrySameChannel(action AttemptFailureAction) bool {
	return action == ActionRetrySameChannel
}

func shouldRefreshAuth(action AttemptFailureAction) bool {
	return action == ActionRefreshAuth
}

func shouldFailover(action AttemptFailureAction) bool {
	return action == ActionFailover
}
