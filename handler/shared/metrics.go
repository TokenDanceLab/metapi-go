package shared

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MetricsCollector is a zero-dependency Prometheus metrics collector.
// It tracks proxy request counts and operational gauges using atomic counters.
// Kept in handler/shared so proxy and admin handlers can record without importing app
// (app already depends on handler packages).
type MetricsCollector struct {
	proxyRequestsTotal atomic.Int64
	proxyErrorsTotal   atomic.Int64
	proxyStreamsActive atomic.Int64
	activeChannels     atomic.Int64
	dbConnectionsOpen  atomic.Int64
	dbConnectionsInUse atomic.Int64
	dbConnErrorsTotal  atomic.Int64
	routeRebuildOK     atomic.Int64
	startTime          time.Time

	// Labeled outcomes + latency histograms (low-cardinality only).
	outcomesMu sync.Mutex
	outcomes   map[outcomeKey]int64
	histMu     sync.Mutex
	histograms map[histKey]*latencyHistogram
}

// Bounded outcome status labels (never raw HTTP codes or error strings).
const (
	OutcomeSuccess       = "success"
	OutcomeError         = "error"
	OutcomeTimeout       = "timeout"
	OutcomeUnavailable   = "unavailable"
	OutcomeUpstreamError = "upstream_error"
	OutcomeClientError   = "client_error"
)

// Bounded endpoint labels derived from path suffixes (never full URLs/models).
const (
	EndpointChat        = "chat"
	EndpointMessages    = "messages"
	EndpointResponses   = "responses"
	EndpointEmbeddings  = "embeddings"
	EndpointCompletions = "completions"
	EndpointImages      = "images"
	EndpointRerank      = "rerank"
	EndpointSearch      = "search"
	EndpointGemini      = "gemini"
	EndpointModels      = "models"
	EndpointOther       = "other"
)

// Default latency histogram buckets in seconds (TTFT-ish to long completions).
var defaultLatencyBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120,
}

type outcomeKey struct {
	endpoint string
	status   string
	stream   string
}

type histKey struct {
	endpoint string
	status   string
}

type latencyHistogram struct {
	buckets []float64
	counts  []uint64 // len = len(buckets)+1 (+Inf)
	sum     float64
	count   uint64
}

var globalMetrics = &MetricsCollector{
	startTime:  time.Now(),
	outcomes:   make(map[outcomeKey]int64),
	histograms: make(map[histKey]*latencyHistogram),
}

// ProxyObservation is a privacy-safe, low-cardinality proxy terminal event.
// It must never carry prompts, keys, cookies, model names, or upstream bodies.
type ProxyObservation struct {
	Endpoint string        // bounded label from EndpointLabelFromPath
	Status   string        // Outcome* constants
	Stream   bool          // stream vs non-stream
	Latency  time.Duration // total observed latency for the attempt/request
}

// Observer is an optional export hook for OTEL/Langfuse-style sinks.
// Implementations must not block; drop or buffer on backpressure.
// The default is a no-op.
type Observer interface {
	ObserveProxy(obs ProxyObservation)
}

type noopObserver struct{}

func (noopObserver) ObserveProxy(ProxyObservation) {}

var (
	observerMu sync.RWMutex
	observer   Observer = noopObserver{}
)

// SetObserver registers an optional export sink. Pass nil to reset to no-op.
// Safe for concurrent use; hot path reads under RLock.
func SetObserver(o Observer) {
	observerMu.Lock()
	defer observerMu.Unlock()
	if o == nil {
		observer = noopObserver{}
		return
	}
	observer = o
}

// GetObserver returns the currently registered Observer (never nil).
func GetObserver() Observer {
	observerMu.RLock()
	defer observerMu.RUnlock()
	return observer
}

// RecordProxyRequest increments the proxy request counter.
func RecordProxyRequest() { globalMetrics.proxyRequestsTotal.Add(1) }

// RecordProxyError increments the proxy error counter.
func RecordProxyError() { globalMetrics.proxyErrorsTotal.Add(1) }

// RecordStreamStart increments active SSE stream count.
func RecordStreamStart() { globalMetrics.proxyStreamsActive.Add(1) }

// RecordStreamEnd decrements active SSE stream count.
func RecordStreamEnd() { globalMetrics.proxyStreamsActive.Add(-1) }

// SetActiveChannels sets the active channel gauge.
func SetActiveChannels(n int64) { globalMetrics.activeChannels.Store(n) }

// SetDBConnections sets the DB open-connection gauge.
func SetDBConnections(n int64) { globalMetrics.dbConnectionsOpen.Store(n) }

