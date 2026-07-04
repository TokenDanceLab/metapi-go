package proxy

import (
	"context"
	"fmt"

	"github.com/tokendancelab/metapi-go/routing"
)

// SurfaceWarningScope is the warning scope for surface operations.
type SurfaceWarningScope string

const (
	ScopeChat      SurfaceWarningScope = "chat"
	ScopeResponses SurfaceWarningScope = "responses"
)

// SurfaceSelectedChannel is a lightweight selected channel for surface operations.
type SurfaceSelectedChannel struct {
	Channel     SurfaceChannelRef
	Account     SurfaceAccountRef
	Site        SurfaceSiteRef
	ActualModel string
}

// SurfaceChannelRef is a lightweight channel reference.
type SurfaceChannelRef struct {
	RouteID *int64
	ID      int64
}

// SurfaceAccountRef is a lightweight account reference.
type SurfaceAccountRef struct {
	ID        int64
	Username  *string
	ExtraConfig *string
	OAuthProvider *string
}

// SurfaceSiteRef is a lightweight site reference.
type SurfaceSiteRef struct {
	Name     *string
	ID       int64
	URL      string
	Platform string
}

// SurfaceFailureResponse is a terminal failure response.
type SurfaceFailureResponse struct {
	Action  string // "respond" or "retry"
	Status  int
	Payload map[string]any
}

// ConvertToSurfaceSelectedChannel converts a routing.SelectedChannel to a SurfaceSelectedChannel.
func ConvertToSurfaceSelectedChannel(sel *routing.SelectedChannel) SurfaceSelectedChannel {
	var routeID *int64
	if sel.Channel.RouteID != 0 {
		routeID = &sel.Channel.RouteID
	}

	var username *string
	if sel.Account.Username != nil && *sel.Account.Username != "" {
		username = sel.Account.Username
	}

	// Get oauthProvider from account OAuth fields
	var oauthProvider *string
	if sel.Account.OAuthProvider != nil && *sel.Account.OAuthProvider != "" {
		oauthProvider = sel.Account.OAuthProvider
	}

	return SurfaceSelectedChannel{
		Channel: SurfaceChannelRef{
			RouteID: routeID,
			ID:      sel.Channel.ID,
		},
		Account: SurfaceAccountRef{
			ID:           sel.Account.ID,
			Username:     username,
			ExtraConfig:  sel.Account.ExtraConfig,
			OAuthProvider: oauthProvider,
		},
		Site: SurfaceSiteRef{
			Name:     &sel.Site.Name,
			ID:       sel.Site.ID,
			URL:      sel.Site.URL,
			Platform: sel.Site.Platform,
		},
		ActualModel: sel.ActualModel,
	}
}

// SurfaceFailureToolkit provides common failure handling methods for surfaces.
type SurfaceFailureToolkit struct {
	WarningScope      SurfaceWarningScope
	DownstreamPath    string
	MaxRetries        int
	Router            TokenRouterInterface
	Coord             *ProxyChannelCoordinator
	LogProxy          func(ctx context.Context, entry ProxyLogEntry) error
	ReportAllFailed   func(model string, reason string)
	ReportTokenExpired func(accountID int64, username *string, siteName *string, detail string)
}

// ProxyLogEntry is a single proxy log row.
type ProxyLogEntry struct {
	RouteID             *int64
	ChannelID           *int64
	AccountID           *int64
	DownstreamAPIKeyID  *int64
	ModelRequested      string
	ModelActual         *string
	Status              string
	HTTPStatus          int
	IsStream            *bool
	FirstByteLatencyMs  *int64
	LatencyMs           int64
	PromptTokens        *int64
	CompletionTokens    *int64
	TotalTokens         *int64
	EstimatedCost       float64
	BillingDetails      any
	ClientFamily        string
	ClientAppID         string
	ClientAppName       string
	ClientConfidence    string
	ErrorMessage        *string
	RetryCount          int
	UpstreamPath        *string
	UsageSource         string
}

// SurfaceUsageSummary is a usage summary for surface operations.
type SurfaceUsageSummary struct {
	PromptTokens            int64
	CompletionTokens        int64
	TotalTokens             int64
	CacheReadTokens         int64
	CacheCreationTokens     int64
	PromptTokensIncludeCache *bool
}

// ---- Shared Surface Helpers ----

// BuildSurfaceStickySessionKey builds a sticky session key for a surface request.
func BuildSurfaceStickySessionKey(
	coord *ProxyChannelCoordinator,
	clientKind string,
	sessionID string,
	requestedModel string,
	downstreamPath string,
	downstreamAPIKeyID *int64,
) string {
	return coord.BuildStickySessionKey(clientKind, sessionID, requestedModel, downstreamPath, downstreamAPIKeyID)
}

