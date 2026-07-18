package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	backupsvc "github.com/tokendancelab/metapi-go/service/backup"
	"github.com/tokendancelab/metapi-go/service/checkin"
	"github.com/tokendancelab/metapi-go/store"
)

// =============================================================================
// §1  Registry Tests
// =============================================================================

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.schedulers) != 0 {
		t.Errorf("expected 0 schedulers, got %d", len(r.schedulers))
	}
}

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()
	s1 := newMockScheduler("a")
	s2 := newMockScheduler("b")
	r.Register(s1)
	r.Register(s2)

	names := r.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 registered schedulers, got %d", len(names))
	}
	if names[0] != "a" || names[1] != "b" {
		t.Errorf("expected [a b], got %v", names)
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	names := r.List()
	if len(names) != 0 {
		t.Errorf("expected [] from empty registry, got %v", names)
	}

	r.Register(newMockScheduler("x"))
	names = r.List()
	if len(names) != 1 || names[0] != "x" {
		t.Errorf("expected [x], got %v", names)
	}
}

func TestRegistryStartAll(t *testing.T) {
	r := NewRegistry()
	m1 := newMockScheduler("m1")
	m2 := newMockScheduler("m2")
	r.Register(m1)
	r.Register(m2)

	ctx, cancel := context.WithCancel(context.Background())
	r.StartAll(ctx)

	// Allow goroutines to run Start().
	time.Sleep(50 * time.Millisecond)

	if !m1.started.Load() {
		t.Error("m1 was not started")
	}
	if !m2.started.Load() {
		t.Error("m2 was not started")
	}

	cancel()
	r.StopAll()

	if !m1.stopped.Load() {
		t.Error("m1 was not stopped")
	}
	if !m2.stopped.Load() {
		t.Error("m2 was not stopped")
	}
}

func TestRegistryStartAll_PanicRecovery(t *testing.T) {
	r := NewRegistry()
	panicSched := &panicScheduler{}
	m := newMockScheduler("normal")
	r.Register(panicSched)
	r.Register(m)

	ctx := context.Background()
	// Should not panic -- StartAll recovers panics.
	r.StartAll(ctx)

	time.Sleep(50 * time.Millisecond)

	if !m.started.Load() {
		t.Error("normal scheduler was not started after a panic in another scheduler")
	}
}

func TestRegistryStopAll_ErrorTolerance(t *testing.T) {
	r := NewRegistry()
	errSched := &errorStopScheduler{name: "err-stop"}
	m := newMockScheduler("ok")
	r.Register(errSched)
	r.Register(m)

	ctx := context.Background()
	r.StartAll(ctx)
	time.Sleep(30 * time.Millisecond)

	// StopAll should call both, log error for errSched, still stop ok.
	r.StopAll()

	if !m.stopped.Load() {
		t.Error("ok scheduler was not stopped")
	}
}

// =============================================================================
// §2  Cron Utilities Tests
// =============================================================================

func TestValidateCronExpr(t *testing.T) {
	tests := []struct {
		expr  string
		valid bool
	}{
		{"", false},
		{"   ", false},
		{"invalid", false},
		// 6-field expressions (sec min hour dom month dow) — valid
		{"* * * * * *", true},
		{"*/5 * * * * *", true},
		{"0 0 */6 * * *", true},
		{"0 0 6 * * *", true},
		{"0 58 23 * * *", true},
		{"0 0 0 1 * *", true},
		{"0 0 0 * * 0", true},
		{"30 0 */2 * * *", true},
		// 5-field expressions — auto-converted to 6-field, now valid
		{"* * * * *", true},
		{"*/5 * * * *", true},
		{"0 */6 * * *", true},
		{"0 8 * * *", true},
		{"58 23 * * *", true}, // daily-summary default
		{"0 6 * * *", true},   // log-cleanup default
		// Descriptor — not supported
		{"@every 1h", false},
		{"not a cron expr", false},
	}

	for _, tc := range tests {
		got := ValidateCronExpr(tc.expr)
		if got != tc.valid {
			t.Errorf("ValidateCronExpr(%q) = %v, want %v", tc.expr, got, tc.valid)
		}
	}
}

