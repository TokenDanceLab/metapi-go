package app

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// MetricsCollector is a zero-dependency Prometheus metrics collector.
// It tracks proxy request counts and latency using atomic counters.
type MetricsCollector struct {
	proxyRequestsTotal atomic.Int64
	proxyErrorsTotal   atomic.Int64
	proxyStreamsActive atomic.Int64
	activeChannels     atomic.Int64
	dbConnectionsOpen  atomic.Int64
	startTime          time.Time
	mu                 sync.RWMutex
}

var globalMetrics = &MetricsCollector{startTime: time.Now()}

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

// SetDBConnections sets the DB connection gauge.
func SetDBConnections(n int64) { globalMetrics.dbConnectionsOpen.Store(n) }

// PrometheusHandler serves Prometheus text-format metrics at GET /metrics.
// Zero external dependencies — emits only the exposition format directly.
func PrometheusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	m := globalMetrics
	uptime := time.Since(m.startTime).Seconds()

	fmt.Fprintf(w, "# HELP metapi_uptime_seconds Process uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE metapi_uptime_seconds gauge\n")
	fmt.Fprintf(w, "metapi_uptime_seconds %.0f\n", uptime)

	fmt.Fprintf(w, "# HELP metapi_proxy_requests_total Total proxy requests served\n")
	fmt.Fprintf(w, "# TYPE metapi_proxy_requests_total counter\n")
	fmt.Fprintf(w, "metapi_proxy_requests_total %d\n", m.proxyRequestsTotal.Load())

	fmt.Fprintf(w, "# HELP metapi_proxy_errors_total Total proxy upstream errors\n")
	fmt.Fprintf(w, "# TYPE metapi_proxy_errors_total counter\n")
	fmt.Fprintf(w, "metapi_proxy_errors_total %d\n", m.proxyErrorsTotal.Load())

	fmt.Fprintf(w, "# HELP metapi_proxy_streams_active Active SSE proxy streams\n")
	fmt.Fprintf(w, "# TYPE metapi_proxy_streams_active gauge\n")
	fmt.Fprintf(w, "metapi_proxy_streams_active %d\n", m.proxyStreamsActive.Load())

	fmt.Fprintf(w, "# HELP metapi_active_channels Active proxy channels\n")
	fmt.Fprintf(w, "# TYPE metapi_active_channels gauge\n")
	fmt.Fprintf(w, "metapi_active_channels %d\n", m.activeChannels.Load())

	fmt.Fprintf(w, "# HELP metapi_db_connections_open Open database connections\n")
	fmt.Fprintf(w, "# TYPE metapi_db_connections_open gauge\n")
	fmt.Fprintf(w, "metapi_db_connections_open %d\n", m.dbConnectionsOpen.Load())
}
