package proxyhandler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/handler/shared"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/service"
	generate_content "github.com/tokendancelab/metapi-go/transform/gemini/generate_content"
	"github.com/tokendancelab/metapi-go/transform/openai/responses"
)

// UpstreamConfig holds the dependencies needed for upstream forwarding.
type UpstreamConfig struct {
	Router         proxy.TokenRouterInterface
	RouteRefresher proxy.RouteRefreshWorkflow
	Coordinator    *proxy.ProxyChannelCoordinator
	Executor       *proxy.RuntimeExecutor
	// SiteLimiter caps concurrent dispatches per site (sites.max_concurrency).
	// When nil, proxy.DefaultSiteConcurrencyLimiter is used.
	// Orthogonal to Coordinator channel leases (FE-SITE-CONC / upstream #594).
	SiteLimiter *proxy.SiteConcurrencyLimiter
	// LogProxy persists successful/failed proxy attempts into proxy_logs.
	// When nil, defaultLogProxyWriter uses store.GetDB() (no-op if DB unset).
	LogProxy func(ctx context.Context, entry proxy.ProxyLogEntry) error
}

var upstreamCfg *UpstreamConfig
var unconfiguredUpstreamLogOnce sync.Once

const defaultMaxStreamResponseBytes int64 = 128 << 20

// defaultUpstreamClient is used as a safety fallback when the RuntimeExecutor
// has not been wired (e.g., during tests). It carries a 90s timeout so a hung
// upstream never leaks a goroutine. Production deployments should always wire
// the Executor field via SetUpstreamConfig.
var defaultUpstreamClient = &http.Client{
	Timeout: 90 * time.Second,
}

// SetUpstreamConfig sets the package-level upstream forwarding dependencies.
// Called during server startup to wire in the routing engine and HTTP executor.
func SetUpstreamConfig(cfg *UpstreamConfig) {
	upstreamCfg = cfg
}

// getUpstreamConfig returns the configured upstream dependencies.
func getUpstreamConfig() *UpstreamConfig {
	return upstreamCfg
}

// dispatchUpstream forwards a proxy request to the selected upstream channel.
// Implements the spec's 10-step Handler pattern.
func dispatchUpstream(w http.ResponseWriter, r *http.Request, ctx *Ctx) {
	shared.RecordProxyRequest()
	startedAt := time.Now()
	// Parent request/trace id is stable across channel retries and endpoint fallbacks.
	reqCtx, requestID := proxy.EnsureRequestID(r.Context(), "")
	if requestID != "" && r.Context() != reqCtx {
		r = r.WithContext(reqCtx)
	}
	cfg := getUpstreamConfig()
	if cfg == nil {
		if isProxyStubEnabled() {
			writeStubResponse(w, ctx)
			return
		}
		unconfiguredUpstreamLogOnce.Do(func() {
			slog.Error("proxy upstream dependencies are not configured", "request_id", requestID)
		})
		writeJSONErrorWithRequest(w, http.StatusServiceUnavailable, "Proxy upstream is not configured", "server_error", requestID)
		observeProxyTerminal(ctx, shared.OutcomeUnavailable, ctx != nil && ctx.IsStream, time.Since(startedAt))
		return
	}

	excludeChannelIDs := make([]int64, 0)
	maxRetries := ctx.MaxRetries
	downstreamPolicy := routingPolicyFromAuth(ctx.Policy)
	upstreamPath := ctx.DownstreamPath
	if upstreamPath == "" {
		upstreamPath = r.URL.Path
	}
	var pendingFailure *pendingUpstreamFailure

	for retry := 0; retry <= maxRetries; retry++ {
		// Step 6: Channel selection
		selected, err := proxy.SelectProxyChannelForAttempt(
			r.Context(),
			cfg.Router,
			cfg.Coordinator,
			cfg.RouteRefresher,
			proxy.ChannelSelectionInput{
				RequestedModel:    ctx.RequestedModel,
				DownstreamPolicy:  downstreamPolicy,
				ExcludeChannelIDs: excludeChannelIDs,
				RetryCount:        retry,
				ForcedChannelID:   ctx.ForcedChannelID,
			},
		)
		if err != nil || selected == nil {
			slog.Warn("channel selection failed",
				"err", err,
				"model", ctx.RequestedModel,
				"retry", retry,
				"request_id", requestID,
			)
			if pendingFailure != nil {
				pendingFailure.write(w, requestID)
				observeProxyTerminal(ctx, pendingFailure.outcomeStatus(), ctx != nil && ctx.IsStream, time.Since(startedAt))
				return
			}
			writeJSONErrorWithRequest(w, 503, "No available channels", "server_error", requestID)
			observeProxyTerminal(ctx, shared.OutcomeUnavailable, ctx != nil && ctx.IsStream, time.Since(startedAt))
			return
		}
		excludeChannelIDs = append(excludeChannelIDs, selected.Channel.ID)

		// Site-scoped concurrency (orthogonal to channel leases).
		// On saturate: skip to next channel/site — do NOT mark expired/cascade.
		// FE-SITE-CONC / upstream #594 / SC2 sites.max_concurrency.
		siteLimiter := cfg.SiteLimiter
		if siteLimiter == nil {
			siteLimiter = proxy.DefaultSiteConcurrencyLimiter
		}
		siteSlot, acquired := siteLimiter.TryAcquire(selected.Site.ID, selected.Site.MaxConcurrency)
		if !acquired {
			slog.Info("site concurrency saturated; skipping channel without failure cascade",
				"site_id", selected.Site.ID,
				"channel_id", selected.Channel.ID,
				"max_concurrency", selected.Site.MaxConcurrency,
				"model", ctx.RequestedModel,
				"retry", retry,
				"request_id", requestID,
			)
			continue
		}

		// Hold the site slot for the full attempt; always release (even on panic path).
		var finished bool
		var nextPending *pendingUpstreamFailure
		func() {
			defer siteSlot.Release()
			finished, nextPending = dispatchSelectedUpstream(w, r, ctx, cfg, selected, upstreamPath, retry, maxRetries, requestID)
		}()
		if finished {
			return
		}
		if nextPending != nil {
			pendingFailure = nextPending
		}
	}

	writeJSONErrorWithRequest(w, 503, "All channels exhausted", "server_error", requestID)
	observeProxyTerminal(ctx, shared.OutcomeUnavailable, ctx != nil && ctx.IsStream, time.Since(startedAt))
}

// observeProxyTerminal records labeled counters, latency histogram, and optional export hook.
// Labels are privacy-safe (endpoint family + outcome); never model/key/body.
func observeProxyTerminal(ctx *Ctx, status string, stream bool, latency time.Duration) {
	endpoint := shared.EndpointOther
	if ctx != nil && ctx.DownstreamPath != "" {
		endpoint = shared.EndpointLabelFromPath(ctx.DownstreamPath)
	}
	shared.ObserveProxyOutcome(shared.ProxyObservation{
		Endpoint: endpoint,
		Status:   status,
		Stream:   stream,
		Latency:  latency,
	})
}