func TestParseCronExpr(t *testing.T) {
	t.Run("valid 6-field expr", func(t *testing.T) {
		if err := ParseCronExpr("0 0 */6 * * *"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid 5-field expr auto-converted", func(t *testing.T) {
		if err := ParseCronExpr("0 */6 * * *"); err != nil {
			t.Errorf("unexpected error for 5-field expr: %v", err)
		}
	})

	t.Run("empty expr", func(t *testing.T) {
		err := ParseCronExpr("")
		if err == nil {
			t.Error("expected error for empty cron expr")
		}
	})

	t.Run("spaces-only expr", func(t *testing.T) {
		err := ParseCronExpr("   ")
		if err == nil {
			t.Error("expected error for spaces-only cron expr")
		}
	})

	t.Run("invalid expr", func(t *testing.T) {
		err := ParseCronExpr("not valid")
		if err == nil {
			t.Error("expected error for invalid cron expr")
		}
	})
}

func TestNormalizeCronExpr(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   ", ""},
		{"* * * * *", "0 * * * * *"},
		{"*/5 * * * *", "0 */5 * * * *"},
		{"0 */6 * * *", "0 0 */6 * * *"},
		{"0 8 * * *", "0 0 8 * * *"},
		{"58 23 * * *", "0 58 23 * * *"},
		{"  0 8 * * *  ", "0 0 8 * * *"},                   // with surrounding whitespace
		{"0 0 */6 * * *", "0 0 */6 * * *"},                 // already 6-field, unchanged
		{"0   0   */6   *  *  *", "0   0   */6   *  *  *"}, // irregular spacing preserved (not 5 fields)
		{"@every 1h", "@every 1h"},                         // non-standard, passed through
	}

	for _, tc := range tests {
		got := normalizeCronExpr(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeCronExpr(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestCronRunner_AddJob(t *testing.T) {
	cr := newCronRunner()
	var count atomic.Int32

	id, err := cr.addJob("* * * * * *", func() {
		count.Add(1)
	})
	if err != nil {
		t.Fatalf("failed to add cron job: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero entry ID")
	}
}

func TestCronRunner_RemoveJob(t *testing.T) {
	cr := newCronRunner()
	var count atomic.Int32

	id, err := cr.addJob("* * * * * *", func() {
		count.Add(1)
	})
	if err != nil || id == 0 {
		t.Skip("cannot add cron job for removal test")
	}
	cr.removeJob(id)
	cr.start()
	time.Sleep(1500 * time.Millisecond)
	cr.stop()

	if count.Load() > 0 {
		t.Errorf("expected 0 runs after remove, got %d", count.Load())
	}
}

func TestCronRunner_StartStop(t *testing.T) {
	cr := newCronRunner()
	var count atomic.Int32

	_, err := cr.addJob("* * * * * *", func() {
		count.Add(1)
	})
	if err != nil {
		t.Skipf("cannot add cron job: %v", err)
	}

	cr.start()
	time.Sleep(2500 * time.Millisecond)
	cr.stop()

	if count.Load() < 1 {
		t.Errorf("expected at least 1 execution, got %d", count.Load())
	}
}

func TestCronRunner_PanicRecovery(t *testing.T) {
	cr := newCronRunner()
	var safeCount atomic.Int32

	_, err := cr.addJob("* * * * * *", func() {
		panic("deliberate panic in cron job")
	})
	if err != nil {
		t.Skipf("cannot add cron job: %v", err)
	}
	_, err = cr.addJob("* * * * * *", func() {
		safeCount.Add(1)
	})
	if err != nil {
		t.Skipf("cannot add second cron job: %v", err)
	}

	cr.start()
	time.Sleep(2500 * time.Millisecond)
	cr.stop()

	if safeCount.Load() < 1 {
		t.Error("safe cron job did not execute; panic may have crashed the runner")
	}
}

// =============================================================================
// §3  Helper Utilities Tests
// =============================================================================

func TestStringsTrimLower(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "active"},
		{"   ", "active"},
		{"  Cron  ", "cron"},
		{"INTERVAL", "interval"},
		{"Interval", "interval"},
		{"active", "active"},
	}

	for _, tc := range tests {
		got := stringsTrimLower(tc.input)
		if got != tc.expected {
			t.Errorf("stringsTrimLower(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestFormatErr(t *testing.T) {
	err := formatErr("invalid value: %d", 42)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Error() != "invalid value: 42" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestClampInt(t *testing.T) {
	tests := []struct {
		v, lo, hi, expected int
	}{
		{5, 1, 10, 5},
		{0, 1, 10, 1},
		{15, 1, 10, 10},
		{-5, 1, 10, 1},
		{1, 1, 10, 1},
		{10, 1, 10, 10},
		{5, -100, 100, 5},
		{0, 0, 0, 0},
		{100, 1, 24, 24},
		{0, 1, 3650, 1},
	}

	for _, tc := range tests {
		got := clampInt(tc.v, tc.lo, tc.hi)
		if got != tc.expected {
			t.Errorf("clampInt(%d, %d, %d) = %d, want %d",
				tc.v, tc.lo, tc.hi, got, tc.expected)
		}
	}
}

func TestMaxInt(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 2},
		{2, 1, 2},
		{0, 0, 0},
		{-1, 1, 1},
		{-5, -3, -3},
		{100, 50, 100},
	}

	for _, tc := range tests {
		got := maxInt(tc.a, tc.b)
		if got != tc.expected {
			t.Errorf("maxInt(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.expected)
		}
	}
}

func TestMaxInt64(t *testing.T) {
	tests := []struct {
		a, b, expected int64
	}{
		{1, 2, 2},
		{100, 50, 100},
		{0, 0, 0},
		{-1, 1, 1},
		{-5, -3, -3},
	}

	for _, tc := range tests {
		got := maxInt64(tc.a, tc.b)
		if got != tc.expected {
			t.Errorf("maxInt64(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.expected)
		}
	}
}

func TestToISOTime(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 30, 45, 0, time.UTC)
	got := toISOTime(now)
	expected := "2026-07-04T12:30:45Z"
	if got != expected {
		t.Errorf("toISOTime = %q, want %q", got, expected)
	}
}

func TestFormatTimeToSQL(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		now := time.Date(2026, 7, 4, 12, 30, 45, 0, time.UTC)
		got := formatTimeToSQL(now)
		expected := "2026-07-04T12:30:45Z"
		if got != expected {
			t.Errorf("formatTimeToSQL = %q, want %q", got, expected)
		}
	})

	t.Run("converts non-UTC to UTC RFC3339", func(t *testing.T) {
		loc := time.FixedZone("UTC+8", 8*3600)
		now := time.Date(2026, 7, 4, 20, 30, 45, 0, loc) // 12:30:45Z
		got := formatTimeToSQL(now)
		expected := "2026-07-04T12:30:45Z"
		if got != expected {
			t.Errorf("formatTimeToSQL = %q, want %q", got, expected)
		}
	})

	t.Run("zero", func(t *testing.T) {
		got := formatTimeToSQL(time.Time{})
		if got != "" {
			t.Errorf("expected empty for zero time, got %q", got)
		}
	})

	t.Run("lexicographic same-day compare vs RFC3339 rows", func(t *testing.T) {
		// Space-format cutoffs shield same-day old rows because 'T' > ' '.
		row := "2026-07-10T08:00:00Z"
		spaceCutoff := "2026-07-10 12:00:00"
		rfcCutoff := formatTimeToSQL(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
		if row < spaceCutoff {
			t.Fatalf("precondition failed: RFC3339 row unexpectedly < space cutoff")
		}
		if !(row < rfcCutoff) {
			t.Fatalf("RFC3339 row %q should be < cutoff %q", row, rfcCutoff)
		}
		newer := "2026-07-10T14:00:00Z"
		if newer < rfcCutoff {
			t.Fatalf("in-window row %q must not be < cutoff %q", newer, rfcCutoff)
		}
	})
}

// =============================================================================
// §4  CheckinScheduler Tests
// =============================================================================

func TestNewCheckinScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewCheckinScheduler(cfg)
	if s == nil {
		t.Fatal("NewCheckinScheduler returned nil")
	}
	if s.Name() != "checkin" {
		t.Errorf("Name() = %q, want %q", s.Name(), "checkin")
	}
	if s.mode != cfg.CheckinScheduleMode {
		t.Errorf("mode = %q, want %q", s.mode, cfg.CheckinScheduleMode)
	}
}

func TestCheckinScheduler_UpdateCheckinSchedule(t *testing.T) {
	cfg := testConfig()
	cfg.CheckinScheduleMode = "interval"
	s := NewCheckinScheduler(cfg)

	t.Run("invalid mode", func(t *testing.T) {
		err := s.UpdateCheckinSchedule("bogus", "* * * * *", 6)
		if err == nil {
			t.Error("expected error for invalid mode")
		}
	})

	t.Run("invalid cron", func(t *testing.T) {
		err := s.UpdateCheckinSchedule("cron", "not-a-cron", 6)
		if err == nil {
			t.Error("expected error for invalid cron expression")
		}
	})

	t.Run("interval too low", func(t *testing.T) {
		err := s.UpdateCheckinSchedule("interval", "* * * * *", 0)
		if err == nil {
			t.Error("expected error for interval hours < 1")
		}
	})

	t.Run("interval too high", func(t *testing.T) {
		err := s.UpdateCheckinSchedule("interval", "* * * * *", 25)
		if err == nil {
			t.Error("expected error for interval hours > 24")
		}
	})

	t.Run("valid cron mode", func(t *testing.T) {
		err := s.UpdateCheckinSchedule("cron", "0 0 */6 * * *", 12)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if s.mode != "cron" {
			t.Errorf("mode = %q, want cron", s.mode)
		}
		if cfg.CheckinCron != "0 0 */6 * * *" {
			t.Errorf("cron = %q, want 0 0 */6 * * *", cfg.CheckinCron)
		}
	})

	t.Run("valid interval mode", func(t *testing.T) {
		err := s.UpdateCheckinSchedule("interval", "* * * * *", 6)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if s.mode != "interval" {
			t.Errorf("mode = %q, want interval", s.mode)
		}
	})
}

func TestCheckinScheduler_FilterDue(t *testing.T) {
	cfg := testConfig()
	cfg.CheckinIntervalHours = 6
	s := NewCheckinScheduler(cfg)

	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

	makeCandidate := func(id int64, hoursAgo int) intervalCandidate {
		timestamp := now.Add(-time.Duration(hoursAgo) * time.Hour)
		iso := timestamp.Format(time.RFC3339)
		return intervalCandidate{id: id, lastCheckinAt: &iso}
	}

	t.Run("no candidates", func(t *testing.T) {
		due := s.filterDue([]intervalCandidate{}, now)
		if len(due) != 0 {
			t.Errorf("expected 0 due, got %d", len(due))
		}
	})

	t.Run("never checked in", func(t *testing.T) {
		c := intervalCandidate{id: 1, lastCheckinAt: nil}
		due := s.filterDue([]intervalCandidate{c}, now)
		if len(due) != 1 {
			t.Errorf("expected 1 due (never checked in), got %d", len(due))
		}
	})

	t.Run("checked in recently", func(t *testing.T) {
		c := makeCandidate(1, 2) // 2 hours ago, within 6-hour interval
		due := s.filterDue([]intervalCandidate{c}, now)
		if len(due) != 0 {
			t.Errorf("expected 0 due (within interval), got %d", len(due))
		}
	})

	t.Run("checked in long ago", func(t *testing.T) {
		c := makeCandidate(1, 8) // 8 hours ago, outside 6-hour interval
		due := s.filterDue([]intervalCandidate{c}, now)
		if len(due) != 1 {
			t.Errorf("expected 1 due (outside interval), got %d", len(due))
		}
	})

	t.Run("mixed candidates", func(t *testing.T) {
		neverChecked := intervalCandidate{id: 1, lastCheckinAt: nil}
		recent := makeCandidate(2, 2)
		old := makeCandidate(3, 8)
		due := s.filterDue([]intervalCandidate{neverChecked, recent, old}, now)
		if len(due) != 2 {
			t.Errorf("expected 2 due (never+old), got %d", len(due))
		}
		for _, d := range due {
			if d == recent.id {
				t.Errorf("recent candidate should NOT be due")
			}
		}
	})

	t.Run("invalid timestamp format treated as never checked", func(t *testing.T) {
		invalidTime := "not-a-valid-time"
		c := intervalCandidate{id: 1, lastCheckinAt: &invalidTime}
		due := s.filterDue([]intervalCandidate{c}, now)
		if len(due) != 1 {
			t.Errorf("expected 1 due (invalid time treated as never checked), got %d", len(due))
		}
	})

	t.Run("empty last_checkin_at string treated as never checked", func(t *testing.T) {
		emptyTime := ""
		c := intervalCandidate{id: 1, lastCheckinAt: &emptyTime}
		due := s.filterDue([]intervalCandidate{c}, now)
		if len(due) != 1 {
			t.Errorf("expected 1 due (empty time treated as never checked), got %d", len(due))
		}
	})

	t.Run("attempt suppresses re-checkin", func(t *testing.T) {
		c := makeCandidate(1, 8) // is due
		s.ResetAttempts()
		// Simulate an attempt in progress
		s.mu.Lock()
		s.attemptByAccount[c.id] = now.UnixMilli()
		s.mu.Unlock()
		due := s.filterDue([]intervalCandidate{c}, now)
		if len(due) != 0 {
			t.Errorf("expected 0 due (attempt in flight), got %d", len(due))
		}
	})
}

func TestCheckinScheduler_ResetAttempts(t *testing.T) {
	cfg := testConfig()
	s := NewCheckinScheduler(cfg)
	s.mu.Lock()
	s.attemptByAccount[1] = 12345
	s.mu.Unlock()

	s.ResetAttempts()
	s.mu.Lock()
	if len(s.attemptByAccount) != 0 {
		t.Errorf("expected empty attemptByAccount after reset, got %d entries", len(s.attemptByAccount))
	}
	s.mu.Unlock()
}

func TestCheckinScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	cfg.CheckinScheduleMode = "interval"
	s := NewCheckinScheduler(cfg)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	// Calling Stop again should be safe.
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

func TestCountResults(t *testing.T) {
	t.Run("all success", func(t *testing.T) {
		results := []checkin.CheckinAllResult{
			{AccountID: 1, Result: checkin.CheckinResult{Success: true}},
			{AccountID: 2, Result: checkin.CheckinResult{Success: true}},
		}
		ok, bad := countResults(results)
		if ok != 2 || bad != 0 {
			t.Errorf("countResults = (%d, %d), want (2, 0)", ok, bad)
		}
	})

	t.Run("all failures", func(t *testing.T) {
		results := []checkin.CheckinAllResult{
			{AccountID: 1, Result: checkin.CheckinResult{Success: false}},
			{AccountID: 2, Result: checkin.CheckinResult{Success: false}},
		}
		ok, bad := countResults(results)
		if ok != 0 || bad != 2 {
			t.Errorf("countResults = (%d, %d), want (0, 2)", ok, bad)
		}
	})

	t.Run("mixed", func(t *testing.T) {
		results := []checkin.CheckinAllResult{
			{AccountID: 1, Result: checkin.CheckinResult{Success: true}},
			{AccountID: 2, Result: checkin.CheckinResult{Success: false}},
			{AccountID: 3, Result: checkin.CheckinResult{Success: true}},
		}
		ok, bad := countResults(results)
		if ok != 2 || bad != 1 {
			t.Errorf("countResults = (%d, %d), want (2, 1)", ok, bad)
		}
	})

	t.Run("empty", func(t *testing.T) {
		ok, bad := countResults(nil)
		if ok != 0 || bad != 0 {
			t.Errorf("countResults(nil) = (%d, %d), want (0, 0)", ok, bad)
		}
	})
}

// =============================================================================
// §5  BalanceScheduler Tests
// =============================================================================

func TestNewBalanceScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewBalanceScheduler(cfg, nil)
	if s == nil {
		t.Fatal("NewBalanceScheduler returned nil")
	}
	if s.Name() != "balance-refresh" {
		t.Errorf("Name() = %q, want balance-refresh", s.Name())
	}
}

func TestBalanceScheduler_UpdateCron(t *testing.T) {
	cfg := testConfig()
	cfg.BalanceRefreshCron = "0 0 0 * * *"
	s := NewBalanceScheduler(cfg, nil)

	t.Run("invalid cron", func(t *testing.T) {
		err := s.UpdateCron("not-a-cron")
		if err == nil {
			t.Error("expected error for invalid cron")
		}
	})

	t.Run("valid cron", func(t *testing.T) {
		err := s.UpdateCron("30 0 */2 * * *")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if cfg.BalanceRefreshCron != "30 0 */2 * * *" {
			t.Errorf("cron not updated, got %q", cfg.BalanceRefreshCron)
		}
	})
}

func TestBalanceScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	cfg.BalanceRefreshCron = "0 0 0 * * *"
	s := NewBalanceScheduler(cfg, nil)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

// =============================================================================
// §6  ChannelRecoveryScheduler Tests
// =============================================================================

func TestNewChannelRecoveryScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewChannelRecoveryScheduler(cfg)
	if s == nil {
		t.Fatal("NewChannelRecoveryScheduler returned nil")
	}
	if s.Name() != "channel-recovery" {
		t.Errorf("Name() = %q, want channel-recovery", s.Name())
	}
	if len(s.inFlightKeys) != 0 {
		t.Error("expected empty inFlightKeys")
	}
	if len(s.lastStartedAtByKey) != 0 {
		t.Error("expected empty lastStartedAtByKey")
	}
}

func TestChannelRecovery_MergeCandidates(t *testing.T) {
	s := NewChannelRecoveryScheduler(testConfig())

	t.Run("cooling takes priority over same channel", func(t *testing.T) {
		cooling := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "gpt-4"},
		}
		active := []recoveryCandidate{
			{source: "active", channelID: 1, modelName: "gpt-4"},
		}
		merged := s.mergeCandidates(cooling, active)
		if len(merged) != 1 {
			t.Fatalf("expected 1 merged, got %d", len(merged))
		}
		if merged[0].source != "cooldown" {
			t.Errorf("expected cooldown to win, got %s", merged[0].source)
		}
	})

	t.Run("active added when not in cooling", func(t *testing.T) {
		cooling := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "gpt-4"},
		}
		active := []recoveryCandidate{
			{source: "active", channelID: 2, modelName: "claude-3"},
		}
		merged := s.mergeCandidates(cooling, active)
		if len(merged) != 2 {
			t.Errorf("expected 2 merged (different channels), got %d", len(merged))
		}
	})

	t.Run("empty inputs", func(t *testing.T) {
		merged := s.mergeCandidates(nil, nil)
		if len(merged) != 0 {
			t.Errorf("expected 0 merged, got %d", len(merged))
		}
	})

	t.Run("only cooling", func(t *testing.T) {
		cooling := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "gpt-4"},
			{source: "cooldown", channelID: 2, modelName: "claude-3"},
		}
		merged := s.mergeCandidates(cooling, nil)
		if len(merged) != 2 {
			t.Errorf("expected 2 merged, got %d", len(merged))
		}
	})
}

