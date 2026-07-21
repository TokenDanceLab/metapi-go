package e2e

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/proxy"
)

// TestP0585HTTP_MultiChannel5xxStorm_ChannelScopedExclude is an HTTP-path
// load-proof for P0-585 cascade isolation.
//
// Unlike proxy/conductor unit tests, this drives auth → handler → conductor →
// mock upstream over httptest, proving request-path exclude stays channel-scoped
// under a multi-channel 5xx storm and respects ProxyMaxChannelAttempts.
//
// Honesty: this strengthens automated evidence. Inventory P0-585 remains
// **partial** until production/live multi-channel e2e (do not flip present).
func TestP0585HTTP_MultiChannel5xxStorm_ChannelScopedExclude(t *testing.T) {
	const budget = 3
	const channelCount = 6

	// Per-channel upstream: always 503 so the conductor must failover.
	type hit struct {
		channelID int64
		n         atomic.Int64
	}
	hits := make([]*hit, channelCount)
	upstreams := make([]*httptest.Server, channelCount)
	for i := 0; i < channelCount; i++ {
		id := int64(i + 1)
		h := &hit{channelID: id}
		hits[i] = h
		upstreams[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.n.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"upstream down","type":"server_error"}}`))
		}))
		t.Cleanup(upstreams[i].Close)
	}

	mockR := newMockRouter()
	// FIFO stack so SelectChannel + SelectNextChannel walk channels 1..N.
	for i := 0; i < channelCount; i++ {
		mockR.pushChannel(makeChannel(int64(i+1), upstreams[i].URL, "gpt-4"))
	}

	cfg := makeTestConfig()
	cfg.ProxyMaxChannelAttempts = budget
	config.Set(cfg)
	t.Cleanup(func() { config.Set(makeTestConfig()) })
	coord := proxy.NewProxyChannelCoordinator(cfg)
	r := setupE2ERouter(mockR, coord, injectAuthMiddleware("global", nil, "global-proxy-token"))

	req := httptest.NewRequest("POST", "/chat/completions",
		strings.NewReader(`{"model":"gpt-4","messages":[{"role":"user","content":"storm"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Storm must fail (no healthy channel); status is upstream-ish (503 or 502).
	if rec.Code == http.StatusOK {
		t.Fatalf("expected storm failure, got 200 body=%s", rec.Body.String())
	}

	// Exactly budget channels selected; no channel twice.
	if len(mockR.selectedIDs) != budget {
		t.Fatalf("selectedIDs=%v (len %d), want %d unique attempts", mockR.selectedIDs, len(mockR.selectedIDs), budget)
	}
	seen := map[int64]bool{}
	for _, id := range mockR.selectedIDs {
		if seen[id] {
			t.Fatalf("P0-585: channel %d selected twice under storm: %v", id, mockR.selectedIDs)
		}
		seen[id] = true
		if id < 1 || id > int64(channelCount) {
			t.Fatalf("unexpected channel id %d", id)
		}
	}

	// Exclude snapshots grow by one channel id each failover step.
	if len(mockR.excludeSnapshots) != budget-1 {
		t.Fatalf("excludeSnapshots=%v (len %d), want %d", mockR.excludeSnapshots, len(mockR.excludeSnapshots), budget-1)
	}
	for i, snap := range mockR.excludeSnapshots {
		if len(snap) != i+1 {
			t.Fatalf("excludeSnapshots[%d]=%v, want len %d", i, snap, i+1)
		}
		// Every exclude id must be a previously attempted channel (channel-scoped).
		for _, ex := range snap {
			if !containsInt64(mockR.selectedIDs, ex) {
				t.Fatalf("exclude id %d not in attempted set %v", ex, mockR.selectedIDs)
			}
		}
	}

	// Each selected channel's upstream was hit at least once; unselected never hit.
	selectedSet := map[int64]bool{}
	for _, id := range mockR.selectedIDs {
		selectedSet[id] = true
	}
	for _, h := range hits {
		n := h.n.Load()
		if selectedSet[h.channelID] {
			if n < 1 {
				t.Fatalf("channel %d selected but upstream hits=%d", h.channelID, n)
			}
		} else if n != 0 {
			t.Fatalf("channel %d not selected but upstream hits=%d (cascade leak?)", h.channelID, n)
		}
	}

	// Failures recorded only for attempted channels (no invent sibling poison here).
	if len(mockR.recordedFailures) < budget {
		t.Logf("recordedFailures=%d (may be less if surface collapses), selected=%v",
			len(mockR.recordedFailures), mockR.selectedIDs)
	}
	for _, f := range mockR.recordedFailures {
		if !selectedSet[f.channelID] {
			t.Fatalf("failure recorded for unselected channel %d", f.channelID)
		}
	}

	t.Logf("P0-585 HTTP load-proof: selected=%v excludes=%v status=%d — inventory remains partial (no prod e2e)",
		mockR.selectedIDs, mockR.excludeSnapshots, rec.Code)
}

// TestP0585HTTP_5xxThenHealthySiblingSucceeds proves channel-scoped failover
// recovers on a healthy sibling without re-hitting the failed channel.
func TestP0585HTTP_5xxThenHealthySiblingSucceeds(t *testing.T) {
	var badHits, goodHits atomic.Int64

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"temporary","type":"server_error"}}`))
	}))
	t.Cleanup(bad.Close)

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodHits.Add(1)
		writeJSONHelper(w, 200, map[string]any{
			"id": "chatcmpl-p0585", "object": "chat.completion", "model": "gpt-4",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{"role": "assistant", "content": "recovered"},
			}},
		})
	}))
	t.Cleanup(good.Close)

	mockR := newMockRouter()
	mockR.pushChannel(makeChannel(1, bad.URL, "gpt-4"))
	mockR.pushChannel(makeChannel(2, good.URL, "gpt-4"))
	// Extra channel that must not be selected if recovery works on #2.
	mockR.pushChannel(makeChannel(3, good.URL, "gpt-4"))

	cfg := makeTestConfig()
	cfg.ProxyMaxChannelAttempts = 3
	config.Set(cfg)
	t.Cleanup(func() { config.Set(makeTestConfig()) })
	coord := proxy.NewProxyChannelCoordinator(cfg)
	r := setupE2ERouter(mockR, coord, injectAuthMiddleware("global", nil, "global-proxy-token"))

	req := httptest.NewRequest("POST", "/chat/completions",
		strings.NewReader(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "recovered") {
		t.Fatalf("body=%q want recovered content", rec.Body.String())
	}
	if badHits.Load() != 1 {
		t.Fatalf("bad upstream hits=%d want 1", badHits.Load())
	}
	if goodHits.Load() != 1 {
		t.Fatalf("good upstream hits=%d want 1", goodHits.Load())
	}
	if len(mockR.selectedIDs) < 2 || mockR.selectedIDs[0] != 1 || mockR.selectedIDs[1] != 2 {
		t.Fatalf("selectedIDs=%v want [1,2,...]", mockR.selectedIDs)
	}
	// First exclude after channel 1 fail must include 1 only (channel-scoped).
	if len(mockR.excludeSnapshots) < 1 {
		t.Fatal("expected at least one SelectNext exclude snapshot")
	}
	first := mockR.excludeSnapshots[0]
	if len(first) != 1 || first[0] != 1 {
		t.Fatalf("first exclude=%v want [1] (channel-scoped, not site-wide invent)", first)
	}
	// Channel 3 must not appear in selected path when #2 succeeds.
	for _, id := range mockR.selectedIDs {
		if id == 3 {
			t.Fatalf("channel 3 selected after recovery: %v", mockR.selectedIDs)
		}
	}
	t.Logf("P0-585 HTTP recover: selected=%v excludes=%v", mockR.selectedIDs, mockR.excludeSnapshots)
}

func containsInt64(xs []int64, v int64) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