// dispatchSelectedUpstream runs steps 7-9 for one selected channel.
// finished=true means the response was written (success or terminal error).
// finished=false means the caller should continue the retry loop (optionally
// with nextPending as the last soft failure to surface if selection ends).
//
// Chat-family surfaces iterate multi-protocol endpoint candidates with observed
// first-byte timeout (issue #38 / upstream #387). Failures that fall through to
// the next protocol candidate do not record channel failure so healthy siblings
// are not poisoned by a single protocol miss.
func dispatchSelectedUpstream(
	w http.ResponseWriter,
	r *http.Request,
	ctx *Ctx,
	cfg *UpstreamConfig,
	selected *routing.SelectedChannel,
	upstreamPath string,
	retry int,
	maxRetries int,
	requestID string,
) (finished bool, nextPending *pendingUpstreamFailure) {
	if requestID == "" {
		requestID = proxy.RequestIDFromContext(r.Context())
	}
	// Step 7: Build upstream request materials
	upstreamModel := selected.ActualModel
	if upstreamModel == "" {
		upstreamModel = ctx.RequestedModel
	}
	runtimeCfg := config.Get()
	// Proxy selection: key proxy > account > site > system > direct
	// (FE-KEY-PROXY / upstream #578; see proxy.KeyProxyPrecedence).
	proxyConfig := service.BuildPlatformProxyConfig(runtimeCfg, &selected.Account, &selected.Site)
	if ctx != nil && ctx.Auth != nil {
		proxyConfig = proxy.ApplyKeyProxyOverride(proxyConfig, ctx.Auth.ProxyURL)
	}
	firstByteTimeoutMs := int64(0)
	disableCrossProtocolFallback := false
	if runtimeCfg != nil {
		firstByteTimeoutMs = proxy.FirstByteTimeoutMs(runtimeCfg.ProxyFirstByteTimeoutSec)
		disableCrossProtocolFallback = runtimeCfg.DisableCrossProtocolFallback
	}

	contentType := "application/json"
	var bodyBytes []byte
	var err error
	if ctx.Multipart {
		// Multipart bodies are not multi-protocol rewritten; single-shot only.
		var bodyReader io.Reader
		bodyReader, contentType, err = CloneMultipartBody(r, map[string]string{"model": upstreamModel})
		if err != nil {
			slog.Warn("multipart upstream body construction failed",
				"err", err, "path", upstreamPath, "model", upstreamModel, "request_id", requestID, "retry", retry)
			writeJSONErrorWithRequest(w, 400, "Invalid multipart request body", "invalid_request_error", requestID)
			observeProxyTerminal(ctx, shared.OutcomeClientError, false, 0)
			return true, nil
		}
		if bodyReader != nil {
			bodyBytes, err = io.ReadAll(bodyReader)
			if err != nil {
				slog.Warn("multipart upstream body read failed",
					"err", err, "path", upstreamPath, "request_id", requestID, "retry", retry)
				writeJSONErrorWithRequest(w, 400, "Invalid multipart request body", "invalid_request_error", requestID)
				observeProxyTerminal(ctx, shared.OutcomeClientError, false, 0)
				return true, nil
			}
		}
		return dispatchEndpointAttempt(w, r, ctx, cfg, selected, upstreamModel, proxyConfig, upstreamPath, contentType, bodyBytes, firstByteTimeoutMs, retry, maxRetries, true, requestID)
	}
	bodyBytes = swapModelInJSON(ctx.RawBody, upstreamModel)

	// Site protocol preference (#56 / upstream #340): responses-only + stream.
	sitePref := proxy.DetectSiteProtocolPreferenceFromSite(
		selected.Site.Platform,
		selected.Site.URL,
		selected.Site.CustomHeaders,
	)
	if errMsg := responsesOnlyClientError(upstreamPath, bodyBytes, sitePref); errMsg != "" {
		// Clear failure for chat/messages clients when site cannot serve without transform.
		writeJSONErrorWithRequest(w, http.StatusBadRequest, errMsg, "invalid_request_error", requestID)
		observeProxyTerminal(ctx, shared.OutcomeClientError, false, 0)
		return true, nil
	}

	candidatePaths := resolveUpstreamCandidatePaths(upstreamPath, disableCrossProtocolFallback, sitePref)
	var lastPending *pendingUpstreamFailure
	for i, path := range candidatePaths {
		isLast := i >= len(candidatePaths)-1
		attemptBody, sanitizeErr := sanitizeUpstreamJSONBody(bodyBytes, selected.Site.Platform, path, upstreamModel)
		if sanitizeErr != nil {
			// Clear client-facing continuity error (issue #54 / upstream #504).
			writeJSONErrorWithRequest(w, http.StatusBadRequest, sanitizeErr.Error(), "invalid_request_error", requestID)
			observeProxyTerminal(ctx, shared.OutcomeClientError, false, 0)
			return true, nil
		}
		// Force stream=true for responses-only / stream-preferring sites (and codex/sub2api).
		attemptBody, forcedStream := applyUpstreamStreamPreference(attemptBody, selected.Site.Platform, path, sitePref)
		effectiveStream := ctx.IsStream || forcedStream
		// OpenAI chat stream: ensure final SSE usage chunk via stream_options.include_usage (#345 / P0-555 residual).
		attemptBody, expectStreamUsage := applyUpstreamStreamIncludeUsage(attemptBody, selected.Site.Platform, path, effectiveStream)
		finished, pending, cont := dispatchEndpointAttemptWithContinue(
			w, r, ctx, cfg, selected, upstreamModel, proxyConfig,
			path, contentType, attemptBody, firstByteTimeoutMs,
			retry, maxRetries, isLast, disableCrossProtocolFallback, effectiveStream, expectStreamUsage, requestID,
		)
		if finished {
			return true, nil
		}
		if pending != nil {
			lastPending = pending
		}
		if cont {
			// Soft protocol/timeout miss: try next candidate without channel poison.
			continue
		}
		// Channel-level retry or terminal pending for outer loop.
		return false, lastPending
	}
	if lastPending != nil {
		return false, lastPending
	}
	if retry < maxRetries {
		return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Upstream request failed", "upstream_error")
	}
	writeJSONErrorWithRequest(w, 502, "Upstream request failed", "upstream_error", requestID)
	observeProxyTerminal(ctx, shared.OutcomeUpstreamError, false, 0)
	return true, nil
}

// resolveUpstreamCandidatePaths returns ordered upstream paths for one channel attempt.
// Non chat-family paths yield the original path only.
// sitePref controls responses-only / prefer-responses ordering (#56).
func resolveUpstreamCandidatePaths(upstreamPath string, disableCrossProtocolFallback bool, sitePref proxy.SiteProtocolPreference) []string {
	candidates := proxy.ResolveEndpointCandidatesWithOptions(upstreamPath, proxy.EndpointCandidateOptions{
		DisableCrossProtocolFallback: disableCrossProtocolFallback,
		Preference:                   sitePref,
	})
	if len(candidates) == 0 {
		return []string{upstreamPath}
	}
	paths := make([]string, 0, len(candidates))
	for _, ep := range candidates {
		if p := proxy.PathForEndpoint(ep); p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return []string{upstreamPath}
	}
	return paths
}

// responsesOnlyClientError returns a clear client message when a chat/messages
// shaped request hits a responses-only site (no heavy protocol transform in this wave).
func responsesOnlyClientError(downstreamPath string, bodyBytes []byte, pref proxy.SiteProtocolPreference) string {
	if !pref.ResponsesOnly {
		return ""
	}
	ep, ok := proxy.EndpointFromPath(downstreamPath)
	if !ok || ep == proxy.EndpointResponses {
		return ""
	}
	// If body already looks responses-shaped (has input, no messages), allow path rewrite.
	if bodyLooksResponsesShaped(bodyBytes) {
		return ""
	}
	return proxy.ResponsesOnlyChatUnsupportedMessage(downstreamPath)
}

func bodyLooksResponsesShaped(bodyBytes []byte) bool {
	if len(bodyBytes) == 0 {
		return false
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return false
	}
	if _, hasMessages := body["messages"]; hasMessages {
		return false
	}
	if _, hasInput := body["input"]; hasInput {
		return true
	}
	return false
}

// applyUpstreamStreamPreference forces stream=true when site/platform requires it.
func applyUpstreamStreamPreference(bodyBytes []byte, sitePlatform, upstreamPath string, pref proxy.SiteProtocolPreference) ([]byte, bool) {
	pathLower := strings.ToLower(upstreamPath)
	isCompact := strings.Contains(pathLower, "/responses/compact")
	// Platform helper (codex/sub2api) OR site preference.
	force := responses.ShouldForceResponsesUpstreamStream(sitePlatform, isCompact) ||
		proxy.ShouldForceUpstreamStream(pref, upstreamPath, isCompact)
	if !force {
		return bodyBytes, false
	}
	if len(bodyBytes) == 0 {
		return []byte(`{"stream":true}`), true
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return bodyBytes, false
	}
	// Already streaming?
	if v, ok := body["stream"]; ok {
		if b, ok := v.(bool); ok && b {
			return bodyBytes, false
		}
		if s, ok := v.(string); ok && (s == "true" || s == "1") {
			return bodyBytes, false
		}
	}
	next := make(map[string]any, len(body)+1)
	for k, v := range body {
		next[k] = v
	}
	next["stream"] = true
	out, err := json.Marshal(next)
	if err != nil {
		return bodyBytes, false
	}
	return out, true
}