func TestChannelRecovery_FilterDue(t *testing.T) {
	s := NewChannelRecoveryScheduler(testConfig())
	nowMs := time.Now().UnixMilli()

	t.Run("empty candidates", func(t *testing.T) {
		due := s.filterDue(nil, nowMs)
		if len(due) != 0 {
			t.Errorf("expected 0 due, got %d", len(due))
		}
	})

	t.Run("never probed is due", func(t *testing.T) {
		candidates := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "gpt-4"},
		}
		due := s.filterDue(candidates, nowMs)
		if len(due) != 1 {
			t.Errorf("expected 1 due (never probed), got %d", len(due))
		}
	})

	t.Run("in flight suppressed", func(t *testing.T) {
		s.mu.Lock()
		s.inFlightKeys["1:gpt-4"] = true
		s.mu.Unlock()

		candidates := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "gpt-4"},
		}
		due := s.filterDue(candidates, nowMs)
		if len(due) != 0 {
			t.Errorf("expected 0 due (in flight), got %d", len(due))
		}

		s.mu.Lock()
		delete(s.inFlightKeys, "1:gpt-4")
		s.mu.Unlock()
	})

	t.Run("cooldown within recheck suppressed", func(t *testing.T) {
		s.mu.Lock()
		s.lastStartedAtByKey["1:gpt-4"] = nowMs - 5_000 // 5s ago, less than 30s recheck
		s.mu.Unlock()

		candidates := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "gpt-4"},
		}
		due := s.filterDue(candidates, nowMs)
		if len(due) != 0 {
			t.Errorf("expected 0 due (cooldown within recheck), got %d", len(due))
		}

		s.mu.Lock()
		delete(s.lastStartedAtByKey, "1:gpt-4")
		s.mu.Unlock()
	})

	t.Run("active within recheck suppressed", func(t *testing.T) {
		s.mu.Lock()
		s.lastStartedAtByKey["1:gpt-4"] = nowMs - 60_000 // 1m ago, less than 5m recheck
		s.mu.Unlock()

		candidates := []recoveryCandidate{
			{source: "active", channelID: 1, modelName: "gpt-4"},
		}
		due := s.filterDue(candidates, nowMs)
		if len(due) != 0 {
			t.Errorf("expected 0 due (active within recheck), got %d", len(due))
		}

		s.mu.Lock()
		delete(s.lastStartedAtByKey, "1:gpt-4")
		s.mu.Unlock()
	})

	t.Run("cooldown past recheck is due", func(t *testing.T) {
		s.mu.Lock()
		s.lastStartedAtByKey["1:gpt-4"] = nowMs - 60_000 // 60s ago, more than 30s recheck
		s.mu.Unlock()

		candidates := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "gpt-4"},
		}
		due := s.filterDue(candidates, nowMs)
		if len(due) != 1 {
			t.Errorf("expected 1 due (cooldown past recheck), got %d", len(due))
		}

		s.mu.Lock()
		delete(s.lastStartedAtByKey, "1:gpt-4")
		s.mu.Unlock()
	})

	t.Run("active past recheck is due", func(t *testing.T) {
		s.mu.Lock()
		s.lastStartedAtByKey["1:gpt-4"] = nowMs - (10 * 60_000) // 10m ago, more than 5m recheck
		s.mu.Unlock()

		candidates := []recoveryCandidate{
			{source: "active", channelID: 1, modelName: "gpt-4"},
		}
		due := s.filterDue(candidates, nowMs)
		if len(due) != 1 {
			t.Errorf("expected 1 due (active past recheck), got %d", len(due))
		}

		s.mu.Lock()
		delete(s.lastStartedAtByKey, "1:gpt-4")
		s.mu.Unlock()
	})
}

