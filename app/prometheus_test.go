package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/handler/shared"
)

func TestPrometheusHandler_FormatSmoke(t *testing.T) {
	shared.ResetMetricsForTest()
	shared.RecordProxyRequest()
	shared.RecordProxyError()
	shared.RecordStreamStart()
	shared.RecordRouteRebuildCompleted()
	shared.SetActiveChannels(2)
	shared.SetDBConnections(3)

	rec := httptest.NewRecorder()
	PrometheusHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	required := []string{
		"# HELP metapi_proxy_requests_total",
		"# TYPE metapi_proxy_requests_total counter",
		"metapi_proxy_requests_total 1",
		"metapi_proxy_errors_total 1",
		"metapi_proxy_streams_active 1",
		"metapi_active_channels 2",
		"metapi_db_connections_open 3",
		`metapi_route_rebuild_total{result="completed"} 1`,
		"# HELP metapi_uptime_seconds",
	}
	for _, want := range required {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
	shared.RecordStreamEnd()
}