// applyUpstreamStreamIncludeUsage forces stream_options.include_usage=true on OpenAI-compatible
// chat/completions and legacy /v1/completions stream bodies so upstream SSE emits a final usage chunk (P0-555 residual / #345/#350).
// Platform-safe: skips non-chat endpoints and platforms known to reject stream_options (codex/sub2api).
// Does not invent tokens; only asks the provider to include usage when streaming.
//
// The bool is true when the outbound stream body is expected to carry usage via include_usage
// (we injected it, or the client already set include_usage=true on an accepting path). Callers
// use that flag to warn once if the stream ends without extracted usage (#400 / P0-555 residual).
func applyUpstreamStreamIncludeUsage(bodyBytes []byte, sitePlatform, upstreamPath string, isStream bool) ([]byte, bool) {
	if !isStream || len(bodyBytes) == 0 {
		return bodyBytes, false
	}
	if !acceptsOpenAIStreamIncludeUsagePath(upstreamPath) {
		return bodyBytes, false
	}
	if rejectsOpenAIStreamOptions(sitePlatform) {
		return bodyBytes, false
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return bodyBytes, false
	}
	// Only when this attempt is streaming (client or forced).
	if !jsonTruthyBool(body["stream"]) && !isStream {
		return bodyBytes, false
	}
	opts, _ := body["stream_options"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
	} else {
		// Copy so we do not mutate nested maps shared with original body reference.
		nextOpts := make(map[string]any, len(opts)+1)
		for k, v := range opts {
			nextOpts[k] = v
		}
		opts = nextOpts
	}
	if jsonTruthyBool(opts["include_usage"]) {
		// Already requested; leave other stream_options keys intact without rewrite.
		// Still expect a final usage chunk from upstream.
		return bodyBytes, true
	}
	opts["include_usage"] = true
	next := make(map[string]any, len(body)+1)
	for k, v := range body {
		next[k] = v
	}
	next["stream_options"] = opts
	out, err := json.Marshal(next)
	if err != nil {
		return bodyBytes, false
	}
	return out, true
}

// shouldWarnMissingStreamUsage reports whether a completed/partial stream that requested
// include_usage still lacks usable token counts. Never invents tokens — only detects absence.
func shouldWarnMissingStreamUsage(expectIncludeUsage bool, usage ParsedUsage) bool {
	if !expectIncludeUsage {
		return false
	}
	if !usage.Found {
		return true
	}
	// Found but all zero: provider emitted a usage object without counts (still residual).
	return usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 &&
		usage.CacheReadTokens == 0 && usage.CacheCreationTokens == 0 && usage.ReasoningTokens == 0
}

// warnMissingStreamUsageAfterIncludeUsage logs once per call site (one success/partial end path).
// model/path identify the request; tokens are never invented here.
func warnMissingStreamUsageAfterIncludeUsage(model, path string, usage ParsedUsage) {
	if !shouldWarnMissingStreamUsage(true, usage) {
		return
	}
	slog.Warn("stream ended without usage after include_usage",
		"model", model,
		"path", path,
		"usage_found", usage.Found,
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"total_tokens", usage.TotalTokens,
	)
}

// acceptsOpenAIStreamIncludeUsagePath reports OpenAI-compatible paths that honor
// stream_options.include_usage (chat + legacy completions). Messages/Responses excluded.
func acceptsOpenAIStreamIncludeUsagePath(upstreamPath string) bool {
	ep, ok := proxy.EndpointFromPath(upstreamPath)
	if ok && ep == proxy.EndpointChat {
		return true
	}
	path := strings.TrimSpace(upstreamPath)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimRight(path, "/")
	// Legacy OpenAI completions (not chat/completions — EndpointChat already matched).
	if path == "/v1/completions" || path == "/completions" || strings.HasSuffix(path, "/v1/completions") {
		return true
	}
	return false
}

// rejectsOpenAIStreamOptions reports platforms that historically 400 on stream_options
// (original metapi #446 Codex OAuth) or always strip it in Responses sanitize.
func rejectsOpenAIStreamOptions(sitePlatform string) bool {
	switch strings.ToLower(strings.TrimSpace(sitePlatform)) {
	case "codex", "chatgpt-codex", "chatgpt codex", "sub2api":
		return true
	default:
		return false
	}
}

// jsonTruthyBool accepts JSON bool or common string encodings of true.
func jsonTruthyBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// dispatchEndpointAttempt is the single-path entry used by multipart.
func dispatchEndpointAttempt(
	w http.ResponseWriter,
	r *http.Request,
	ctx *Ctx,
	cfg *UpstreamConfig,
	selected *routing.SelectedChannel,
	upstreamModel string,
	proxyConfig *platform.ProxyConfig,
	upstreamPath string,
	contentType string,
	bodyBytes []byte,
	firstByteTimeoutMs int64,
	retry int,
	maxRetries int,
	recordFailure bool,
	requestID string,
) (finished bool, nextPending *pendingUpstreamFailure) {
	finished, pending, _ := dispatchEndpointAttemptWithContinue(
		w, r, ctx, cfg, selected, upstreamModel, proxyConfig,
		upstreamPath, contentType, bodyBytes, firstByteTimeoutMs,
		retry, maxRetries, true, true, ctx != nil && ctx.IsStream, false, requestID,
	)
	if !recordFailure {
		return finished, pending
	}
	return finished, pending
}