func TestChannelRecovery_Prioritize(t *testing.T) {
	s := NewChannelRecoveryScheduler(testConfig())

	t.Run("never-probed first", func(t *testing.T) {
		s.mu.Lock()
		s.lastStartedAtByKey["1:m1"] = 1000
		s.mu.Unlock()

		candidates := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "m1"},
			{source: "cooldown", channelID: 2, modelName: "m2"},
			{source: "cooldown", channelID: 3, modelName: "m3"},
		}
		s.prioritize(candidates)

		if len(candidates) < 3 {
			t.Fatal("expected 3 candidates after prioritize")
		}
		// Never-probed should come first
		for _, c := range candidates[:2] {
			if c.channelID == 1 {
				t.Error("probed channel 1 should not be in first two positions")
			}
		}

		s.mu.Lock()
		delete(s.lastStartedAtByKey, "1:m1")
		s.mu.Unlock()
	})

	t.Run("earliest-probed first among probed", func(t *testing.T) {
		s.mu.Lock()
		s.lastStartedAtByKey["1:a"] = 2000 // later
		s.lastStartedAtByKey["2:b"] = 1000 // earlier
		s.mu.Unlock()

		candidates := []recoveryCandidate{
			{source: "cooldown", channelID: 1, modelName: "a"},
			{source: "cooldown", channelID: 2, modelName: "b"},
		}
		s.prioritize(candidates)

		if candidates[0].channelID != 2 {
			t.Errorf("expected earlier-probed channel 2 first, got %d", candidates[0].channelID)
		}

		s.mu.Lock()
		delete(s.lastStartedAtByKey, "1:a")
		delete(s.lastStartedAtByKey, "2:b")
		s.mu.Unlock()
	})

	t.Run("tiebreaker lower channel ID first", func(t *testing.T) {
		s.mu.Lock()
		s.lastStartedAtByKey["5:x"] = 1000
		s.lastStartedAtByKey["3:y"] = 1000
		s.mu.Unlock()

		candidates := []recoveryCandidate{
			{source: "cooldown", channelID: 5, modelName: "x"},
			{source: "cooldown", channelID: 3, modelName: "y"},
		}
		s.prioritize(candidates)

		if candidates[0].channelID != 3 {
			t.Errorf("expected lower channel ID 3 first, got %d", candidates[0].channelID)
		}

		s.mu.Lock()
		delete(s.lastStartedAtByKey, "5:x")
		delete(s.lastStartedAtByKey, "3:y")
		s.mu.Unlock()
	})

	t.Run("stable for single candidate", func(t *testing.T) {
		candidates := []recoveryCandidate{
			{source: "cooldown", channelID: 42, modelName: "sole"},
		}
		s.prioritize(candidates)
		if len(candidates) != 1 || candidates[0].channelID != 42 {
			t.Errorf("single candidate was altered")
		}
	})
}

