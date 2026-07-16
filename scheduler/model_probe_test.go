package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
)

// ---- fakes for #114 probe recording ----

type fakeProbe struct {
	mu       sync.Mutex
	outcomes map[int64]ProbeOutcome
	err      error
	calls    []ProbeTarget
}

func (f *fakeProbe) ProbeChannel(_ context.Context, target ProbeTarget) (ProbeOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, target)
	if f.err != nil {
		return ProbeOutcome{}, f.err
	}
	if out, ok := f.outcomes[target.ChannelID]; ok {
		return out, nil
	}
	return ProbeOutcome{Status: "inconclusive"}, nil
}

type fakeRecorder struct {
	mu            sync.Mutex
	successCalls  []probeSuccessCall
	failureCalls  []probeFailureCall
	successErr    error
	failureErr    error
}

type probeSuccessCall struct {
	ChannelID int64
	LatencyMs float64
	Model     *string
	AccountID *int64
}

type probeFailureCall struct {
	ChannelID  int64
	HTTPStatus *int
	ErrorText  *string
	Model      *string
	AccountID  *int64
}

func (f *fakeRecorder) RecordProbeSuccess(_ context.Context, channelID int64, latencyMs float64, modelName *string, actualAccountID *int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.successCalls = append(f.successCalls, probeSuccessCall{
		ChannelID: channelID,
		LatencyMs: latencyMs,
		Model:     modelName,
		AccountID: actualAccountID,
	})
	return f.successErr
}

func (f *fakeRecorder) RecordProbeFailure(_ context.Context, channelID int64, httpStatus *int, errorText *string, modelName *string, actualAccountID *int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failureCalls = append(f.failureCalls, probeFailureCall{
		ChannelID:  channelID,
		HTTPStatus: httpStatus,
		ErrorText:  errorText,
		Model:      modelName,
		AccountID:  actualAccountID,
	})
	return f.failureErr
}

func TestApplyProbeOutcome_SuccessAndFailureTransitions(t *testing.T) {
	rec := &fakeRecorder{}
	target := ProbeTarget{ChannelID: 9, AccountID: 3, SiteID: 1, ModelName: "gpt-x"}

	status, err := ApplyProbeOutcome(context.Background(), rec, target, ProbeOutcome{
		Status:    "success",
		LatencyMs: 42,
	})
	if err != nil || status != "success" {
		t.Fatalf("success: status=%s err=%v", status, err)
	}
	if len(rec.successCalls) != 1 || rec.successCalls[0].LatencyMs != 42 {
		t.Fatalf("success calls: %+v", rec.successCalls)
	}

	status, err = ApplyProbeOutcome(context.Background(), rec, target, ProbeOutcome{
		Status:     "failure",
		HTTPStatus: 502,
		ErrorText:  "bad gateway",
	})
	if err != nil || status != "failure" {
		t.Fatalf("failure: status=%s err=%v", status, err)
	}
	if len(rec.failureCalls) != 1 {
		t.Fatalf("failure calls: %+v", rec.failureCalls)
	}
	fc := rec.failureCalls[0]
	if fc.HTTPStatus == nil || *fc.HTTPStatus != 502 {
		t.Fatalf("http status = %v", fc.HTTPStatus)
	}
	if fc.ErrorText == nil || *fc.ErrorText != "bad gateway" {
		t.Fatalf("error text = %v", fc.ErrorText)
	}

	status, err = ApplyProbeOutcome(context.Background(), rec, target, ProbeOutcome{Status: "inconclusive"})
	if err != nil || status != "inconclusive" {
		t.Fatalf("inconclusive: status=%s err=%v", status, err)
	}
	// No additional success/failure calls.
	if len(rec.successCalls) != 1 || len(rec.failureCalls) != 1 {
		t.Fatalf("inconclusive mutated recorder: success=%d failure=%d", len(rec.successCalls), len(rec.failureCalls))
	}
}

func TestModelProbeScheduler_ProbeOne_RecordsSuccessAndFailure(t *testing.T) {
	cfg := &config.Config{
		ModelAvailabilityProbeEnabled:     true,
		ModelAvailabilityProbeIntervalMs:  60_000,
		ModelAvailabilityProbeTimeoutMs:   5000,
		ModelAvailabilityProbeConcurrency: 2,
	}
	s := NewModelProbeScheduler(cfg)
	probe := &fakeProbe{outcomes: map[int64]ProbeOutcome{
		1: {Status: "success", LatencyMs: 11},
		2: {Status: "failure", HTTPStatus: 503, ErrorText: "unavailable"},
		3: {Status: "inconclusive"},
	}}
	rec := &fakeRecorder{}
	s.SetProbeExecutor(probe)
	s.SetHealthRecorder(rec)

	if got := s.probeOne(ProbeTarget{ChannelID: 1, AccountID: 10, ModelName: "m1"}, 5000); got != "success" {
		t.Fatalf("channel 1 => %s", got)
	}
	if got := s.probeOne(ProbeTarget{ChannelID: 2, AccountID: 10, ModelName: "m2"}, 5000); got != "failure" {
		t.Fatalf("channel 2 => %s", got)
	}
	if got := s.probeOne(ProbeTarget{ChannelID: 3, AccountID: 10, ModelName: "m3"}, 5000); got != "inconclusive" {
		t.Fatalf("channel 3 => %s", got)
	}

	if len(rec.successCalls) != 1 || rec.successCalls[0].ChannelID != 1 {
		t.Fatalf("success calls: %+v", rec.successCalls)
	}
	if len(rec.failureCalls) != 1 || rec.failureCalls[0].ChannelID != 2 {
		t.Fatalf("failure calls: %+v", rec.failureCalls)
	}
}

func TestModelProbeScheduler_ProbeOne_ExecutorErrorIsInconclusive(t *testing.T) {
	s := NewModelProbeScheduler(testConfig())
	s.SetProbeExecutor(&fakeProbe{err: errors.New("dial boom")})
	s.SetHealthRecorder(&fakeRecorder{})
	if got := s.probeOne(ProbeTarget{ChannelID: 1, AccountID: 1, ModelName: "m"}, 1000); got != "inconclusive" {
		t.Fatalf("got %s", got)
	}
}

func TestModelProbeScheduler_ProbeOne_MissingDepsSkipped(t *testing.T) {
	s := NewModelProbeScheduler(testConfig())
	if got := s.probeOne(ProbeTarget{ChannelID: 1, AccountID: 1, ModelName: "m"}, 1000); got != "skipped" {
		t.Fatalf("got %s, want skipped when deps missing", got)
	}
}

func TestModelProbeScheduler_DisabledDoesNotStartTicker(t *testing.T) {
	cfg := testConfig()
	cfg.ModelAvailabilityProbeEnabled = false
	s := NewModelProbeScheduler(cfg)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// running stays false when disabled
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if running {
		t.Fatal("disabled scheduler should not mark running")
	}
	_ = s.Stop()
}

func TestModelProbeScheduler_StartStopWithDeps(t *testing.T) {
	cfg := testConfig()
	cfg.ModelAvailabilityProbeEnabled = true
	cfg.ModelAvailabilityProbeIntervalMs = 60_000
	s := NewModelProbeScheduler(cfg)
	s.SetProbeExecutor(&fakeProbe{outcomes: map[int64]ProbeOutcome{}})
	s.SetHealthRecorder(&fakeRecorder{})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
