package proxy

import (
	"context"

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
	SelectChannel     func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error)
	SelectNextChannel func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error)
	PreviewSelectedChannel func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error)
	RefreshAuth       func(ctx context.Context, selected *routing.SelectedChannel, failureCtx struct {
		Status       int
		RawErrorText string
	}) (*routing.SelectedChannel, error)
}

// ExecuteInput is the input for the Execute method.
type ExecuteInput struct {
	RequestedModel   string
	DownstreamPolicy routing.DownstreamRoutingPolicy
	Attempt          func(ctx context.Context, input AttemptInput) (AttemptResult, error)
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
	deps ConductorDependencies
}

// NewDefaultProxyConductor creates a new conductor.
func NewDefaultProxyConductor(deps ConductorDependencies) *DefaultProxyConductor {
	return &DefaultProxyConductor{deps: deps}
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
func (c *DefaultProxyConductor) Execute(ctx context.Context, input ExecuteInput) (ExecuteResult, error) {
	excludeChannelIDs := make([]int64, 0)
	attempts := 0

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

		action := failureActionOf(result)

		if isTerminalFailure(action) {
			if input.OnTerminalFailure != nil {
				_ = input.OnTerminalFailure(ctx, selected, struct {
					Status       int
					RawErrorText string
				}{Status: result.Status, RawErrorText: result.RawErrorText})
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

		if shouldRefreshAuth(action) && c.deps.RefreshAuth != nil {
			refreshed, refreshErr := c.deps.RefreshAuth(ctx, selected, struct {
				Status       int
				RawErrorText string
			}{Status: result.Status, RawErrorText: result.RawErrorText})
			if refreshErr == nil && refreshed != nil {
				selected = refreshed
				continue
			}
		}

		if shouldFailover(action) {
			excludeChannelIDs = append(excludeChannelIDs, selected.Channel.ID)
			next, nextErr := c.deps.SelectNextChannel(
				ctx,
				input.RequestedModel,
				excludeChannelIDs,
				input.DownstreamPolicy,
			)
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