// =============================================================================
// §7  ModelProbeScheduler Tests
// =============================================================================

func TestNewModelProbeScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewModelProbeScheduler(cfg)
	if s == nil {
		t.Fatal("NewModelProbeScheduler returned nil")
	}
	if s.Name() != "model-probe" {
		t.Errorf("Name() = %q, want model-probe", s.Name())
	}
	if len(s.accountLeases) != 0 {
		t.Error("expected empty accountLeases")
	}
}

func TestModelProbeScheduler_AccountLease(t *testing.T) {
	cfg := testConfig()
	s := NewModelProbeScheduler(cfg)

	if !s.TryAcquireAccountLease(1) {
		t.Error("first acquire should succeed")
	}
	if s.TryAcquireAccountLease(1) {
		t.Error("second acquire on same account should fail (already leased)")
	}
	if !s.TryAcquireAccountLease(2) {
		t.Error("acquire on different account should succeed")
	}

	s.ReleaseAccountLease(1)
	if !s.TryAcquireAccountLease(1) {
		t.Error("re-acquire after release should succeed")
	}

	// Release non-existent key should not panic.
	s.ReleaseAccountLease(999)
}

func TestModelProbeScheduler_ResetLeases(t *testing.T) {
	cfg := testConfig()
	s := NewModelProbeScheduler(cfg)
	s.TryAcquireAccountLease(1)
	s.TryAcquireAccountLease(2)
	s.TryAcquireAccountLease(3)

	s.ResetLeases()

	s.mu.Lock()
	if len(s.accountLeases) != 0 {
		t.Errorf("expected 0 leases after reset, got %d", len(s.accountLeases))
	}
	s.mu.Unlock()

	if !s.TryAcquireAccountLease(1) {
		t.Error("acquire after reset should succeed")
	}
}

func TestModelProbeScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	cfg.ModelAvailabilityProbeEnabled = true
	cfg.ModelAvailabilityProbeIntervalMs = 60_000
	s := NewModelProbeScheduler(cfg)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

// =============================================================================
// §8  LogCleanupScheduler Tests
// =============================================================================

func TestNewLogCleanupScheduler(t *testing.T) {
	cfg := testConfig()
	cfg.LogCleanupCron = "0 0 6 * * *"
	s := NewLogCleanupScheduler(cfg)
	if s == nil {
		t.Fatal("NewLogCleanupScheduler returned nil")
	}
	if s.Name() != "log-cleanup" {
		t.Errorf("Name() = %q, want log-cleanup", s.Name())
	}
}

func TestLogCleanupScheduler_UpdateSettings(t *testing.T) {
	cfg := testConfig()
	cfg.LogCleanupCron = "0 0 6 * * *"
	s := NewLogCleanupScheduler(cfg)

	t.Run("invalid cron", func(t *testing.T) {
		err := s.UpdateSettings("not-a-cron", true, true, 30)
		if err == nil {
			t.Error("expected error for invalid cron")
		}
	})

	t.Run("valid update", func(t *testing.T) {
		err := s.UpdateSettings("0 0 3 * * *", true, false, 90)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if cfg.LogCleanupCron != "0 0 3 * * *" {
			t.Errorf("cron not updated, got %q", cfg.LogCleanupCron)
		}
		if !cfg.LogCleanupUsageLogsEnabled {
			t.Error("usage logs should be enabled")
		}
		if cfg.LogCleanupProgramLogsEnabled {
			t.Error("program logs should be disabled")
		}
		if cfg.LogCleanupRetentionDays != 90 {
			t.Errorf("retention_days = %d, want 90", cfg.LogCleanupRetentionDays)
		}
	})

	t.Run("clamp retention days low", func(t *testing.T) {
		s.UpdateSettings("0 0 3 * * *", true, true, 0)
		if cfg.LogCleanupRetentionDays != 1 {
			t.Errorf("retention_days should be clamped to 1, got %d", cfg.LogCleanupRetentionDays)
		}
	})

	t.Run("clamp retention days high", func(t *testing.T) {
		s.UpdateSettings("0 0 3 * * *", true, true, 5000)
		if cfg.LogCleanupRetentionDays != 3650 {
			t.Errorf("retention_days should be clamped to 3650, got %d", cfg.LogCleanupRetentionDays)
		}
	})
}

// =============================================================================
// §9  DailySummaryScheduler Tests
// =============================================================================

func TestNewDailySummaryScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewDailySummaryScheduler(cfg)
	if s == nil {
		t.Fatal("NewDailySummaryScheduler returned nil")
	}
	if s.Name() != "daily-summary" {
		t.Errorf("Name() = %q, want daily-summary", s.Name())
	}
}

func TestDailySummaryScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	s := NewDailySummaryScheduler(cfg)
	err := s.Stop()
	if err != nil {
		t.Errorf("Stop on empty cronRunner returned error: %v", err)
	}
}

// =============================================================================
// §10 UsageAggregationScheduler Tests
// =============================================================================

func TestNewUsageAggregationScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewUsageAggregationScheduler(cfg)
	if s == nil {
		t.Fatal("NewUsageAggregationScheduler returned nil")
	}
	if s.Name() != "usage-aggregation" {
		t.Errorf("Name() = %q, want usage-aggregation", s.Name())
	}
}

func TestUsageAggregationScheduler_BuildLeaseOwner(t *testing.T) {
	cfg := testConfig()
	s := NewUsageAggregationScheduler(cfg)
	owner := s.buildLeaseOwner()
	if owner == "" {
		t.Error("expected non-empty lease owner")
	}
	host, _ := os.Hostname()
	expected := fmt.Sprintf("%s:%d", host, os.Getpid())
	if owner != expected {
		t.Errorf("lease owner = %q, want %q", owner, expected)
	}
}

func TestUsageAggregationScheduler_GenerateLeaseToken(t *testing.T) {
	cfg := testConfig()
	s := NewUsageAggregationScheduler(cfg)

	t1 := s.generateLeaseToken()
	t2 := s.generateLeaseToken()

	if t1 == "" {
		t.Error("expected non-empty token")
	}
	if len(t1) != 32 {
		t.Errorf("expected 32-char hex token, got %d chars", len(t1))
	}
	if t1 == t2 {
		t.Error("two tokens should be different")
	}
}

func TestUsageAggregationScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	s := NewUsageAggregationScheduler(cfg)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

func TestProjectionPassResult(t *testing.T) {
	r := &ProjectionPassResult{
		ProcessedLogs: 150,
		WatermarkID:   999,
		Recomputed:    true,
	}
	if r.ProcessedLogs != 150 || r.WatermarkID != 999 || !r.Recomputed {
		t.Errorf("ProjectionPassResult fields incorrect: %+v", r)
	}
}

// =============================================================================
// §11 AdminSnapshotScheduler Tests
// =============================================================================

