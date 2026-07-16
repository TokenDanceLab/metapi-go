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
	"time"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/service"
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
	cfg := getUpstreamConfig()
	if cfg == nil {
		if isProxyStubEnabled() {
			writeStubResponse(w, ctx)
			return
		}
		unconfiguredUpstreamLogOnce.Do(func() {
			slog.Error("proxy upstream dependencies are not configured")
		})
		writeJSONError(w, http.StatusServiceUnavailable, "Proxy upstream is not configured", "server_error")
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
			},
		)
		if err != nil || selected == nil {
			slog.Warn("channel selection failed", "err", err, "model", ctx.RequestedModel, "retry", retry)
			if pendingFailure != nil {
				pendingFailure.write(w)
				return
			}
			writeJSONError(w, 503, "No available channels", "server_error")
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
			)
			continue
		}

		// Hold the site slot for the full attempt; always release (even on panic path).
		var finished bool
		var nextPending *pendingUpstreamFailure
		func() {
			defer siteSlot.Release()
			finished, nextPending = dispatchSelectedUpstream(w, r, ctx, cfg, selected, upstreamPath, retry, maxRetries)
		}()
		if finished {
			return
		}
		if nextPending != nil {
			pendingFailure = nextPending
		}
	}

	writeJSONError(w, 503, "All channels exhausted", "server_error")
}

// dispatchSelectedUpstream runs steps 7-9 for one selected channel.
// finished=true means the response was written (success or terminal error).
// finished=false means the caller should continue the retry loop (optionally
// with nextPending as the last soft failure to surface if selection ends).
func dispatchSelectedUpstream(
	w http.ResponseWriter,
	r *http.Request,
	ctx *Ctx,
	cfg *UpstreamConfig,
	selected *routing.SelectedChannel,
	upstreamPath string,
	retry int,
	maxRetries int,
) (finished bool, nextPending *pendingUpstreamFailure) {
	// Step 7: Build upstream request
	upstreamModel := selected.ActualModel
	if upstreamModel == "" {
		upstreamModel = ctx.RequestedModel
	}
	upstreamURL := proxy.BuildUpstreamURL(selected.Site.URL, upstreamPath)
	proxyConfig := service.BuildPlatformProxyConfig(config.Get(), &selected.Account, &selected.Site)

	// Step 8: Send upstream request
	startedAt := time.Now()
	contentType := "application/json"
	var bodyReader io.Reader
	var err error
	if ctx.Multipart {
		bodyReader, contentType, err = CloneMultipartBody(r, map[string]string{"model": upstreamModel})
		if err != nil {
			slog.Warn("multipart upstream body construction failed", "err", err, "url", upstreamURL, "model", upstreamModel)
			writeJSONError(w, 400, "Invalid multipart request body", "invalid_request_error")
			return true, nil
		}
	} else {
		forwardBytes := swapModelInJSON(ctx.RawBody, upstreamModel)
		bodyReader = bytesReader(forwardBytes)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bodyReader)
	if err != nil {
		slog.Warn("upstream request construction failed", "err", err, "url", upstreamURL, "model", upstreamModel)
		if retry < maxRetries {
			return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Upstream request failed", "upstream_error")
		}
		writeJSONError(w, 502, "Upstream request failed", "upstream_error")
		return true, nil
	}
	req.Header.Set("Content-Type", contentType)
	if selected.TokenValue != "" {
		req.Header.Set("Authorization", "Bearer "+selected.TokenValue)
	}
	applyProxyCustomHeaders(req, proxyConfig)

	var resp *http.Response
	resp, err = sendUpstreamRequest(cfg, req, proxyConfig)
	latencyMs := time.Since(startedAt).Milliseconds()

	if err != nil {
		slog.Warn("upstream request failed", "err", err, "url", upstreamURL, "model", upstreamModel, "channel_id", selected.Channel.ID)
		recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, 0, err.Error())
		if retry < maxRetries {
			return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Upstream request failed", "upstream_error")
		}
		writeJSONError(w, 502, "Upstream request failed", "upstream_error")
		return true, nil
	}

	// Step 9: Handle response
	if ctx.IsStream {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodyBytes, readErr := proxy.ReadBufferedResponseBody(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				slog.Warn("failed to read upstream stream error response", "err", readErr, "latency_ms", latencyMs, "status", resp.StatusCode)
				recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, http.StatusBadGateway, readErr.Error())
				if retry < maxRetries {
					return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Failed to read upstream response", "upstream_error")
				}
				writeJSONError(w, 502, "Failed to read upstream response", "upstream_error")
				return true, nil
			}
			rawErrText := string(bodyBytes)
			recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, resp.StatusCode, rawErrText)
			if retry < maxRetries && proxy.ShouldRetryProxyRequest(resp.StatusCode, rawErrText) {
				return false, bufferedPendingUpstreamFailure(resp, bodyBytes)
			}
			relayBufferedUpstreamErrorResponse(w, resp, bodyBytes)
			return true, nil
		}
		// Always close the upstream body, including early client disconnects.
		func() {
			defer resp.Body.Close()
			handleStreamUpstream(w, r, resp, latencyMs)
		}()
		recordUpstreamSuccess(r.Context(), cfg, selected, upstreamModel, latencyMs)
	} else {
		bodyBytes, readErr := proxy.ReadBufferedResponseBody(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			slog.Warn("failed to read upstream response", "err", readErr, "latency_ms", latencyMs, "channel_id", selected.Channel.ID)
			recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, http.StatusBadGateway, readErr.Error())
			if retry < maxRetries {
				return false, jsonPendingUpstreamFailure(http.StatusBadGateway, "Failed to read upstream response", "upstream_error")
			}
			writeJSONError(w, 502, "Failed to read upstream response", "upstream_error")
			return true, nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			rawErrText := string(bodyBytes)
			recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, resp.StatusCode, rawErrText)
			if retry < maxRetries && proxy.ShouldRetryProxyRequest(resp.StatusCode, rawErrText) {
				return false, bufferedPendingUpstreamFailure(resp, bodyBytes)
			}
			relayBufferedUpstreamResponse(w, resp, bodyBytes)
			return true, nil
		}
		failure := proxy.DetectProxyFailure(string(bodyBytes), &proxy.UsageSummary{})
		if failure != nil {
			slog.Warn("content-based failure detected",
				"reason", failure.Reason,
				"status", failure.Status,
				"model", upstreamModel,
				"channel_id", selected.Channel.ID,
				"latency_ms", latencyMs,
			)
			recordUpstreamFailure(r.Context(), cfg, selected, upstreamModel, failure.Status, failure.Reason)
			if retry < maxRetries && proxy.ShouldRetryProxyRequest(failure.Status, failure.Reason) {
				return false, jsonPendingUpstreamFailure(failure.Status, "Upstream returned an error response", "upstream_error")
			}
			writeJSONError(w, failure.Status, "Upstream returned an error response", "upstream_error")
			return true, nil
		}
		recordUpstreamSuccess(r.Context(), cfg, selected, upstreamModel, latencyMs)
		relayBufferedUpstreamResponse(w, resp, bodyBytes)
	}
	return true, nil
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