// SetDBConnectionsInUse sets the DB in-use connection gauge.
func SetDBConnectionsInUse(n int64) { globalMetrics.dbConnectionsInUse.Store(n) }

// RecordDBConnError increments the DB connection-budget / open error counter
// (e.g. SQLSTATE 53300 too many connections for role).
func RecordDBConnError() { globalMetrics.dbConnErrorsTotal.Add(1) }

// RecordRouteRebuildCompleted increments successful route rebuild/cache-invalidate counter.
func RecordRouteRebuildCompleted() { globalMetrics.routeRebuildOK.Add(1) }

// ObserveProxyOutcome records a terminal proxy outcome: labeled counter, latency
// histogram, legacy error counter (when not success), and optional Observer hook.
// Labels are sanitized to a fixed allowlist; unknown values collapse to safe defaults.
func ObserveProxyOutcome(obs ProxyObservation) {
	endpoint := sanitizeEndpoint(obs.Endpoint)
	status := sanitizeStatus(obs.Status)
	stream := "false"
	if obs.Stream {
		stream = "true"
	}
	if latency := obs.Latency; latency < 0 {
		obs.Latency = 0
	}

	key := outcomeKey{endpoint: endpoint, status: status, stream: stream}
	globalMetrics.outcomesMu.Lock()
	globalMetrics.outcomes[key]++
	globalMetrics.outcomesMu.Unlock()

	observeLatency(endpoint, status, obs.Latency)

	if status != OutcomeSuccess {
		globalMetrics.proxyErrorsTotal.Add(1)
	}

	// Export hook last so metric state is consistent even if the sink panics
	// (recover so a bad sink cannot break the proxy hot path).
	func() {
		defer func() { _ = recover() }()
		GetObserver().ObserveProxy(ProxyObservation{
			Endpoint: endpoint,
			Status:   status,
			Stream:   obs.Stream,
			Latency:  obs.Latency,
		})
	}()
}

func observeLatency(endpoint, status string, d time.Duration) {
	seconds := d.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	hk := histKey{endpoint: endpoint, status: status}
	globalMetrics.histMu.Lock()
	h := globalMetrics.histograms[hk]
	if h == nil {
		h = newLatencyHistogram(defaultLatencyBuckets)
		globalMetrics.histograms[hk] = h
	}
	h.observe(seconds)
	globalMetrics.histMu.Unlock()
}

func newLatencyHistogram(buckets []float64) *latencyHistogram {
	cp := append([]float64(nil), buckets...)
	return &latencyHistogram{
		buckets: cp,
		counts:  make([]uint64, len(cp)+1),
	}
}

func (h *latencyHistogram) observe(seconds float64) {
	h.count++
	h.sum += seconds
	idx := len(h.buckets) // +Inf
	for i, b := range h.buckets {
		if seconds <= b {
			idx = i
			break
		}
	}
	h.counts[idx]++
}

// EndpointLabelFromPath maps a request path to a low-cardinality endpoint label.
// Never returns raw paths; unknown paths become "other".
func EndpointLabelFromPath(path string) string {
	path = strings.TrimSpace(path)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	path = strings.ToLower(strings.TrimRight(path, "/"))
	switch {
	case strings.HasSuffix(path, "/v1/chat/completions") || path == "/chat/completions" || path == "/v1/chat/completions":
		return EndpointChat
	case strings.HasSuffix(path, "/v1/messages") || path == "/messages" || path == "/v1/messages" ||
		strings.HasSuffix(path, "/anthropic/v1/messages") || strings.HasSuffix(path, "/messages/count_tokens"):
		return EndpointMessages
	case strings.Contains(path, "/v1/responses") || path == "/responses":
		return EndpointResponses
	case strings.HasSuffix(path, "/v1/embeddings") || path == "/embeddings":
		return EndpointEmbeddings
	case strings.HasSuffix(path, "/v1/completions") || path == "/completions":
		return EndpointCompletions
	case strings.Contains(path, "/v1/images"):
		return EndpointImages
	case strings.HasSuffix(path, "/v1/rerank") || path == "/rerank":
		return EndpointRerank
	case strings.HasSuffix(path, "/v1/search") || path == "/search":
		return EndpointSearch
	case strings.Contains(path, "generatecontent") || strings.Contains(path, "counttokens") ||
		strings.Contains(path, "/v1beta/") || strings.Contains(path, "/v1internal"):
		return EndpointGemini
	case strings.HasSuffix(path, "/v1/models") || path == "/models" || strings.HasSuffix(path, "/models"):
		return EndpointModels
	default:
		return EndpointOther
	}
}

