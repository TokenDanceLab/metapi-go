package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
)

// UpstreamConfig holds the dependencies needed for upstream forwarding.
type UpstreamConfig struct {
	Router         proxy.TokenRouterInterface
	RouteRefresher proxy.RouteRefreshWorkflow
	Coordinator    *proxy.ProxyChannelCoordinator
	Executor       *proxy.RuntimeExecutor
}

var upstreamCfg *UpstreamConfig

// SetUpstreamConfig sets the package-level upstream forwarding dependencies.
// Called during server startup to wire in the routing engine and HTTP executor.
func SetUpstreamConfig(cfg *UpstreamConfig) {
	upstreamCfg = cfg
}

// getUpstreamConfig returns the configured upstream dependencies.
// Falls back to stub mode (returns nil) if not configured.
func getUpstreamConfig() *UpstreamConfig {
	return upstreamCfg
}

// dispatchUpstream forwards a proxy request to the selected upstream channel.
// Implements the spec's 10-step Handler pattern.
func dispatchUpstream(w http.ResponseWriter, r *http.Request, ctx *Ctx) {
	cfg := getUpstreamConfig()
	if cfg == nil {
		writeStubResponse(w, ctx)
		return
	}

	excludeChannelIDs := make([]int64, 0)
	maxRetries := ctx.MaxRetries

	for retry := 0; retry <= maxRetries; retry++ {
		// Step 6: Channel selection
		selected, err := proxy.SelectProxyChannelForAttempt(
			context.Background(),
			cfg.Router,
			cfg.Coordinator,
			cfg.RouteRefresher,
			proxy.ChannelSelectionInput{
				RequestedModel:    ctx.RequestedModel,
				DownstreamPolicy:  routing.EmptyDownstreamRoutingPolicy,
				ExcludeChannelIDs: excludeChannelIDs,
				RetryCount:        retry,
			},
		)
		if err != nil || selected == nil {
			slog.Warn("channel selection failed", "err", err, "model", ctx.RequestedModel, "retry", retry)
			writeJSONError(w, 503, "No available channels", "server_error")
			return
		}
		excludeChannelIDs = append(excludeChannelIDs, selected.Channel.ID)

		// Step 7: Build upstream request
		upstreamModel := selected.ActualModel
		if upstreamModel == "" {
			upstreamModel = ctx.RequestedModel
		}
		forwardBody := cloneAndSetModel(ctx.Body, upstreamModel)
		forwardBytes, _ := json.Marshal(forwardBody)
		upstreamURL := proxy.BuildUpstreamURL(selected.Site.URL, r.URL.Path)

		// Step 8: Send upstream request
		startedAt := time.Now()
		req, err := http.NewRequestWithContext(context.Background(), r.Method, upstreamURL, bytesReader(forwardBytes))
		if err != nil {
			slog.Warn("upstream request construction failed", "err", err, "url", upstreamURL, "model", upstreamModel)
			if retry < maxRetries {
				continue
			}
			writeJSONError(w, 502, "Upstream request failed", "upstream_error")
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if selected.TokenValue != "" {
			req.Header.Set("Authorization", "Bearer "+selected.TokenValue)
		}

		resp, err := http.DefaultClient.Do(req)
		latencyMs := time.Since(startedAt).Milliseconds()

		if err != nil {
			slog.Warn("upstream request failed", "err", err, "url", upstreamURL, "model", upstreamModel, "channel_id", selected.Channel.ID)
			if retry < maxRetries {
				continue
			}
			writeJSONError(w, 502, "Upstream request failed", "upstream_error")
			return
		}

		// Step 9: Handle response
		if ctx.IsStream {
			handleStreamUpstream(w, resp, latencyMs)
		} else {
			handleNonStreamUpstream(w, resp, latencyMs, ctx.RequestedModel, upstreamModel, selected.Channel.ID)
		}
		resp.Body.Close()
		return
	}

	writeJSONError(w, 503, "All channels exhausted", "server_error")
}

// writeStubResponse returns a stub response when upstream forwarding is not wired.
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
// It performs raw byte passthrough for minimal latency, then optionally parses the
// accumulated stream body to detect SSE error events and empty content.
func handleStreamUpstream(w http.ResponseWriter, resp *http.Response, latencyMs int64) {
	writeSSEHeaders(w)
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)

	// Copy upstream headers that are relevant
	if ct := resp.Header.Get("Content-Type"); ct != "" && strings.Contains(ct, "text/event-stream") {
		// Already set by writeSSEHeaders
	}

	var accumulated bytes.Buffer
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			accumulated.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				slog.Warn("SSE stream read error", "err", err, "latency_ms", latencyMs)
			}
			break
		}
	}

	// Post-stream SSE analysis: parse the accumulated body to check for
	// error events and empty content.
	if accumulated.Len() > 0 {
		result := ParseAndAnalyzeSseStream(accumulated.String())

		// Log SSE error events at WARN level
		if result.HasErrorEvent {
			LogSseErrorEvents(result.Events)
		}

		// Check for empty content (stream ended with no data events)
		if !result.HasDataEvent {
			emptyContentFail := os.Getenv("PROXY_EMPTY_CONTENT_FAIL")
			if strings.ToLower(emptyContentFail) == "true" || emptyContentFail == "1" {
				slog.Warn("SSE stream contained no data events",
					"latency_ms", latencyMs,
					"event_count", len(result.Events),
					"has_done_marker", result.HasDoneMarker,
				)
			}
		}
	}
}

// handleNonStreamUpstream writes a non-streaming upstream response to the downstream.
func handleNonStreamUpstream(w http.ResponseWriter, resp *http.Response, latencyMs int64, requestedModel, upstreamModel string, channelID int64) {
	bodyBytes, err := io.ReadAll(resp.Body)
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

// cloneAndSetModel returns a copy of the body with the model field replaced.
func cloneAndSetModel(body map[string]any, upstreamModel string) map[string]any {
	cloned := make(map[string]any, len(body)+1)
	for k, v := range body {
		if k == "model" {
			cloned[k] = upstreamModel
		} else {
			cloned[k] = v
		}
	}
	if _, ok := cloned["model"]; !ok {
		cloned["model"] = upstreamModel
	}
	return cloned
}

func bytesReader(b []byte) io.Reader {
	if len(b) == 0 {
		return nil
	}
	return strings.NewReader(string(b))
}

// Ensure imports are used
var _ = fmt.Sprintf
var _ = json.Marshal
var _ = context.Background
