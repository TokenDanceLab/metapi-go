package app

import (
	"log/slog"
	"net/http"

	"github.com/tokendancelab/metapi-go/handler/shared"
	"github.com/tokendancelab/metapi-go/store"
)

// RecordProxyRequest increments the proxy request counter.
func RecordProxyRequest() { shared.RecordProxyRequest() }

// RecordProxyError increments the proxy error counter.
func RecordProxyError() { shared.RecordProxyError() }

// RecordStreamStart increments active SSE stream count.
func RecordStreamStart() { shared.RecordStreamStart() }

// RecordStreamEnd decrements active SSE stream count.
func RecordStreamEnd() { shared.RecordStreamEnd() }

// SetActiveChannels sets the active channel gauge.
func SetActiveChannels(n int64) { shared.SetActiveChannels(n) }

// SetDBConnections sets the DB connection gauge.
func SetDBConnections(n int64) { shared.SetDBConnections(n) }

// RecordRouteRebuildCompleted increments successful route rebuild/cache-invalidate counter.
func RecordRouteRebuildCompleted() { shared.RecordRouteRebuildCompleted() }

func refreshRuntimeGauges() {
	if db := store.GetDB(); db != nil && db.DB != nil && db.DB.DB != nil {
		stats := db.DB.DB.Stats()
		shared.SetDBConnections(int64(stats.OpenConnections))
	}
}

// PrometheusHandler serves Prometheus text-format metrics at GET /metrics.
// Zero external dependencies — emits only the exposition format directly.
func PrometheusHandler(w http.ResponseWriter, r *http.Request) {
	refreshRuntimeGauges()
	if err := shared.WritePrometheusMetrics(w); err != nil {
		slog.Warn("metrics: failed to write prometheus exposition", "error", err)
	}
}
