package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	shared.ObserveProxyOutcome(shared.ProxyObservation{
		Endpoint: shared.EndpointChat,
		Status:   shared.OutcomeSuccess,
		Stream:   false,
		Latency:  100 * time.Millisecond,
	})

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
		"# TYPE metapi_proxy_outcomes_total counter",
		`metapi_proxy_outcomes_total{endpoint="chat",status="success",stream="false"} 1`,
		"# TYPE metapi_proxy_request_duration_seconds histogram",
		`metapi_proxy_request_duration_seconds_count{endpoint="chat",status="success"} 1`,
	}
	for _, want := range required {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
	shared.RecordStreamEnd()
}

func TestSetMetricsObserver_AppSurface(t *testing.T) {
	shared.ResetMetricsForTest()
	var called int
	SetMetricsObserver(observerFunc(func(obs shared.ProxyObservation) {
		called++
		if obs.Endpoint != shared.EndpointEmbeddings {
			t.Fatalf("endpoint = %q", obs.Endpoint)
		}
	}))
	ObserveProxyOutcome(shared.ProxyObservation{
		Endpoint: shared.EndpointEmbeddings,
		Status:   shared.OutcomeSuccess,
		Latency:  time.Millisecond,
	})
	if called != 1 {
		t.Fatalf("called = %d, want 1", called)
	}
	SetMetricsObserver(nil)
}

type observerFunc func(shared.ProxyObservation)

func (f observerFunc) ObserveProxy(obs shared.ProxyObservation) { f(obs) }