func TestNewAdminSnapshotScheduler(t *testing.T) {
	cfg := testConfig()
	usage := NewUsageAggregationScheduler(cfg)
	s := NewAdminSnapshotScheduler(cfg, usage)
	if s == nil {
		t.Fatal("NewAdminSnapshotScheduler returned nil")
	}
	if s.Name() != "admin-snapshot" {
		t.Errorf("Name() = %q, want admin-snapshot", s.Name())
	}
	if s.aggregation != usage {
		t.Error("aggregation reference not set")
	}
}

func TestAdminSnapshotScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	s := NewAdminSnapshotScheduler(cfg, nil)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

// =============================================================================
// §12 BackupWebdavConfig Tests
// =============================================================================

func TestIsValidHTTPURL(t *testing.T) {
	tests := []struct {
		url   string
		valid bool
	}{
		{"", false},
		{"   ", false},
		{"ftp://example.com/file", false},
		{"http://", false},
		{"https://", false},
		{"https:///backup.json", false},
		{"http://user:pass@example.com/file", false},
		{"http://example.com/file", true},
		{"https://example.com/file", true},
		{"https://webdav.example.com/backup.json", true},
		{"http://localhost/backup.json", false},
		{"http://127.0.0.1/backup.json", false},
		{"http://[::1]/backup.json", false},
		{"http://10.0.0.1/backup.json", false},
		{"http://169.254.169.254/latest/meta-data", false},
		{"http://[fe80::1]/backup.json", false},
		{"http://224.0.0.1/backup.json", false},
		{"http://0.0.0.0/backup.json", false},
		{"not-a-url", false},
	}

	for _, tc := range tests {
		got := isValidHTTPURL(tc.url)
		if got != tc.valid {
			t.Errorf("isValidHTTPURL(%q) = %v, want %v", tc.url, got, tc.valid)
		}
	}
}

func allowPrivateBackupWebdavTargetsForTest(t *testing.T) {
	t.Helper()
	old := allowPrivateBackupWebdavTargets
	allowPrivateBackupWebdavTargets = true
	t.Cleanup(func() { allowPrivateBackupWebdavTargets = old })
}

func setSchedulerBackupExportLimitsForTest(t *testing.T, maxRows int, maxCellBytes int, maxPayloadBytes int64) {
	t.Helper()
	oldMaxRows := backupsvc.MaxExportRowsPerTable
	oldMaxCellBytes := backupsvc.MaxExportCellBytes
	oldMaxPayloadBytes := backupsvc.MaxExportPayloadBytes
	backupsvc.MaxExportRowsPerTable = maxRows
	backupsvc.MaxExportCellBytes = maxCellBytes
	backupsvc.MaxExportPayloadBytes = maxPayloadBytes
	t.Cleanup(func() {
		backupsvc.MaxExportRowsPerTable = oldMaxRows
		backupsvc.MaxExportCellBytes = oldMaxCellBytes
		backupsvc.MaxExportPayloadBytes = oldMaxPayloadBytes
	})
}

func TestBackupWebdavSchedulerRunExportUploadsRestorableBackupPayload(t *testing.T) {
	allowPrivateBackupWebdavTargetsForTest(t)

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", `"dark"`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}

	previousDB := store.GetDB()
	store.OverrideDB(db)
	t.Cleanup(func() { store.OverrideDB(previousDB) })

	var observedMethod string
	var observedBody string
	var observedPayload map[string]any
	webdav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedMethod = r.Method
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		observedBody = string(data)
		if err := json.Unmarshal(data, &observedPayload); err != nil {
			t.Fatalf("uploaded body is not JSON: %v; body=%s", err, string(data))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(webdav.Close)

	s := NewBackupWebdavScheduler(testConfig())
	s.runExport(&BackupWebdavConfig{
		Enabled:    true,
		FileURL:    webdav.URL + "/backup.json",
		ExportType: "all",
	})

	if observedMethod != http.MethodPut {
		t.Fatalf("method = %q, want PUT", observedMethod)
	}
	if strings.Contains(observedBody, "\n  ") {
		t.Fatalf("uploaded scheduler backup is indented, want compact JSON: %q", observedBody)
	}
	if observedPayload["type"] != "all" {
		t.Fatalf("uploaded type = %v, want all", observedPayload["type"])
	}
	tables, ok := observedPayload["tables"].(map[string]any)
	if !ok {
		t.Fatalf("uploaded tables = %#v, want object", observedPayload["tables"])
	}
	settingsRows, ok := tables["settings"].([]any)
	if !ok || len(settingsRows) == 0 {
		t.Fatalf("uploaded settings rows = %#v, want non-empty array", tables["settings"])
	}
	foundTheme := false
	for _, row := range settingsRows {
		obj, ok := row.(map[string]any)
		if ok && obj["key"] == "theme" {
			foundTheme = true
			break
		}
	}
	if !foundTheme {
		t.Fatalf("uploaded settings rows do not include seeded theme row: %#v", settingsRows)
	}
}

func TestBackupWebdavSchedulerDoesNotUploadOversizedPayload(t *testing.T) {
	allowPrivateBackupWebdavTargetsForTest(t)
	setSchedulerBackupExportLimitsForTest(t, 50_000, 4<<20, 32)

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	previousDB := store.GetDB()
	store.OverrideDB(db)
	t.Cleanup(func() { store.OverrideDB(previousDB) })

	called := false
	webdav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(webdav.Close)

	s := NewBackupWebdavScheduler(testConfig())
	s.runExport(&BackupWebdavConfig{
		Enabled:    true,
		FileURL:    webdav.URL + "/backup.json",
		ExportType: "preferences",
	})

	if called {
		t.Fatal("WebDAV server was called after export payload exceeded limit")
	}

	raw, err := store.NewSettingsStore(db).Get(backupWebdavStateSettingKey)
	if err != nil {
		t.Fatalf("read backup state: %v", err)
	}
	if !strings.Contains(raw, "备份导出超过") {
		t.Fatalf("state = %s, want export limit error", raw)
	}
}

func TestBackupWebdavHTTPClientRejectsHTTPSDowngradeRedirect(t *testing.T) {
	client := newBackupWebdavHTTPClient()
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect is nil, want downgrade protection")
	}

	from := httptest.NewRequest(http.MethodPut, "https://webdav.example.com/backup.json", nil)
	to := httptest.NewRequest(http.MethodGet, "http://webdav.example.com/backup.json", nil)

	if err := client.CheckRedirect(to, []*http.Request{from}); err == nil {
		t.Fatal("HTTPS-to-HTTP redirect was allowed, want rejection")
	}
}

func TestBackupWebdavHTTPClientRejectsPrivateRedirectTarget(t *testing.T) {
	client := newBackupWebdavHTTPClient()
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect is nil, want redirect validation")
	}

	from := httptest.NewRequest(http.MethodPut, "https://webdav.example.com/backup.json", nil)
	to := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/backup.json", nil)

	if err := client.CheckRedirect(to, []*http.Request{from}); err == nil {
		t.Fatal("redirect to loopback was allowed, want rejection")
	}
}

func TestBackupWebdavHTTPTransportDoesNotUseEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")

	transport := newBackupWebdavHTTPTransport()
	if transport.Proxy != nil {
		t.Fatal("backup WebDAV transport uses an environment proxy hook, want direct dial with SSRF checks")
	}
}

