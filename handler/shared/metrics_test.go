package shared

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsCollector_RecordAndExpose(t *testing.T) {
	ResetMetricsForTest()
	RecordProxyRequest()
	RecordProxyRequest()
	RecordProxyError()
	RecordStreamStart()
	RecordRouteRebuildCompleted()

	req, err, streams, rebuilds := SnapshotForTest()
	if req != 2 || err != 1 || streams != 1 || rebuilds != 1 {
		t.Fatalf("snapshot = req=%d err=%d streams=%d rebuilds=%d", req, err, streams, rebuilds)
	}

	rec := httptest.NewRecorder()
	if writeErr := WritePrometheusMetrics(rec); writeErr != nil {
		t.Fatalf("WritePrometheusMetrics: %v", writeErr)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "metapi_proxy_requests_total 2") {
		t.Fatalf("body missing requests counter: %s", body)
	}
	if !strings.Contains(body, "metapi_proxy_errors_total 1") {
		t.Fatalf("body missing errors counter: %s", body)
	}
	RecordStreamEnd()
	_, _, streams, _ = SnapshotForTest()
	if streams != 0 {
		t.Fatalf("streams after end = %d, want 0", streams)
	}
}