// StatusFromHTTP maps an HTTP status to a bounded outcome label.
func StatusFromHTTP(code int) string {
	switch {
	case code >= 200 && code < 300:
		return OutcomeSuccess
	case code == http.StatusRequestTimeout || code == 408:
		return OutcomeTimeout
	case code == http.StatusServiceUnavailable || code == http.StatusTooManyRequests:
		return OutcomeUnavailable
	case code >= 400 && code < 500:
		return OutcomeClientError
	case code >= 500:
		return OutcomeUpstreamError
	default:
		return OutcomeError
	}
}

func sanitizeEndpoint(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case EndpointChat, EndpointMessages, EndpointResponses, EndpointEmbeddings,
		EndpointCompletions, EndpointImages, EndpointRerank, EndpointSearch,
		EndpointGemini, EndpointModels, EndpointOther:
		return strings.TrimSpace(strings.ToLower(v))
	case "":
		return EndpointOther
	default:
		return EndpointOther
	}
}

func sanitizeStatus(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case OutcomeSuccess, OutcomeError, OutcomeTimeout, OutcomeUnavailable,
		OutcomeUpstreamError, OutcomeClientError:
		return strings.TrimSpace(strings.ToLower(v))
	case "":
		return OutcomeError
	default:
		return OutcomeError
	}
}

// ResetMetricsForTest clears counters/gauges for deterministic tests.
func ResetMetricsForTest() {
	globalMetrics.proxyRequestsTotal.Store(0)
	globalMetrics.proxyErrorsTotal.Store(0)
	globalMetrics.proxyStreamsActive.Store(0)
	globalMetrics.activeChannels.Store(0)
	globalMetrics.dbConnectionsOpen.Store(0)
	globalMetrics.dbConnectionsInUse.Store(0)
	globalMetrics.dbConnErrorsTotal.Store(0)
	globalMetrics.routeRebuildOK.Store(0)
	globalMetrics.startTime = time.Now()
	globalMetrics.outcomesMu.Lock()
	globalMetrics.outcomes = make(map[outcomeKey]int64)
	globalMetrics.outcomesMu.Unlock()
	globalMetrics.histMu.Lock()
	globalMetrics.histograms = make(map[histKey]*latencyHistogram)
	globalMetrics.histMu.Unlock()
	SetObserver(nil)
}

// SnapshotForTest returns current counter values for assertions.
func SnapshotForTest() (requests, errors, streams, rebuilds int64) {
	return globalMetrics.proxyRequestsTotal.Load(),
		globalMetrics.proxyErrorsTotal.Load(),
		globalMetrics.proxyStreamsActive.Load(),
		globalMetrics.routeRebuildOK.Load()
}

// OutcomeSnapshotForTest returns labeled outcome counts for assertions.
func OutcomeSnapshotForTest() map[string]int64 {
	globalMetrics.outcomesMu.Lock()
	defer globalMetrics.outcomesMu.Unlock()
	out := make(map[string]int64, len(globalMetrics.outcomes))
	for k, v := range globalMetrics.outcomes {
		out[fmt.Sprintf("%s|%s|%s", k.endpoint, k.status, k.stream)] = v
	}
	return out
}

// HistogramCountForTest returns sample count for one labeled histogram series.
func HistogramCountForTest(endpoint, status string) uint64 {
	endpoint = sanitizeEndpoint(endpoint)
	status = sanitizeStatus(status)
	globalMetrics.histMu.Lock()
	defer globalMetrics.histMu.Unlock()
	h := globalMetrics.histograms[histKey{endpoint: endpoint, status: status}]
	if h == nil {
		return 0
	}
	return h.count
}