// dispatchEndpointAttemptWithContinue runs one endpoint path.
// cont=true means the caller should try the next protocol candidate without
// recording channel failure / without writing a terminal response.
func dispatchEndpointAttemptWithContinue(
	w http.ResponseWriter,
	r *http.Request,
	ctx *Ctx,
	cfg *UpstreamConfig,
	selected *routing.SelectedChannel,
	upstreamModel string,
	proxyConfig *platform.ProxyConfig,
	upstreamPath string,
	contentType string,
	bodyBytes []byte,
	firstByteTimeoutMs int64,
	retry int,
	maxRetries int,
	isLastEndpoint bool,
	disableCrossProtocolFallback bool,
	effectiveStream bool,
	expectStreamUsage bool,
	requestID string,
) (finished bool, nextPending *pendingUpstreamFailure, cont bool) {
	if requestID == "" {
		requestID = proxy.RequestIDFromContext(r.Context())
	}
	upstreamURL := proxy.BuildUpstreamURL(selected.Site.URL, upstreamPath)
	startedAt := time.Now()

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytesReader(bodyBytes))
	if err != nil {
		slog.Warn("upstream request construction failed",
			"err", err, "url", upstreamURL, "model", upstreamModel,
			"request_id", requestID, "retry", retry)
		if !isLastEndpoint && !disableCrossProtocolFallback {
			return false, nil, true
		}
		if retry < maxRetries {
			return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Upstream request failed", "upstream_error"), false
		}
		writeJSONErrorWithRequest(w, 502, "Upstream request failed", "upstream_error", requestID)
		observeProxyTerminal(ctx, shared.OutcomeUpstreamError, effectiveStream, time.Since(startedAt))
		return true, nil, false
	}
	req.Header.Set("Content-Type", contentType)
	// Custom headers first (deny-list skips Authorization/Host/hop-by-hop), then
	// Bearer so site custom_headers can never override the selected token (#356).
	applyProxyCustomHeaders(req, proxyConfig)
	if selected.TokenValue != "" {
		req.Header.Set("Authorization", "Bearer "+selected.TokenValue)
	}

	resp, err := sendUpstreamRequest(cfg, req, proxyConfig, firstByteTimeoutMs)
	latencyMs := time.Since(startedAt).Milliseconds()

	if err != nil {
		// First-byte timeout: continue to next protocol when allowed; do not poison.
		if proxy.IsObservedFirstByteTimeoutError(err) {
			slog.Info("upstream first-byte timeout",
				"url", upstreamURL,
				"model", upstreamModel,
				"channel_id", selected.Channel.ID,
				"first_byte_timeout_ms", firstByteTimeoutMs,
				"is_last_endpoint", isLastEndpoint,
				"request_id", requestID,
				"retry", retry,
			)
			if !isLastEndpoint && !disableCrossProtocolFallback {
				return false, nil, true
			}
			// Terminal for this channel attempt.
			errText := err.Error()
			recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, 0, errText)
			writeFailureProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, http.StatusRequestTimeout, effectiveStream, ParsedUsage{Source: usageSourceUnknown}, retry, requestID, errText)
			if retry < maxRetries && proxy.ShouldRetryProxyRequest(408, errText) {
				return false, jsonPendingUpstreamFailure(http.StatusRequestTimeout, "Upstream first-byte timeout", "upstream_error"), false
			}
			writeJSONErrorWithRequest(w, http.StatusRequestTimeout, "Upstream first-byte timeout", "upstream_error", requestID)
			observeProxyTerminal(ctx, shared.OutcomeTimeout, effectiveStream, time.Since(startedAt))
			return true, nil, false
		}
		slog.Warn("upstream request failed",
			"err", err, "url", upstreamURL, "model", upstreamModel,
			"channel_id", selected.Channel.ID, "request_id", requestID, "retry", retry)
		if !isLastEndpoint && !disableCrossProtocolFallback {
			// Network error may still be protocol-local; allow next endpoint without poison.
			return false, nil, true
		}
		errText := err.Error()
		recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, 0, errText)
		writeFailureProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, http.StatusBadGateway, effectiveStream, ParsedUsage{Source: usageSourceUnknown}, retry, requestID, errText)
		if retry < maxRetries {
			return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Upstream request failed", "upstream_error"), false
		}
		writeJSONErrorWithRequest(w, 502, "Upstream request failed", "upstream_error", requestID)
		observeProxyTerminal(ctx, shared.OutcomeUpstreamError, effectiveStream, time.Since(startedAt))
		return true, nil, false
	}

	// Step 9: Handle response
	if effectiveStream {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, readErr := proxy.ReadBufferedResponseBody(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				slog.Warn("failed to read upstream stream error response",
					"err", readErr, "latency_ms", latencyMs, "status", resp.StatusCode,
					"request_id", requestID, "retry", retry)
				if !isLastEndpoint && !disableCrossProtocolFallback {
					return false, nil, true
				}
				errText := readErr.Error()
				recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, http.StatusBadGateway, errText)
				writeFailureProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, http.StatusBadGateway, true, ParsedUsage{Source: usageSourceUnknown}, retry, requestID, errText)
				if retry < maxRetries {
					return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Failed to read upstream response", "upstream_error"), false
				}
				writeJSONErrorWithRequest(w, 502, "Failed to read upstream response", "upstream_error", requestID)
				observeProxyTerminal(ctx, shared.OutcomeUpstreamError, true, time.Duration(latencyMs)*time.Millisecond)
				return true, nil, false
			}
			rawErrText := string(respBody)
			if shouldContinueEndpointFallback(resp.StatusCode, rawErrText, isLastEndpoint, disableCrossProtocolFallback) {
				return false, nil, true
			}
			// Best-effort usage from error JSON bodies (some gateways still include usage).
			failUsage := ParseUsageFromBody(respBody)
			recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, resp.StatusCode, rawErrText)
			writeFailureProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, resp.StatusCode, true, failUsage, retry, requestID, truncateErrText(rawErrText))
			if retry < maxRetries && proxy.ShouldRetryProxyRequest(resp.StatusCode, rawErrText) {
				return false, bufferedPendingUpstreamFailure(resp, respBody), false
			}
			relayBufferedUpstreamErrorResponse(w, resp, respBody)
			observeProxyTerminal(ctx, shared.StatusFromHTTP(resp.StatusCode), true, time.Duration(latencyMs)*time.Millisecond)
			return true, nil, false
		}
		// Always close the upstream body, including early client disconnects.
		var streamUsage ParsedUsage
		func() {
			defer resp.Body.Close()
			streamUsage = handleStreamUpstream(w, r, resp, latencyMs)
		}()
		// Observability only: include_usage was on the outbound body but SSE had no usable tokens (#400).
		// Still record zeros / unknown — never invent tokens.
		if expectStreamUsage {
			warnMissingStreamUsageAfterIncludeUsage(upstreamModel, upstreamPath, streamUsage)
		}
		recordUpstreamSuccess(r.Context(), cfg, selected, upstreamModel, latencyMs, streamUsage)
		writeSuccessProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, resp.StatusCode, true, streamUsage, retry, requestID)
		observeProxyTerminal(ctx, shared.OutcomeSuccess, true, time.Duration(latencyMs)*time.Millisecond)
		return true, nil, false
	}

	respBody, readErr := proxy.ReadBufferedResponseBody(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		slog.Warn("failed to read upstream response",
			"err", readErr, "latency_ms", latencyMs, "channel_id", selected.Channel.ID,
			"request_id", requestID, "retry", retry)
		if !isLastEndpoint && !disableCrossProtocolFallback {
			return false, nil, true
		}
		errText := readErr.Error()
		recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, http.StatusBadGateway, errText)
		writeFailureProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, http.StatusBadGateway, false, ParsedUsage{Source: usageSourceUnknown}, retry, requestID, errText)
		if retry < maxRetries {
			return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Failed to read upstream response", "upstream_error"), false
		}
		writeJSONErrorWithRequest(w, 502, "Failed to read upstream response", "upstream_error", requestID)
		observeProxyTerminal(ctx, shared.OutcomeUpstreamError, false, time.Duration(latencyMs)*time.Millisecond)
		return true, nil, false
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rawErrText := string(respBody)
		if shouldContinueEndpointFallback(resp.StatusCode, rawErrText, isLastEndpoint, disableCrossProtocolFallback) {
			return false, nil, true
		}
		// Non-stream HTTP errors: retain any usage object in the error body
		// (measurable under-count residual after #300 disconnect partial).
		failUsage := ParseUsageFromBody(respBody)
		recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, resp.StatusCode, rawErrText)
		writeFailureProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, resp.StatusCode, false, failUsage, retry, requestID, truncateErrText(rawErrText))
		if retry < maxRetries && proxy.ShouldRetryProxyRequest(resp.StatusCode, rawErrText) {
			return false, bufferedPendingUpstreamFailure(resp, respBody), false
		}
		relayBufferedUpstreamResponse(w, resp, respBody)
		observeProxyTerminal(ctx, shared.StatusFromHTTP(resp.StatusCode), false, time.Duration(latencyMs)*time.Millisecond)
		return true, nil, false
	}
	usage := ParseUsageFromBody(respBody)
	failure := proxy.DetectProxyFailure(string(respBody), usage.ToUsageSummary())
	if failure != nil {
		slog.Warn("content-based failure detected",
			"reason", failure.Reason,
			"status", failure.Status,
			"model", upstreamModel,
			"channel_id", selected.Channel.ID,
			"latency_ms", latencyMs,
			"request_id", requestID,
			"retry", retry,
		)
		if shouldContinueEndpointFallback(failure.Status, failure.Reason, isLastEndpoint, disableCrossProtocolFallback) {
			return false, nil, true
		}
		// Content failures often still carry real usage (keyword match / empty-
		// content edge cases with non-zero tokens). Persist failed row + tokens.
		recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, failure.Status, failure.Reason)
		writeFailureProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, failure.Status, false, usage, retry, requestID, failure.Reason)
		if retry < maxRetries && proxy.ShouldRetryProxyRequest(failure.Status, failure.Reason) {
			return false, jsonPendingUpstreamFailure(failure.Status, "Upstream returned an error response", "upstream_error"), false
		}
		writeJSONErrorWithRequest(w, failure.Status, "Upstream returned an error response", "upstream_error", requestID)
		observeProxyTerminal(ctx, shared.StatusFromHTTP(failure.Status), false, time.Duration(latencyMs)*time.Millisecond)
		return true, nil, false
	}
	recordUpstreamSuccess(r.Context(), cfg, selected, upstreamModel, latencyMs, usage)
	writeSuccessProxyLog(r.Context(), cfg, selected, ctx, upstreamModel, upstreamPath, latencyMs, resp.StatusCode, false, usage, retry, requestID)
	// Videos create (#235): map upstream id → publicId before the client sees the body.
	respBody = maybeRewriteVideosCreateResponse(ctx, selected, upstreamPath, respBody)
	relayBufferedUpstreamResponse(w, resp, respBody)
	observeProxyTerminal(ctx, shared.OutcomeSuccess, false, time.Duration(latencyMs)*time.Millisecond)
	return true, nil, false
}

