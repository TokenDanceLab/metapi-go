package shared

import (
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	RecordStreamMissingUsage()
	RecordStreamMissingUsage()
	if got := StreamMissingUsageTotal(); got != 2 {
		t.Fatalf("StreamMissingUsageTotal = %d, want 2", got)
	}
	rec2 := httptest.NewRecorder()
	if writeErr := WritePrometheusMetrics(rec2); writeErr != nil {
		t.Fatalf("WritePrometheusMetrics after missing usage: %v", writeErr)
	}
	body2 := rec2.Body.String()
	if !strings.Contains(body2, "metapi_stream_missing_usage_total 2") {
		t.Fatalf("body missing stream missing usage counter: %s", body2)
	}
	RecordStreamEnd()
	_, _, streams, _ = SnapshotForTest()
	if streams != 0 {
		t.Fatalf("streams after end = %d, want 0", streams)
	}
}

func TestObserveProxyOutcome_HistogramAndLabels(t *testing.T) {
	ResetMetricsForTest()

	ObserveProxyOutcome(ProxyObservation{
		Endpoint: EndpointChat,
		Status:   OutcomeSuccess,
		Stream:   false,
		Latency:  120 * time.Millisecond,
	})
	ObserveProxyOutcome(ProxyObservation{
		Endpoint: EndpointChat,
		Status:   OutcomeSuccess,
		Stream:   true,
		Latency:  2 * time.Second,
	})
	ObserveProxyOutcome(ProxyObservation{
		Endpoint: EndpointMessages,
		Status:   OutcomeTimeout,
		Stream:   false,
		Latency:  15 * time.Second,
	})

	outcomes := OutcomeSnapshotForTest()
	if outcomes["chat|success|false"] != 1 {
		t.Fatalf("chat success non-stream = %d, want 1; all=%v", outcomes["chat|success|false"], outcomes)
	}
	if outcomes["chat|success|true"] != 1 {
		t.Fatalf("chat success stream = %d, want 1", outcomes["chat|success|true"])
	}
	if outcomes["messages|timeout|false"] != 1 {
		t.Fatalf("messages timeout = %d, want 1", outcomes["messages|timeout|false"])
	}

	if got := HistogramCountForTest(EndpointChat, OutcomeSuccess); got != 2 {
		t.Fatalf("chat success hist count = %d, want 2", got)
	}
	if got := HistogramCountForTest(EndpointMessages, OutcomeTimeout); got != 1 {
		t.Fatalf("messages timeout hist count = %d, want 1", got)
	}

	// Non-success outcomes also bump legacy error counter.
	_, errors, _, _ := SnapshotForTest()
	if errors != 1 {
		t.Fatalf("proxy_errors_total = %d, want 1 (timeout only)", errors)
	}

	rec := httptest.NewRecorder()
	if err := WritePrometheusMetrics(rec); err != nil {
		t.Fatalf("WritePrometheusMetrics: %v", err)
	}
	body := rec.Body.String()
	required := []string{
		`metapi_proxy_outcomes_total{endpoint="chat",status="success",stream="false"} 1`,
		`metapi_proxy_outcomes_total{endpoint="chat",status="success",stream="true"} 1`,
		`metapi_proxy_outcomes_total{endpoint="messages",status="timeout",stream="false"} 1`,
		`# TYPE metapi_proxy_request_duration_seconds histogram`,
		`metapi_proxy_request_duration_seconds_count{endpoint="chat",status="success"} 2`,
		`metapi_proxy_request_duration_seconds_bucket{endpoint="chat",status="success",le="+Inf"} 2`,
		// existing scrape series must remain
		"metapi_proxy_requests_total 0",
		"metapi_proxy_errors_total 1",
	}
	for _, want := range required {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
	// Cumulative bucket: 120ms should fall into le="0.25"
	if !strings.Contains(body, `metapi_proxy_request_duration_seconds_bucket{endpoint="chat",status="success",le="0.25"}`) {
		t.Fatalf("missing 0.25s bucket for chat success:\n%s", body)
	}
}

func TestEndpointLabelFromPath_Bounded(t *testing.T) {
	cases := map[string]string{
		"/v1/chat/completions":             EndpointChat,
		"/v1/messages":                     EndpointMessages,
		"/v1/responses":                    EndpointResponses,
		"/v1/embeddings":                   EndpointEmbeddings,
		"/v1/images/generations":           EndpointImages,
		"/v1/rerank":                       EndpointRerank,
		"/v1beta/models/x:generateContent": EndpointGemini,
		"/secret/raw/path?key=abc":         EndpointOther,
		"":                                 EndpointOther,
	}
	for path, want := range cases {
		if got := EndpointLabelFromPath(path); got != want {
			t.Fatalf("EndpointLabelFromPath(%q)=%q, want %q", path, got, want)
		}
	}
}

func TestObserveProxyOutcome_SanitizesHighCardinality(t *testing.T) {
	ResetMetricsForTest()
	// Attempt to inject model names / free text — must collapse.
	ObserveProxyOutcome(ProxyObservation{
		Endpoint: "gpt-4o-mini",
		Status:   "HTTP 502 bad gateway from site-42",
		Stream:   false,
		Latency:  time.Millisecond,
	})
	outcomes := OutcomeSnapshotForTest()
	if _, ok := outcomes["other|error|false"]; !ok {
		t.Fatalf("expected sanitized other|error|false, got %v", outcomes)
	}
	for k := range outcomes {
		if strings.Contains(k, "gpt") || strings.Contains(k, "502") || strings.Contains(k, "site") {
			t.Fatalf("high-cardinality label leaked: %q", k)
		}
	}
}

type countingObserver struct {
	n    atomic.Int64
	last ProxyObservation
}

func (c *countingObserver) ObserveProxy(obs ProxyObservation) {
	c.n.Add(1)
	c.last = obs
}

func TestObserverHook_ReceivesSanitizedObservation(t *testing.T) {
	ResetMetricsForTest()
	obs := &countingObserver{}
	SetObserver(obs)
	ObserveProxyOutcome(ProxyObservation{
		Endpoint: EndpointResponses,
		Status:   OutcomeSuccess,
		Stream:   true,
		Latency:  50 * time.Millisecond,
	})
	if obs.n.Load() != 1 {
		t.Fatalf("observer calls = %d, want 1", obs.n.Load())
	}
	if obs.last.Endpoint != EndpointResponses || obs.last.Status != OutcomeSuccess || !obs.last.Stream {
		t.Fatalf("observer last = %+v", obs.last)
	}
	// Reset clears observer to no-op.
	ResetMetricsForTest()
	ObserveProxyOutcome(ProxyObservation{Endpoint: EndpointChat, Status: OutcomeSuccess})
	if obs.n.Load() != 1 {
		t.Fatalf("after reset observer still called: %d", obs.n.Load())
	}
}

func TestStatusFromHTTP(t *testing.T) {
	if StatusFromHTTP(200) != OutcomeSuccess {
		t.Fatal("200")
	}
	if StatusFromHTTP(408) != OutcomeTimeout {
		t.Fatal("408")
	}
	if StatusFromHTTP(429) != OutcomeUnavailable {
		t.Fatal("429")
	}
	if StatusFromHTTP(400) != OutcomeClientError {
		t.Fatal("400")
	}
	if StatusFromHTTP(502) != OutcomeUpstreamError {
		t.Fatal("502")
	}
}