func (p *pendingUpstreamFailure) write(w http.ResponseWriter) {
	if p == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "No available channels", "server_error")
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
	writeJSONError(w, status, message, typ)
}

func applyProxyCustomHeaders(req *http.Request, proxyConfig *platform.ProxyConfig) {
	if proxyConfig == nil {
		return
	}
	for k, v := range proxyConfig.CustomHeaders {
		req.Header.Set(k, v)
	}
}

func sendUpstreamRequest(cfg *UpstreamConfig, req *http.Request, proxyConfig *platform.ProxyConfig) (*http.Response, error) {
	if proxyConfig != nil && (proxyConfig.ProxyURL != "" || proxyConfig.InsecureSkipTLS) {
		return platform.DoWithProxy(req.Context(), req, proxyConfig)
	}
	if cfg.Executor != nil {
		return cfg.Executor.Do(req)
	}
	return defaultUpstreamClient.Do(req)
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

func recordUpstreamSuccess(ctx context.Context, cfg *UpstreamConfig, selected *routing.SelectedChannel, modelName string, latencyMs int64) {
	if cfg == nil || cfg.Router == nil || selected == nil {
		return
	}
	if err := cfg.Router.RecordSuccess(ctx, selected.Channel.ID, float64(latencyMs), 0, &modelName, nil); err != nil {
		slog.Warn("RecordSuccess failed", "err", err, "channel_id", selected.Channel.ID, "model", modelName)
	}
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
// analyzing bounded SSE event state for error and empty-content detection.
//
// Disables the server-level WriteTimeout via http.ResponseController so long-running
// LLM streams (>60s) are not torn down mid-response.
func handleStreamUpstream(w http.ResponseWriter, r *http.Request, resp *http.Response, latencyMs int64) {
	if resp == nil || resp.Body == nil {
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		relayUpstreamErrorResponse(w, resp, latencyMs)
		return
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
			slog.Info("SSE downstream context ended",
				"err", r.Context().Err(),
				"latency_ms", latencyMs,
				"streamed_bytes", streamedBytes,
			)
			return
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
				if _, writeErr := w.Write(chunk); writeErr != nil {
					slog.Warn("SSE downstream write failed",
						"err", writeErr,
						"latency_ms", latencyMs,
						"streamed_bytes", streamedBytes,
					)
					break
				}
				analyzer.Push(chunk)
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
	if sawStreamBytes {
		result := analyzer.Result()

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
	}
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
	failure := proxy.DetectProxyFailure(string(bodyBytes), &proxy.UsageSummary{})
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