func shouldContinueEndpointFallback(status int, rawErrText string, isLastEndpoint bool, disableCrossProtocolFallback bool) bool {
	if isLastEndpoint || disableCrossProtocolFallback {
		return false
	}
	if proxy.ShouldAbortSameSiteEndpointFallback(status, rawErrText) {
		return false
	}
	if proxy.ShouldDowngradeToNextEndpoint(status, rawErrText) {
		return true
	}
	// First-byte style status=0 should already be handled by error path; treat as continue.
	if status == 0 {
		return true
	}
	return false
}

type pendingUpstreamFailure struct {
	resp        *http.Response
	bodyBytes   []byte
	jsonStatus  int
	jsonMessage string
	jsonType    string
}

func bufferedPendingUpstreamFailure(resp *http.Response, bodyBytes []byte) *pendingUpstreamFailure {
	return &pendingUpstreamFailure{resp: resp, bodyBytes: bodyBytes}
}

func jsonPendingUpstreamFailure(status int, message, typ string) *pendingUpstreamFailure {
	return &pendingUpstreamFailure{
		jsonStatus:  status,
		jsonMessage: message,
		jsonType:    typ,
	}
}

func (p *pendingUpstreamFailure) outcomeStatus() string {
	if p == nil {
		return shared.OutcomeUnavailable
	}
	if p.resp != nil {
		return shared.StatusFromHTTP(p.resp.StatusCode)
	}
	return shared.StatusFromHTTP(p.jsonStatus)
}

func (p *pendingUpstreamFailure) write(w http.ResponseWriter, requestID string) {
	if p == nil {
		writeJSONErrorWithRequest(w, http.StatusServiceUnavailable, "No available channels", "server_error", requestID)
		return
	}
	if p.resp != nil {
		relayBufferedUpstreamResponse(w, p.resp, p.bodyBytes)
		return
	}
	status := p.jsonStatus
	if status == 0 {
		status = http.StatusBadGateway
	}
	message := p.jsonMessage
	if message == "" {
		message = "Upstream request failed"
	}
	typ := p.jsonType
	if typ == "" {
		typ = "upstream_error"
	}
	writeJSONErrorWithRequest(w, status, message, typ, requestID)
}

func applyProxyCustomHeaders(req *http.Request, proxyConfig *platform.ProxyConfig) {
	if proxyConfig == nil {
		return
	}
	// Shared deny-list: Authorization/Host/hop-by-hop/Cookie/Proxy-*/metapi control (#356).
	platform.ApplyCustomHeaders(req, proxyConfig.CustomHeaders)
}

// sendUpstreamRequest dispatches an upstream HTTP request with optional observed
// first-byte timeout. firstByteTimeoutMs is milliseconds (0 disables observation).
// Config PROXY_FIRST_BYTE_TIMEOUT_SEC is seconds; convert via proxy.FirstByteTimeoutMs.
func sendUpstreamRequest(cfg *UpstreamConfig, req *http.Request, proxyConfig *platform.ProxyConfig, firstByteTimeoutMs int64) (*http.Response, error) {
	// Executor path: DoWithObservedFirstByte owns the first-byte deadline and
	// does not cancel the body after headers arrive.
	if (proxyConfig == nil || (proxyConfig.ProxyURL == "" && !proxyConfig.InsecureSkipTLS)) && cfg != nil && cfg.Executor != nil {
		return cfg.Executor.DoWithObservedFirstByte(req.Context(), req, firstByteTimeoutMs)
	}

	if firstByteTimeoutMs <= 0 {
		if proxyConfig != nil && (proxyConfig.ProxyURL != "" || proxyConfig.InsecureSkipTLS) {
			return platform.DoWithProxy(req.Context(), req, proxyConfig)
		}
		return defaultUpstreamClient.Do(req)
	}

	// Proxy / fallback client: mirror DoWithObservedFirstByte timer semantics.
	parent := req.Context()
	reqCtx, cancelReq := context.WithCancel(parent)
	req = req.WithContext(reqCtx)
	var timedOut atomic.Bool
	timer := time.AfterFunc(time.Duration(firstByteTimeoutMs)*time.Millisecond, func() {
		timedOut.Store(true)
		cancelReq()
	})

	var (
		resp *http.Response
		err  error
	)
	if proxyConfig != nil && (proxyConfig.ProxyURL != "" || proxyConfig.InsecureSkipTLS) {
		resp, err = platform.DoWithProxy(reqCtx, req, proxyConfig)
	} else {
		resp, err = defaultUpstreamClient.Do(req)
	}
	if err != nil {
		_ = timer.Stop()
		cancelReq()
		if timedOut.Load() && parent.Err() == nil {
			return nil, proxy.ErrObservedFirstByteTimeout
		}
		return nil, err
	}
	_ = timer.Stop()
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancelReq}
	return resp, nil
}

// cancelOnCloseBody cancels the request context when the response body is closed.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	if b.cancel != nil {
		b.cancel()
	}
	return err
}

func recordUpstreamFailure(ctx context.Context, cfg *UpstreamConfig, selected *routing.SelectedChannel, modelName string, status int, rawErrText string) {
	if cfg == nil || cfg.Router == nil || selected == nil {
		return
	}
	failureCtx := routing.SiteRuntimeFailureContext{
		ErrorText: &rawErrText,
		ModelName: &modelName,
	}
	if status > 0 {
		failureCtx.Status = &status
	}
	if err := cfg.Router.RecordFailure(ctx, selected.Channel.ID, failureCtx, nil); err != nil {
		slog.Warn("RecordFailure failed", "err", err, "channel_id", selected.Channel.ID, "model", modelName)
	}
}

func recordUpstreamSuccess(ctx context.Context, cfg *UpstreamConfig, selected *routing.SelectedChannel, modelName string, latencyMs int64, usage ParsedUsage) {
	if cfg == nil || cfg.Router == nil || selected == nil {
		return
	}
	platformName := ""
	if selected.Site.Platform != "" {
		platformName = selected.Site.Platform
	}
	billing := EstimateBillingCostFromUsage(modelName, platformName, usage)
	if err := cfg.Router.RecordSuccess(ctx, selected.Channel.ID, float64(latencyMs), billing.EstimatedCost, &modelName, nil); err != nil {
		slog.Warn("RecordSuccess failed", "err", err, "channel_id", selected.Channel.ID, "model", modelName)
	}
	// Soft-feed first-byte EMA: until header timing is plumbed separately, use
	// total latency as an upper bound so faster channels still score better (#113).
	siteID := selected.Account.SiteID
	if siteID != 0 {
		routing.RecordSiteRuntimeSuccess(siteID, float64(latencyMs), &modelName, float64(latencyMs))
	}
}

func writeSuccessProxyLog(
	ctx context.Context,
	cfg *UpstreamConfig,
	selected *routing.SelectedChannel,
	proxyCtx *Ctx,
	upstreamModel string,
	upstreamPath string,
	latencyMs int64,
	httpStatus int,
	isStream bool,
	usage ParsedUsage,
	retryCount int,
	requestID string,
) {
	if cfg == nil || selected == nil {
		return
	}
	if requestID == "" {
		requestID = proxy.RequestIDFromContext(ctx)
	}
	requestedModel := ""
	var keyID *int64
	clientFamily, clientAppID, clientAppName, clientConfidence := "", "", "", ""
	if proxyCtx != nil {
		requestedModel = proxyCtx.RequestedModel
		if proxyCtx.Auth != nil {
			keyID = proxyCtx.Auth.KeyID
		}
		clientFamily = proxyCtx.ClientCtx.ClientKind
		clientAppID = proxyCtx.ClientCtx.ClientAppID
		clientAppName = proxyCtx.ClientCtx.ClientAppName
		clientConfidence = proxyCtx.ClientCtx.ClientConfidence
	}
	if requestedModel == "" {
		requestedModel = upstreamModel
	}
	modelActual := upstreamModel
	routeID := selected.Channel.RouteID
	var routeIDPtr *int64
	if routeID != 0 {
		routeIDPtr = &routeID
	}
	channelID := selected.Channel.ID
	accountID := selected.Account.ID
	source := usage.Source
	if source == "" {
		if usage.Found {
			source = usageSourceUpstream
		} else {
			source = usageSourceUnknown
		}
	}
	platformName := ""
	if selected.Site.Platform != "" {
		platformName = selected.Site.Platform
	}
	billing := EstimateBillingCostFromUsage(upstreamModel, platformName, usage)
	entry := proxy.ProxyLogEntry{
		RouteID:            routeIDPtr,
		ChannelID:          &channelID,
		AccountID:          &accountID,
		DownstreamAPIKeyID: keyID,
		ModelRequested:     requestedModel,
		ModelActual:        &modelActual,
		Status:             "success",
		HTTPStatus:         httpStatus,
		IsStream:           boolPtr(isStream),
		FirstByteLatencyMs: int64Ptr(latencyMs),
		LatencyMs:          latencyMs,
		PromptTokens:       int64Ptr(usage.PromptTokens),
		CompletionTokens:   int64Ptr(usage.CompletionTokens),
		TotalTokens:        int64Ptr(usage.TotalTokens),
		EstimatedCost:      billing.EstimatedCost,
		BillingDetails:     billing.BillingDetails,
		ClientFamily:       clientFamily,
		ClientAppID:        clientAppID,
		ClientAppName:      clientAppName,
		ClientConfidence:   clientConfidence,
		RetryCount:         retryCount,
		RequestID:          requestID,
		UpstreamPath:       &upstreamPath,
		UsageSource:        source,
	}
	logProxy(ctx, cfg, entry)
}