func TestBackupWebdavSchedulerFailurePreservesLastSuccessfulSync(t *testing.T) {
	allowPrivateBackupWebdavTargetsForTest(t)

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", `"dark"`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}

	settingsStore := store.NewSettingsStore(db)
	const previousSuccess = "2026-07-01T00:00:00Z"
	if err := settingsStore.Set(backupWebdavStateSettingKey, `{"lastSyncAt":"`+previousSuccess+`","lastAttemptAt":"2026-07-01T00:00:00Z","lastError":null}`); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	previousDB := store.GetDB()
	store.OverrideDB(db)
	t.Cleanup(func() { store.OverrideDB(previousDB) })

	webdav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upload failed"))
	}))
	t.Cleanup(webdav.Close)

	s := NewBackupWebdavScheduler(testConfig())
	s.runExport(&BackupWebdavConfig{
		Enabled:    true,
		FileURL:    webdav.URL + "/backup.json",
		ExportType: "preferences",
	})

	raw, err := settingsStore.Get(backupWebdavStateSettingKey)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var saved struct {
		LastSyncAt    string  `json:"lastSyncAt"`
		LastAttemptAt string  `json:"lastAttemptAt"`
		LastError     *string `json:"lastError"`
	}
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if saved.LastSyncAt != previousSuccess {
		t.Fatalf("lastSyncAt = %q, want previous success %q", saved.LastSyncAt, previousSuccess)
	}
	if saved.LastAttemptAt == "" || saved.LastAttemptAt == previousSuccess {
		t.Fatalf("lastAttemptAt = %q, want fresh failed attempt", saved.LastAttemptAt)
	}
	if saved.LastError == nil || !strings.Contains(*saved.LastError, "HTTP 500") {
		t.Fatalf("lastError = %v, want HTTP 500 error", saved.LastError)
	}
}

func TestValidateBackupWebdavConfig(t *testing.T) {
	t.Run("empty config", func(t *testing.T) {
		// ExportType validation applies regardless of Enabled status.
		// An empty ExportType ("") is not in the allowed set.
		err := validateBackupWebdavConfig(&BackupWebdavConfig{})
		if err == nil {
			t.Error("empty ExportType should be invalid")
		}
	})

	t.Run("disabled with valid export type", func(t *testing.T) {
		err := validateBackupWebdavConfig(&BackupWebdavConfig{
			Enabled:    false,
			ExportType: "all",
		})
		if err != nil {
			t.Errorf("disabled config with valid export type should be valid, got: %v", err)
		}
	})

	t.Run("enabled with valid URL", func(t *testing.T) {
		err := validateBackupWebdavConfig(&BackupWebdavConfig{
			Enabled:    true,
			FileURL:    "https://webdav.example.com/backup.json",
			ExportType: "all",
		})
		if err != nil {
			t.Errorf("valid config should not error, got: %v", err)
		}
	})

	t.Run("enabled with invalid URL", func(t *testing.T) {
		err := validateBackupWebdavConfig(&BackupWebdavConfig{
			Enabled: true,
			FileURL: "not-a-url",
		})
		if err == nil {
			t.Error("expected error for invalid URL")
		}
	})

	t.Run("enabled with empty URL", func(t *testing.T) {
		err := validateBackupWebdavConfig(&BackupWebdavConfig{
			Enabled: true,
			FileURL: "",
		})
		if err == nil {
			t.Error("expected error for empty URL when enabled")
		}
	})

	t.Run("invalid export type", func(t *testing.T) {
		err := validateBackupWebdavConfig(&BackupWebdavConfig{
			Enabled:    true,
			FileURL:    "https://example.com/file",
			ExportType: "invalid-type",
		})
		if err == nil {
			t.Error("expected error for invalid export type")
		}
	})

	t.Run("valid export types", func(t *testing.T) {
		for _, exportType := range []string{"all", "accounts", "preferences"} {
			err := validateBackupWebdavConfig(&BackupWebdavConfig{
				Enabled:    true,
				FileURL:    "https://example.com/file",
				ExportType: exportType,
			})
			if err != nil {
				t.Errorf("export type %q should be valid, got: %v", exportType, err)
			}
		}
	})

	t.Run("auto-sync requires enabled", func(t *testing.T) {
		err := validateBackupWebdavConfig(&BackupWebdavConfig{
			Enabled:         false,
			AutoSyncEnabled: true,
			FileURL:         "https://example.com/file",
			ExportType:      "all",
		})
		if err == nil {
			t.Error("expected error: auto-sync requires enabled")
		}
	})
}

func TestNewBackupWebdavScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewBackupWebdavScheduler(cfg)
	if s == nil {
		t.Fatal("NewBackupWebdavScheduler returned nil")
	}
	if s.Name() != "backup-webdav" {
		t.Errorf("Name() = %q, want backup-webdav", s.Name())
	}
}

// =============================================================================
// §13 OAuthLoopbackScheduler Tests
// =============================================================================

func TestNewOAuthLoopbackScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewOAuthLoopbackScheduler(cfg)
	if s == nil {
		t.Fatal("NewOAuthLoopbackScheduler returned nil")
	}
	if s.Name() != "oauth-loopback" {
		t.Errorf("Name() = %q, want oauth-loopback", s.Name())
	}
}

func TestIntToStr(t *testing.T) {
	tests := []struct {
		n        int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{42, "42"},
		{100, "100"},
		{9844, "9844"},
		{9845, "9845"},
		{9846, "9846"},
		{65535, "65535"},
		{-1, "-1"},
		{-100, "-100"},
	}

	for _, tc := range tests {
		got := intToStr(tc.n)
		if got != tc.expected {
			t.Errorf("intToStr(%d) = %q, want %q", tc.n, got, tc.expected)
		}
	}
}

func TestOAuthLoopbackScheduler_Stop_NoPanic(t *testing.T) {
	cfg := testConfig()
	s := NewOAuthLoopbackScheduler(cfg)
	err := s.Stop()
	if err != nil {
		t.Errorf("Stop before Start returned error: %v", err)
	}
}

// =============================================================================
// §14 ProxyFileRetentionScheduler Tests
// =============================================================================

func TestNewProxyFileRetentionScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewProxyFileRetentionScheduler(cfg)
	if s == nil {
		t.Fatal("NewProxyFileRetentionScheduler returned nil")
	}
	if s.Name() != "proxy-file-retention" {
		t.Errorf("Name() = %q, want proxy-file-retention", s.Name())
	}
}

func TestProxyFileRetentionScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	cfg.ProxyFileRetentionDays = 30
	cfg.ProxyFileRetentionPruneIntervalMinutes = 60
	s := NewProxyFileRetentionScheduler(cfg)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

// =============================================================================
// §15 ProxyLogRetentionScheduler Tests
// =============================================================================

func TestNewProxyLogRetentionScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewProxyLogRetentionScheduler(cfg)
	if s == nil {
		t.Fatal("NewProxyLogRetentionScheduler returned nil")
	}
	if s.Name() != "proxy-log-retention" {
		t.Errorf("Name() = %q, want proxy-log-retention", s.Name())
	}
}