// GetSurfaceStickyPreferredChannelID returns the preferred channel ID from the sticky session.
func GetSurfaceStickyPreferredChannelID(coord *ProxyChannelCoordinator, key string) *int64 {
	if key == "" {
		return nil
	}
	id := coord.GetStickyChannelID(key)
	if id <= 0 {
		return nil
	}
	return &id
}

// BindSurfaceStickyChannel binds a surface request to a sticky channel.
func BindSurfaceStickyChannel(
	coord *ProxyChannelCoordinator,
	key string,
	channelID int64,
	extraConfig *string,
	oauthProvider *string,
) {
	coord.BindStickyChannel(key, channelID, extraConfig, oauthProvider)
}

// ClearSurfaceStickyChannel clears the sticky channel binding.
func ClearSurfaceStickyChannel(coord *ProxyChannelCoordinator, key string, channelID int64) {
	coord.ClearStickyChannel(key, channelID)
}

// AcquireSurfaceChannelLease acquires a channel lease for a surface request.
// Uses channelId=0 (noop) when no sticky session key is present.
func AcquireSurfaceChannelLease(
	coord *ProxyChannelCoordinator,
	stickySessionKey string,
	channelID int64,
	extraConfig *string,
	oauthProvider *string,
) AcquireResult {
	leaseChannelID := int64(0)
	if stickySessionKey != "" {
		leaseChannelID = channelID
	}
	return coord.AcquireChannelLease(leaseChannelID, extraConfig, oauthProvider)
}

// BuildSurfaceChannelBusyMessage builds a human-readable channel busy message.
func BuildSurfaceChannelBusyMessage(waitMs int64) string {
	if waitMs > 0 {
		return fmt.Sprintf("Channel busy: waited %dms for an available session slot", waitMs)
	}
	return "Channel busy: no session slot available"
}

// ---- Failure Toolkit ----

// NewSurfaceFailureToolkit creates a new failure toolkit.
func NewSurfaceFailureToolkit(
	scope SurfaceWarningScope,
	downstreamPath string,
	maxRetries int,
	router TokenRouterInterface,
	coord *ProxyChannelCoordinator,
	logProxy func(ctx context.Context, entry ProxyLogEntry) error,
	reportAllFailed func(model string, reason string),
	reportTokenExpired func(accountID int64, username *string, siteName *string, detail string),
) *SurfaceFailureToolkit {
	return &SurfaceFailureToolkit{
		WarningScope:      scope,
		DownstreamPath:    downstreamPath,
		MaxRetries:        maxRetries,
		Router:            router,
		Coord:             coord,
		LogProxy:          logProxy,
		ReportAllFailed:   reportAllFailed,
		ReportTokenExpired: reportTokenExpired,
	}
}

// HandleUpstreamFailure handles an upstream endpoint failure.
// Records failure, writes proxy log, and decides retry vs respond.
func (ft *SurfaceFailureToolkit) HandleUpstreamFailure(
	ctx context.Context,
	selected SurfaceSelectedChannel,
	requestedModel string,
	modelName string,
	status int,
	errText string,
	rawErrText string,
	isStream bool,
	latencyMs int64,
	retryCount int,
) SurfaceFailureResponse {
	// Record failure
	statusInt := status
	_ = ft.Router.RecordFailure(ctx, selected.Channel.ID, routing.SiteRuntimeFailureContext{
		Status:    &statusInt,
		ErrorText: &rawErrText,
		ModelName: &modelName,
	}, nil)

	// Write proxy log
	_ = ft.LogProxy(ctx, ProxyLogEntry{
		RouteID:        selected.Channel.RouteID,
		ChannelID:      &selected.Channel.ID,
		AccountID:      &selected.Account.ID,
		ModelRequested: requestedModel,
		ModelActual:    &modelName,
		Status:         "failed",
		HTTPStatus:     status,
		LatencyMs:      latencyMs,
		ErrorMessage:   &errText,
		RetryCount:     retryCount,
	})

	// Check retry
	if ShouldRetryProxyRequest(status, errText) {
		if retryCount < ft.MaxRetries {
			return SurfaceFailureResponse{Action: "retry"}
		}
	}

	// Report all failed
	if ft.ReportAllFailed != nil {
		ft.ReportAllFailed(requestedModel, fmt.Sprintf("upstream returned HTTP %d", status))
	}

	return SurfaceFailureResponse{
		Action: "respond",
		Status: status,
		Payload: map[string]any{
			"error": map[string]any{
				"message": errText,
				"type":    "upstream_error",
			},
		},
	}
}