// WritePrometheusMetrics writes Prometheus text-format metrics to w.
func WritePrometheusMetrics(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	m := globalMetrics
	uptime := time.Since(m.startTime).Seconds()

	var b []byte
	appendLine := func(format string, args ...any) {
		b = append(b, []byte(fmt.Sprintf(format, args...))...)
	}

	appendLine("# HELP metapi_uptime_seconds Process uptime in seconds\n")
	appendLine("# TYPE metapi_uptime_seconds gauge\n")
	appendLine("metapi_uptime_seconds %.0f\n", uptime)

	appendLine("# HELP metapi_proxy_requests_total Total proxy requests served\n")
	appendLine("# TYPE metapi_proxy_requests_total counter\n")
	appendLine("metapi_proxy_requests_total %d\n", m.proxyRequestsTotal.Load())

	appendLine("# HELP metapi_proxy_errors_total Total proxy upstream errors\n")
	appendLine("# TYPE metapi_proxy_errors_total counter\n")
	appendLine("metapi_proxy_errors_total %d\n", m.proxyErrorsTotal.Load())

	appendLine("# HELP metapi_proxy_streams_active Active SSE proxy streams\n")
	appendLine("# TYPE metapi_proxy_streams_active gauge\n")
	appendLine("metapi_proxy_streams_active %d\n", m.proxyStreamsActive.Load())

	appendLine("# HELP metapi_active_channels Active proxy channels\n")
	appendLine("# TYPE metapi_active_channels gauge\n")
	appendLine("metapi_active_channels %d\n", m.activeChannels.Load())

	appendLine("# HELP metapi_db_connections_open Open database connections\n")
	appendLine("# TYPE metapi_db_connections_open gauge\n")
	appendLine("metapi_db_connections_open %d\n", m.dbConnectionsOpen.Load())

	appendLine("# HELP metapi_db_connections_in_use In-use database connections\n")
	appendLine("# TYPE metapi_db_connections_in_use gauge\n")
	appendLine("metapi_db_connections_in_use %d\n", m.dbConnectionsInUse.Load())

	appendLine("# HELP metapi_db_conn_errors_total Database connection budget / open errors\n")
	appendLine("# TYPE metapi_db_conn_errors_total counter\n")
	appendLine("metapi_db_conn_errors_total %d\n", m.dbConnErrorsTotal.Load())

	appendLine("# HELP metapi_route_rebuild_total Total route cache rebuild/invalidate operations\n")
	appendLine("# TYPE metapi_route_rebuild_total counter\n")
	appendLine("metapi_route_rebuild_total{result=\"completed\"} %d\n", m.routeRebuildOK.Load())

	// Labeled terminal outcomes (privacy-safe, bounded labels).
	appendLine("# HELP metapi_proxy_outcomes_total Terminal proxy outcomes by endpoint/status/stream\n")
	appendLine("# TYPE metapi_proxy_outcomes_total counter\n")
	m.outcomesMu.Lock()
	outcomeKeys := make([]outcomeKey, 0, len(m.outcomes))
	for k := range m.outcomes {
		outcomeKeys = append(outcomeKeys, k)
	}
	sort.Slice(outcomeKeys, func(i, j int) bool {
		a, b := outcomeKeys[i], outcomeKeys[j]
		if a.endpoint != b.endpoint {
			return a.endpoint < b.endpoint
		}
		if a.status != b.status {
			return a.status < b.status
		}
		return a.stream < b.stream
	})
	for _, k := range outcomeKeys {
		appendLine("metapi_proxy_outcomes_total{endpoint=%q,status=%q,stream=%q} %d\n",
			k.endpoint, k.status, k.stream, m.outcomes[k])
	}
	m.outcomesMu.Unlock()

	// Latency histogram (cumulative buckets).
	appendLine("# HELP metapi_proxy_request_duration_seconds Proxy request duration in seconds\n")
	appendLine("# TYPE metapi_proxy_request_duration_seconds histogram\n")
	m.histMu.Lock()
	histKeys := make([]histKey, 0, len(m.histograms))
	for k := range m.histograms {
		histKeys = append(histKeys, k)
	}
	sort.Slice(histKeys, func(i, j int) bool {
		a, b := histKeys[i], histKeys[j]
		if a.endpoint != b.endpoint {
			return a.endpoint < b.endpoint
		}
		return a.status < b.status
	})
	for _, k := range histKeys {
		h := m.histograms[k]
		if h == nil || h.count == 0 {
			continue
		}
		var cum uint64
		for i, bound := range h.buckets {
			cum += h.counts[i]
			appendLine("metapi_proxy_request_duration_seconds_bucket{endpoint=%q,status=%q,le=\"%g\"} %d\n",
				k.endpoint, k.status, bound, cum)
		}
		cum += h.counts[len(h.buckets)]
		appendLine("metapi_proxy_request_duration_seconds_bucket{endpoint=%q,status=%q,le=\"+Inf\"} %d\n",
			k.endpoint, k.status, cum)
		appendLine("metapi_proxy_request_duration_seconds_sum{endpoint=%q,status=%q} %g\n",
			k.endpoint, k.status, h.sum)
		appendLine("metapi_proxy_request_duration_seconds_count{endpoint=%q,status=%q} %d\n",
			k.endpoint, k.status, h.count)
	}
	m.histMu.Unlock()

	_, err := w.Write(b)
	return err
}
