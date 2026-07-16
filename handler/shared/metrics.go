package shared

import (
	"fmt"
	"net/http"
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
	routeRebuildOK     atomic.Int64
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

// RecordRouteRebuildCompleted increments successful route rebuild/cache-invalidate counter.
func RecordRouteRebuildCompleted() { globalMetrics.routeRebuildOK.Add(1) }

// ResetMetricsForTest clears counters/gauges for deterministic tests.
func ResetMetricsForTest() {
	globalMetrics.proxyRequestsTotal.Store(0)
	globalMetrics.proxyErrorsTotal.Store(0)
	globalMetrics.proxyStreamsActive.Store(0)
	globalMetrics.activeChannels.Store(0)
	globalMetrics.dbConnectionsOpen.Store(0)
	globalMetrics.routeRebuildOK.Store(0)
	globalMetrics.startTime = time.Now()
}

// SnapshotForTest returns current counter values for assertions.
func SnapshotForTest() (requests, errors, streams, rebuilds int64) {
	return globalMetrics.proxyRequestsTotal.Load(),
		globalMetrics.proxyErrorsTotal.Load(),
		globalMetrics.proxyStreamsActive.Load(),
		globalMetrics.routeRebuildOK.Load()
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

	appendLine("# HELP metapi_route_rebuild_total Total route cache rebuild/invalidate operations\n")
	appendLine("# TYPE metapi_route_rebuild_total counter\n")
	appendLine("metapi_route_rebuild_total{result=\"completed\"} %d\n", m.routeRebuildOK.Load())

	_, err := w.Write(b)
	return err
}