// writeFailureProxyLog persists a failed attempt into proxy_logs so stats /
// usage aggregation do not silently under-count tokens when upstream still
// reported usage on error or content-detected failure paths.
// Matches SurfaceFailureToolkit status="failed" semantics.
// Does not invent tokens: zeros + usage_source=unknown when usage.Found is false.
func writeFailureProxyLog(
	ctx context.Context,
	cfg *UpstreamConfig,
	selected *routing.SelectedChannel,
	proxyCtx *Ctx,
	upstreamModel string,
	upstreamPath string,
	latencyMs int64,
	httpStatus int,
	isStream bool,
	usage ParsedUsage,
	retryCount int,
	requestID string,
	errText string,
) {
	if cfg == nil || selected == nil {
		return
	}
	if requestID == "" {
		requestID = proxy.RequestIDFromContext(ctx)
	}
	requestedModel := ""
	var keyID *int64
	clientFamily, clientAppID, clientAppName, clientConfidence := "", "", "", ""
	if proxyCtx != nil {
		requestedModel = proxyCtx.RequestedModel
		if proxyCtx.Auth != nil {
			keyID = proxyCtx.Auth.KeyID
		}
		clientFamily = proxyCtx.ClientCtx.ClientKind
		clientAppID = proxyCtx.ClientCtx.ClientAppID
		clientAppName = proxyCtx.ClientCtx.ClientAppName
		clientConfidence = proxyCtx.ClientCtx.ClientConfidence
	}
	if requestedModel == "" {
		requestedModel = upstreamModel
	}
	modelActual := upstreamModel
	routeID := selected.Channel.RouteID
	var routeIDPtr *int64
	if routeID != 0 {
		routeIDPtr = &routeID
	}
	channelID := selected.Channel.ID
	accountID := selected.Account.ID
	source := usage.Source
	if source == "" {
		if usage.Found {
			source = usageSourceUpstream
		} else {
			source = usageSourceUnknown
		}
	}
	platformName := ""
	if selected.Site.Platform != "" {
		platformName = selected.Site.Platform
	}
	// Only attach cost when usage was found; avoid inventing spend on pure
	// network/timeout failures with zero tokens.
	var estimatedCost float64
	var billingDetails any
	if usage.Found {
		billing := EstimateBillingCostFromUsage(upstreamModel, platformName, usage)
		estimatedCost = billing.EstimatedCost
		billingDetails = billing.BillingDetails
	}
	errMsg := strings.TrimSpace(errText)
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	entry := proxy.ProxyLogEntry{
		RouteID:            routeIDPtr,
		ChannelID:          &channelID,
		AccountID:          &accountID,
		DownstreamAPIKeyID: keyID,
		ModelRequested:     requestedModel,
		ModelActual:        &modelActual,
		Status:             "failed",
		HTTPStatus:         httpStatus,
		IsStream:           boolPtr(isStream),
		FirstByteLatencyMs: int64Ptr(latencyMs),
		LatencyMs:          latencyMs,
		PromptTokens:       int64Ptr(usage.PromptTokens),
		CompletionTokens:   int64Ptr(usage.CompletionTokens),
		TotalTokens:        int64Ptr(usage.TotalTokens),
		EstimatedCost:      estimatedCost,
		BillingDetails:     billingDetails,
		ClientFamily:       clientFamily,
		ClientAppID:        clientAppID,
		ClientAppName:      clientAppName,
		ClientConfidence:   clientConfidence,
		ErrorMessage:       errPtr,
		RetryCount:         retryCount,
		RequestID:          requestID,
		UpstreamPath:       &upstreamPath,
		UsageSource:        source,
	}
	logProxy(ctx, cfg, entry)
}

// truncateErrText bounds proxy_logs.error_message size for large upstream bodies.
func truncateErrText(s string) string {
	const maxErrRunes = 2000
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= maxErrRunes {
		return s
	}
	return string(r[:maxErrRunes]) + "..."
}

func isProxyStubEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("METAPI_ENABLE_PROXY_STUB")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// writeStubResponse returns a local stub response only when explicitly enabled
// for tests or demos. Production defaults to 503 if upstream forwarding is not wired.
func writeStubResponse(w http.ResponseWriter, ctx *Ctx) {
	if ctx.IsStream {
		writeSSEHeaders(w)
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)

		w.Write(sseEvent(`{"id":"stub-metapi-go","object":"chat.completion.chunk","created":` + itoa(time.Now().Unix()) + `,"model":"` + jsonSafeString(ctx.RequestedModel) + `","choices":[{"index":0,"delta":{"content":"Hello from MetAPI Go (stub)"},"finish_reason":null}]}`))
		if flusher != nil {
			flusher.Flush()
		}
		w.Write(sseEvent(`{"id":"stub-metapi-go","object":"chat.completion.chunk","created":` + itoa(time.Now().Unix()) + `,"model":"` + jsonSafeString(ctx.RequestedModel) + `","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`))
		if flusher != nil {
			flusher.Flush()
		}
		w.Write(sseDone())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	stubResp := map[string]any{
		"id":      "stub-metapi-go",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   ctx.RequestedModel,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "Hello from MetAPI Go (stub)"},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
	writeJSON(w, 200, stubResp)
}