// HandleDetectedFailure handles a content-based failure detection.
func (ft *SurfaceFailureToolkit) HandleDetectedFailure(
	ctx context.Context,
	selected SurfaceSelectedChannel,
	requestedModel string,
	modelName string,
	failure *FailureResult,
	latencyMs int64,
	retryCount int,
	promptTokens int64,
	completionTokens int64,
	totalTokens int64,
	upstreamPath string,
) SurfaceFailureResponse {
	// Record failure
	failStatus := failure.Status
	failReason := failure.Reason
	_ = ft.Router.RecordFailure(ctx, selected.Channel.ID, routing.SiteRuntimeFailureContext{
		Status:    &failStatus,
		ErrorText: &failReason,
		ModelName: &modelName,
	}, nil)

	// Write proxy log
	_ = ft.LogProxy(ctx, ProxyLogEntry{
		RouteID:          selected.Channel.RouteID,
		ChannelID:        &selected.Channel.ID,
		AccountID:        &selected.Account.ID,
		ModelRequested:   requestedModel,
		ModelActual:      &modelName,
		Status:           "failed",
		HTTPStatus:       failure.Status,
		LatencyMs:        latencyMs,
		PromptTokens:     &promptTokens,
		CompletionTokens: &completionTokens,
		TotalTokens:      &totalTokens,
		ErrorMessage:     &failure.Reason,
		RetryCount:       retryCount,
		UpstreamPath:     &upstreamPath,
	})

	// Check retry
	if ShouldRetryProxyRequest(failure.Status, failure.Reason) {
		if retryCount < ft.MaxRetries {
			return SurfaceFailureResponse{Action: "retry"}
		}
	}

	if ft.ReportAllFailed != nil {
		ft.ReportAllFailed(requestedModel, failure.Reason)
	}

	return SurfaceFailureResponse{
		Action: "respond",
		Status: failure.Status,
		Payload: map[string]any{
			"error": map[string]any{
				"message": failure.Reason,
				"type":    "upstream_error",
			},
		},
	}
}

// HandleExecutionError handles a non-HTTP execution error (network failure, etc.).
func (ft *SurfaceFailureToolkit) HandleExecutionError(
	ctx context.Context,
	selected SurfaceSelectedChannel,
	requestedModel string,
	modelName string,
	errorMessage string,
	latencyMs int64,
	retryCount int,
) SurfaceFailureResponse {
	// Record failure
	_ = ft.Router.RecordFailure(ctx, selected.Channel.ID, routing.SiteRuntimeFailureContext{
		ErrorText: &errorMessage,
		ModelName: &modelName,
	}, nil)

	// Write proxy log
	_ = ft.LogProxy(ctx, ProxyLogEntry{
		RouteID:        selected.Channel.RouteID,
		ChannelID:      &selected.Channel.ID,
		AccountID:      &selected.Account.ID,
		ModelRequested: requestedModel,
		ModelActual:    &modelName,
		Status:         "failed",
		HTTPStatus:     0,
		LatencyMs:      latencyMs,
		ErrorMessage:   &errorMessage,
		RetryCount:     retryCount,
	})

	if retryCount < ft.MaxRetries {
		return SurfaceFailureResponse{Action: "retry"}
	}

	if ft.ReportAllFailed != nil {
		ft.ReportAllFailed(requestedModel, errorMessage)
	}

	return SurfaceFailureResponse{
		Action: "respond",
		Status: 502,
		Payload: map[string]any{
			"error": map[string]any{
				"message": "Upstream error: " + errorMessage,
				"type":    "upstream_error",
			},
		},
	}
}

// RecordStreamFailure records a stream-level failure.
func (ft *SurfaceFailureToolkit) RecordStreamFailure(
	ctx context.Context,
	selected SurfaceSelectedChannel,
	requestedModel string,
	modelName string,
	errorMessage string,
	latencyMs int64,
	retryCount int,
	promptTokens int64,
	completionTokens int64,
	totalTokens int64,
	upstreamPath string,
	httpStatus int,
	runtimeFailureStatus *int,
) {
	// Record failure
	if runtimeFailureStatus != nil {
		failStatus := *runtimeFailureStatus
		_ = ft.Router.RecordFailure(ctx, selected.Channel.ID, routing.SiteRuntimeFailureContext{
			Status:    &failStatus,
			ErrorText: &errorMessage,
			ModelName: &modelName,
		}, nil)
	} else {
		_ = ft.Router.RecordFailure(ctx, selected.Channel.ID, routing.SiteRuntimeFailureContext{
			ErrorText: &errorMessage,
			ModelName: &modelName,
		}, nil)
	}

	_ = ft.LogProxy(ctx, ProxyLogEntry{
		RouteID:          selected.Channel.RouteID,
		ChannelID:        &selected.Channel.ID,
		AccountID:        &selected.Account.ID,
		ModelRequested:   requestedModel,
		ModelActual:      &modelName,
		Status:           "failed",
		HTTPStatus:       httpStatus,
		LatencyMs:        latencyMs,
		PromptTokens:     &promptTokens,
		CompletionTokens: &completionTokens,
		TotalTokens:      &totalTokens,
		ErrorMessage:     &errorMessage,
		RetryCount:       retryCount,
		UpstreamPath:     &upstreamPath,
	})
}