func TestProxyLogRetentionScheduler_DisabledByLogCleanup(t *testing.T) {
	cfg := testConfig()
	cfg.LogCleanupConfigured = true
	cfg.ProxyLogRetentionDays = 30
	s := NewProxyLogRetentionScheduler(cfg)
	ctx := context.Background()
	err := s.Start(ctx)
	if err != nil {
		t.Errorf("Start returned error: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	s.mu.Lock()
	started := s.running
	s.mu.Unlock()
	if started {
		t.Error("scheduler should be disabled when logCleanup is configured")
	}
}

func TestProxyLogRetentionScheduler_DisabledByZeroDays(t *testing.T) {
	cfg := testConfig()
	cfg.LogCleanupConfigured = false
	cfg.ProxyLogRetentionDays = 0
	s := NewProxyLogRetentionScheduler(cfg)
	ctx := context.Background()
	_ = s.Start(ctx)
	s.mu.Lock()
	started := s.running
	s.mu.Unlock()
	if started {
		t.Error("scheduler should be disabled when retention_days=0")
	}
}

func TestProxyLogRetentionScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	cfg.LogCleanupConfigured = false
	cfg.ProxyLogRetentionDays = 30
	cfg.ProxyLogRetentionPruneIntervalMinutes = 30
	s := NewProxyLogRetentionScheduler(cfg)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

// =============================================================================
// §16 Sub2APIRefreshScheduler Tests
// =============================================================================

func TestNewSub2APIRefreshScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewSub2APIRefreshScheduler(cfg)
	if s == nil {
		t.Fatal("NewSub2APIRefreshScheduler returned nil")
	}
	if s.Name() != "sub2api-refresh" {
		t.Errorf("Name() = %q, want sub2api-refresh", s.Name())
	}
}

func TestSub2APIRefreshScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	s := NewSub2APIRefreshScheduler(cfg)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

func TestSub2ApiRefreshResult(t *testing.T) {
	r := Sub2ApiRefreshResult{
		Scanned:             10,
		Refreshed:           8,
		Failed:              2,
		Skipped:             0,
		RefreshedAccountIDs: []int64{1, 2, 3},
		FailedAccountIDs:    []int64{4, 5},
	}
	if r.Scanned != 10 || r.Refreshed != 8 || r.Failed != 2 {
		t.Errorf("Sub2ApiRefreshResult fields incorrect: %+v", r)
	}
	if len(r.RefreshedAccountIDs) != 3 || len(r.FailedAccountIDs) != 2 {
		t.Errorf("account IDs incorrect")
	}
}

// =============================================================================
// §17 UpdateCenterScheduler Tests
// =============================================================================

func TestNewUpdateCenterScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewUpdateCenterScheduler(cfg)
	if s == nil {
		t.Fatal("NewUpdateCenterScheduler returned nil")
	}
	if s.Name() != "update-center" {
		t.Errorf("Name() = %q, want update-center", s.Name())
	}
}

func TestUpdateCenterScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	s := NewUpdateCenterScheduler(cfg)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

// =============================================================================
// §18 SiteAnnouncementScheduler Tests
// =============================================================================

func TestNewSiteAnnouncementScheduler(t *testing.T) {
	cfg := testConfig()
	s := NewSiteAnnouncementScheduler(cfg)
	if s == nil {
		t.Fatal("NewSiteAnnouncementScheduler returned nil")
	}
	if s.Name() != "site-announcement" {
		t.Errorf("Name() = %q, want site-announcement", s.Name())
	}
}

func TestSiteAnnouncementScheduler_Stop(t *testing.T) {
	cfg := testConfig()
	s := NewSiteAnnouncementScheduler(cfg)
	_ = s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	err = s.Stop()
	if err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

// =============================================================================
// §19 Scheduler Interface Compliance
// =============================================================================

func TestAllSchedulersImplementInterface(t *testing.T) {
	cfg := testConfig()
	usage := NewUsageAggregationScheduler(cfg)

	schedulers := []Scheduler{
		NewCheckinScheduler(cfg),
		NewBalanceScheduler(cfg, nil),
		NewChannelRecoveryScheduler(cfg),
		NewModelProbeScheduler(cfg),
		NewLogCleanupScheduler(cfg),
		NewDailySummaryScheduler(cfg),
		NewUsageAggregationScheduler(cfg),
		NewAdminSnapshotScheduler(cfg, usage),
		NewBackupWebdavScheduler(cfg),
		NewOAuthLoopbackScheduler(cfg),
		NewProxyFileRetentionScheduler(cfg),
		NewProxyLogRetentionScheduler(cfg),
		NewSub2APIRefreshScheduler(cfg),
		NewUpdateCenterScheduler(cfg),
		NewSiteAnnouncementScheduler(cfg),
	}

	names := make(map[string]bool)
	for _, sch := range schedulers {
		name := sch.Name()
		if name == "" {
			t.Errorf("scheduler %T has empty Name()", sch)
		}
		if names[name] {
			t.Errorf("duplicate scheduler name: %q", name)
		}
		names[name] = true
	}

	expectedNames := []string{
		"checkin", "balance-refresh", "channel-recovery", "model-probe",
		"log-cleanup", "daily-summary", "usage-aggregation", "admin-snapshot",
		"backup-webdav", "oauth-loopback", "proxy-file-retention",
		"proxy-log-retention", "sub2api-refresh", "update-center", "site-announcement",
	}

	for _, exp := range expectedNames {
		if !names[exp] {
			t.Errorf("missing scheduler name: %q", exp)
		}
	}
}

// =============================================================================
// §20 Mock Helpers
// =============================================================================

// testConfig returns a Config with safe defaults for testing.
func testConfig() *config.Config {
	return &config.Config{
		CheckinCron:                            "0 0 */6 * * *",
		CheckinScheduleMode:                    "cron",
		CheckinIntervalHours:                   6,
		BalanceRefreshCron:                     "0 0 0 * * *",
		LogCleanupCron:                         "0 0 6 * * *",
		LogCleanupRetentionDays:                90,
		LogCleanupConfigured:                   false,
		LogCleanupUsageLogsEnabled:             true,
		LogCleanupProgramLogsEnabled:           true,
		ModelAvailabilityProbeEnabled:          false,
		ModelAvailabilityProbeIntervalMs:       120_000,
		ModelAvailabilityProbeConcurrency:      4,
		ProxyLogRetentionDays:                  90,
		ProxyLogRetentionPruneIntervalMinutes:  30,
		ProxyFileRetentionDays:                 90,
		ProxyFileRetentionPruneIntervalMinutes: 60,
		ProxyFirstByteTimeoutSec:               30,
	}
}

// mockScheduler implements Scheduler for testing the Registry.
type mockScheduler struct {
	name    string
	started atomic.Bool
	stopped atomic.Bool
}

func newMockScheduler(name string) *mockScheduler {
	return &mockScheduler{name: name}
}

func (m *mockScheduler) Name() string                    { return m.name }
func (m *mockScheduler) Start(ctx context.Context) error { m.started.Store(true); return nil }
func (m *mockScheduler) Stop() error                     { m.stopped.Store(true); return nil }

// panicScheduler panics on Start to test panic recovery in Registry.StartAll.
type panicScheduler struct{}

func (p *panicScheduler) Name() string                    { return "panic-scheduler" }
func (p *panicScheduler) Start(ctx context.Context) error { panic("simulated panic on start") }
func (p *panicScheduler) Stop() error                     { return nil }

// errorStopScheduler returns an error on Stop to test error tolerance in Registry.StopAll.
type errorStopScheduler struct {
	name string
}

func (e *errorStopScheduler) Name() string                    { return e.name }
func (e *errorStopScheduler) Start(ctx context.Context) error { return nil }
func (e *errorStopScheduler) Stop() error                     { return fmt.Errorf("simulated stop error") }