// handleStreamUpstream relays an SSE stream from upstream to the downstream client.
// It performs raw byte passthrough for minimal latency while incrementally
// analyzing bounded SSE event state for error/empty-content detection and
// end-of-stream usage extraction (OpenAI/Anthropic/Gemini/Responses shapes).
//
// Disables the server-level WriteTimeout via http.ResponseController so long-running
// LLM streams (>60s) are not torn down mid-response.
//
// Returns best-effort ParsedUsage from SSE events (may be zero/unknown).
func handleStreamUpstream(w http.ResponseWriter, r *http.Request, resp *http.Response, latencyMs int64) ParsedUsage {
	empty := ParsedUsage{Source: usageSourceUnknown}
	if resp == nil || resp.Body == nil {
		return empty
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		relayUpstreamErrorResponse(w, resp, latencyMs)
		return empty
	}

	writeSSEHeaders(w)
	w.WriteHeader(200)

	// Disable server-level WriteTimeout for SSE streaming.
	// Without this, any stream exceeding app.Server.WriteTimeout (60s) gets
	// forcibly closed — a hard break for reasoning models and long completions.
	if rc, ok := w.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = rc.SetWriteDeadline(time.Time{})
	}

	flusher, _ := w.(http.Flusher)

	// Copy upstream Content-Type if SSE; writeSSEHeaders already sets it.
	if ct := resp.Header.Get("Content-Type"); ct != "" && strings.Contains(ct, "text/event-stream") {
		_ = ct // SSE content type already handled by writeSSEHeaders
	}

	analyzer := newIncrementalSseAnalyzer()
	sawStreamBytes := false
	maxStreamBytes := maxStreamResponseBytes()
	var streamedBytes int64
	buf := make([]byte, 4096)
	for {
		select {
		case <-r.Context().Done():
			// Client disconnect / request cancel: still return any usage already
			// extracted from earlier SSE events (best-effort partial). Do not
			// invent tokens when upstream never emitted a usage event.
			slog.Info("SSE downstream context ended",
				"err", r.Context().Err(),
				"latency_ms", latencyMs,
				"streamed_bytes", streamedBytes,
			)
			if result := analyzer.Result(); result.Usage.Found {
				return result.Usage
			}
			return empty
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			remaining := maxStreamBytes - streamedBytes
			if remaining <= 0 {
				writeSSEStreamError(w, flusher, "stream response exceeded configured byte limit", "upstream_error")
				slog.Warn("SSE stream exceeded byte limit",
					"latency_ms", latencyMs,
					"streamed_bytes", streamedBytes,
					"limit_bytes", maxStreamBytes,
				)
				break
			}
			exceededLimit := int64(len(chunk)) > remaining
			if exceededLimit {
				chunk = chunk[:int(remaining)]
			}

			sawStreamBytes = true
			if len(chunk) > 0 {
				// Extract usage before downstream write so client disconnect
				// on the final usage-bearing chunk still counts tokens.
				analyzer.Push(chunk)
				if _, writeErr := w.Write(chunk); writeErr != nil {
					slog.Warn("SSE downstream write failed",
						"err", writeErr,
						"latency_ms", latencyMs,
						"streamed_bytes", streamedBytes,
					)
					// Downstream gone: keep any usage already extracted (including
					// the chunk that failed to write). Never invent tokens.
					if result := analyzer.Result(); result.Usage.Found {
						return result.Usage
					}
					return empty
				}
				streamedBytes += int64(len(chunk))
			}
			if flusher != nil {
				flusher.Flush()
			}
			if exceededLimit {
				writeSSEStreamError(w, flusher, "stream response exceeded configured byte limit", "upstream_error")
				slog.Warn("SSE stream exceeded byte limit",
					"latency_ms", latencyMs,
					"streamed_bytes", streamedBytes,
					"limit_bytes", maxStreamBytes,
				)
				break
			}
		}
		if err != nil {
			if err != io.EOF {
				slog.Warn("SSE stream read error", "err", err, "latency_ms", latencyMs)
			}
			break
		}
	}

	// Post-stream SSE analysis uses bounded incremental state instead of
	// retaining the complete upstream body.
	result := analyzer.Result()
	if sawStreamBytes {
		if result.DroppedOversizedEvent {
			slog.Warn("SSE stream event exceeded analysis buffer",
				"latency_ms", latencyMs,
				"pending_limit_bytes", maxIncrementalSsePendingBytes,
			)
		}

		// Log SSE error events at WARN level
		if result.HasErrorEvent {
			LogSseErrorEvents(result.ErrorEvents)
		}

		// Check for empty content (stream ended with no data events)
		if !result.HasDataEvent {
			emptyContentFail := os.Getenv("PROXY_EMPTY_CONTENT_FAIL")
			if strings.ToLower(emptyContentFail) == "true" || emptyContentFail == "1" {
				slog.Warn("SSE stream contained no data events",
					"latency_ms", latencyMs,
					"event_count", result.EventCount,
					"has_done_marker", result.HasDoneMarker,
				)
			}
		}
		if result.Usage.Found {
			return result.Usage
		}
	}
	return empty
}

func maxStreamResponseBytes() int64 {
	raw := strings.TrimSpace(os.Getenv("PROXY_MAX_STREAM_RESPONSE_BYTES"))
	if raw == "" {
		return defaultMaxStreamResponseBytes
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return defaultMaxStreamResponseBytes
	}
	return n
}

func writeSSEStreamError(w http.ResponseWriter, flusher http.Flusher, message, typ string) {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{
			"message": message,
			"type":    typ,
		},
	})
	_, _ = w.Write(sseEvent(string(payload)))
	_, _ = w.Write(sseDone())
	if flusher != nil {
		flusher.Flush()
	}
}

func relayUpstreamErrorResponse(w http.ResponseWriter, resp *http.Response, latencyMs int64) {
	bodyBytes, err := proxy.ReadBufferedResponseBody(resp.Body)
	if err != nil {
		slog.Warn("failed to read upstream error response", "err", err, "latency_ms", latencyMs, "status", resp.StatusCode)
		writeJSONError(w, 502, "Failed to read upstream response", "upstream_error")
		return
	}

	for k, v := range resp.Header {
		if k == "Content-Length" || k == "Transfer-Encoding" {
			continue
		}
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(bodyBytes)
}

func relayBufferedUpstreamErrorResponse(w http.ResponseWriter, resp *http.Response, bodyBytes []byte) {
	relayBufferedUpstreamResponse(w, resp, bodyBytes)
}

func relayBufferedUpstreamResponse(w http.ResponseWriter, resp *http.Response, bodyBytes []byte) {
	for k, v := range resp.Header {
		if k == "Content-Length" || k == "Transfer-Encoding" {
			continue
		}
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(bodyBytes)
}

// handleNonStreamUpstream writes a non-streaming upstream response to the downstream.
func handleNonStreamUpstream(w http.ResponseWriter, resp *http.Response, latencyMs int64, requestedModel, upstreamModel string, channelID int64) {
	bodyBytes, err := proxy.ReadBufferedResponseBody(resp.Body)
	if err != nil {
		slog.Warn("failed to read upstream response", "err", err)
		writeJSONError(w, 502, "Failed to read upstream response", "upstream_error")
		return
	}

	// Detect proxy failure
	usage := ParseUsageFromBody(bodyBytes)
	failure := proxy.DetectProxyFailure(string(bodyBytes), usage.ToUsageSummary())
	if failure != nil {
		slog.Warn("content-based failure detected",
			"reason", failure.Reason,
			"status", failure.Status,
			"model", upstreamModel,
			"channel_id", channelID,
			"latency_ms", latencyMs,
		)
		writeJSONError(w, failure.Status, "Upstream returned an error response", "upstream_error")
		return
	}

	// Relay upstream response headers and body
	for k, v := range resp.Header {
		if k == "Content-Length" || k == "Transfer-Encoding" {
			continue
		}
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(bodyBytes)
}

// sanitizeUpstreamJSONBody applies Responses continuity/compact/reasoning-input
// sanitization and official Gemini tool-history thoughtSignature inject for one
// upstream attempt. Chat/messages candidates strip previous_response_id;
// Responses platforms forward or strip per SupportsResponsesPreviousResponseID.
// Multi-turn reasoning items get required content preserved/injected
// (#50 / upstream #538). Native Gemini generateContent bodies (and gemini-cli
// request envelopes) get NormalizeRequest / OpenAI→Gemini rebuild so functionCall
// parts carry thoughtSignature (#309 / upstream #580/#581).
// See docs/analysis/previous-response-id.md,
// docs/analysis/responses-multi-turn-reasoning.md, and
// docs/analysis/gemini-thought-signature.md.
func sanitizeUpstreamJSONBody(bodyBytes []byte, sitePlatform, upstreamPath, upstreamModel string) ([]byte, error) {
	if len(bodyBytes) == 0 {
		return bodyBytes, nil
	}
	// Cheap gate: only rewrite when continuity, compact, multi-turn reasoning
	// input, or Gemini tool-history signature inject is involved. Avoid full
	// JSON parse on hot path otherwise.
	pathLower := strings.ToLower(upstreamPath)
	needsCompact := strings.Contains(pathLower, "/responses/compact")
	isResponsesPath := needsCompact || strings.Contains(pathLower, "/responses")
	needsContinuity := bytes.Contains(bodyBytes, []byte(`"previous_response_id"`))
	// Multi-turn Hermes/Codex (#50 / #538 / #310):
	// 1) Exact compact markers (common client encoding).
	// 2) Responses path + "input" always parse - covers pretty-printed /
	//    spaced JSON where "type" : "reasoning" does not match contiguous bytes.
	// 3) encrypted_content anywhere (continuity payload even without type key).
	needsReasoningInput := bytes.Contains(bodyBytes, []byte(`"type":"reasoning"`)) ||
		bytes.Contains(bodyBytes, []byte(`"type": "reasoning"`)) ||
		bytes.Contains(bodyBytes, []byte(`"encrypted_content"`)) ||
		(isResponsesPath && bytes.Contains(bodyBytes, []byte(`"input"`)))
	needsGeminiThought := needsGeminiThoughtSignatureSanitize(sitePlatform, upstreamPath, bodyBytes)
	if !needsCompact && !needsContinuity && !needsReasoningInput && !needsGeminiThought {
		return bodyBytes, nil
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return bodyBytes, nil
	}
	next := body
	if needsCompact || needsContinuity || needsReasoningInput {
		protocol := responses.ProtocolUnknown
		switch ep, ok := proxy.EndpointFromPath(upstreamPath); {
		case ok && ep == proxy.EndpointChat:
			protocol = responses.ProtocolChat
		case ok && ep == proxy.EndpointMessages:
			protocol = responses.ProtocolMessages
		case ok && ep == proxy.EndpointResponses:
			protocol = responses.ProtocolResponses
		case needsCompact || strings.Contains(pathLower, "/responses"):
			protocol = responses.ProtocolResponses
		}
		sanitized, _, err := responses.SanitizeResponsesRequestBody(next, responses.ContinuityPolicyInput{
			SitePlatform:     sitePlatform,
			Protocol:         protocol,
			UpstreamPath:     upstreamPath,
			IsCompactRequest: needsCompact,
		})
		if err != nil {
			return nil, err
		}
		next = sanitized
	}
	if needsGeminiThought {
		next = applyGeminiThoughtSignatureSanitize(next, upstreamPath, upstreamModel)
	}
	out, marshalErr := json.Marshal(next)
	if marshalErr != nil {
		return bodyBytes, nil
	}
	return out, nil
}

// needsGeminiThoughtSignatureSanitize is a cheap byte/path gate for official
// Gemini native generateContent / gemini-cli tool-history inject. Avoids parse
// when the body clearly has no functionCall / tool_calls tool history.
func needsGeminiThoughtSignatureSanitize(sitePlatform, upstreamPath string, bodyBytes []byte) bool {
	if !isGeminiThoughtSignaturePlatform(sitePlatform) {
		return false
	}
	if !isGeminiNativeGenerateContentPath(upstreamPath) {
		return false
	}
	// Tool-history markers only (functionCall native or OpenAI tool_calls).
	return bytes.Contains(bodyBytes, []byte(`"functionCall"`)) ||
		bytes.Contains(bodyBytes, []byte(`"function_call"`)) ||
		bytes.Contains(bodyBytes, []byte(`"tool_calls"`))
}

func isGeminiThoughtSignaturePlatform(sitePlatform string) bool {
	switch strings.ToLower(strings.TrimSpace(sitePlatform)) {
	case "gemini", "gemini-cli", "google":
		return true
	default:
		return false
	}
}

// isGeminiNativeGenerateContentPath reports paths that carry Gemini contents
// (official generateContent / streamGenerateContent / gemini-cli v1internal).
// OpenAI-compat chat/completions paths are intentionally excluded — they keep
// OpenAI shape and do not accept native thoughtSignature inject.
func isGeminiNativeGenerateContentPath(upstreamPath string) bool {
	pathLower := strings.ToLower(strings.TrimSpace(upstreamPath))
	if pathLower == "" {
		return false
	}
	if strings.Contains(pathLower, "generatecontent") ||
		strings.Contains(pathLower, "streamgeneratecontent") {
		return true
	}
	// Gemini CLI internal: /v1internal::generateContent (double-colon in routes)
	// and /v1internal:generateContent (single-colon detect form).
	if strings.Contains(pathLower, "/v1internal") {
		return true
	}
	return false
}

// applyGeminiThoughtSignatureSanitize rewrites a Gemini-shaped (or CLI-wrapped)
// request so tool-history functionCall parts carry thoughtSignature.
// Prefer real signatures already present; inject dummy for Gemini 3.x /
// thinking-enabled models via generate_content.NormalizeRequest.
// When the client posts OpenAI messages onto a native generateContent path,
// rebuild via BuildGeminiGenerateContentRequestFromOpenAi.
// Residual: process-local aggregate ThoughtSignatures are not re-attached here
// (no multi-instance session store); clients must echo provider_specific_fields
// or send native contents that NormalizeRequest can patch.
func applyGeminiThoughtSignatureSanitize(body map[string]any, upstreamPath, upstreamModel string) map[string]any {
	if body == nil {
		return body
	}
	// gemini-cli envelope: { "model", "request": { contents... } }
	if req, ok := body["request"].(map[string]any); ok && req != nil {
		modelName := resolveGeminiSanitizeModel(body, req, upstreamPath, upstreamModel)
		inner := applyGeminiThoughtSignatureSanitizeBody(req, modelName)
		if inner != nil {
			// Clone top-level map so we do not mutate the unmarshaled input in place
			// when callers share maps (tests / retries).
			out := cloneJSONMapShallow(body)
			out["request"] = inner
			if sharedModel := strings.TrimSpace(asJSONString(out["model"])); sharedModel == "" && modelName != "" {
				out["model"] = modelName
			}
			return out
		}
		return body
	}
	modelName := resolveGeminiSanitizeModel(body, nil, upstreamPath, upstreamModel)
	return applyGeminiThoughtSignatureSanitizeBody(body, modelName)
}

func applyGeminiThoughtSignatureSanitizeBody(body map[string]any, modelName string) map[string]any {
	if body == nil {
		return body
	}
	// Native Gemini: contents present → NormalizeRequest (inject/preserve).
	if _, hasContents := body["contents"]; hasContents {
		return generate_content.NormalizeRequest(body, modelName)
	}
	// OpenAI chat body posted on a native generateContent path (rare bridge).
	if _, hasMessages := body["messages"]; hasMessages {
		return generate_content.BuildGeminiGenerateContentRequestFromOpenAi(body, modelName)
	}
	return body
}

func resolveGeminiSanitizeModel(body, nested map[string]any, upstreamPath, upstreamModel string) string {
	if m := strings.TrimSpace(upstreamModel); m != "" {
		return m
	}
	if nested != nil {
		if m := asJSONString(nested["model"]); m != "" {
			return m
		}
	}
	if body != nil {
		if m := asJSONString(body["model"]); m != "" {
			return m
		}
	}
	_, model, _ := ParseGeminiPath(upstreamPath)
	return strings.TrimSpace(model)
}

func asJSONString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func cloneJSONMapShallow(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// swapModelInJSON performs a shallow JSON re-encode to replace the "model" field.
// It uses json.RawMessage to avoid deep-unmarshalling nested values, then sets
// the model key and marshals once. This replaces the previous approach of:
//
//	cloneAndSetModel(ctx.Body) -> json.Marshal
//
// which allocated a full map copy PLUS serialized the deep structure twice.
func swapModelInJSON(bodyBytes []byte, upstreamModel string) []byte {
	if len(bodyBytes) == 0 {
		// Empty body: synthesize a minimal JSON object with only the model field.
		modelJSON, _ := json.Marshal(upstreamModel)
		return append(append([]byte(`{"model":`), modelJSON...), '}')
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		// Body is already validated in PrepareCtx; fallback to original bytes.
		return bodyBytes
	}
	modelJSON, _ := json.Marshal(upstreamModel)
	raw["model"] = json.RawMessage(modelJSON)
	result, err := json.Marshal(raw)
	if err != nil {
		return bodyBytes
	}
	return result
}

func bytesReader(b []byte) io.Reader {
	if len(b) == 0 {
		return nil
	}
	return bytes.NewReader(b)
}

func routingPolicyFromAuth(policy auth.DownstreamRoutingPolicy) routing.DownstreamRoutingPolicy {
	refs := make([]routing.CredentialRef, 0, len(policy.ExcludedCredentialRefs))
	for _, ref := range policy.ExcludedCredentialRefs {
		tokenID := int64(0)
		if ref.TokenID != nil {
			tokenID = *ref.TokenID
		}
		refs = append(refs, routing.CredentialRef{
			Kind:      string(ref.Kind),
			SiteID:    ref.SiteID,
			AccountID: ref.AccountID,
			TokenID:   tokenID,
		})
	}

	multipliers := policy.SiteWeightMultipliers
	if multipliers == nil {
		multipliers = map[int64]float64{}
	}

	return routing.DownstreamRoutingPolicy{
		SupportedModels:        policy.SupportedModels,
		AllowedRouteIDs:        policy.AllowedRouteIDs,
		SiteWeightMultipliers:  multipliers,
		ExcludedSiteIDs:        policy.ExcludedSiteIDs,
		ExcludedCredentialRefs: refs,
		DenyAllWhenEmpty:       policy.DenyAllWhenEmpty,
	}
}
