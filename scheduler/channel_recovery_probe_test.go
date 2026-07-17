package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

func TestChannelRecovery_ProbeCandidate_UsesInjectedProbeAndApplyOutcome(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "RecSite", "https://rec.example.test", "openai", now, now)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	siteID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', 0, ?, ?)`, siteID, "rec-user", "tok", now, now)
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	accountID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		VALUES (?, ?, 'standard', 'weighted', 1, ?, ?)`, "gpt-*", "Rec Route", now, now)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	routeID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO route_channels (route_id, account_id, source_model, priority, weight, enabled)
		VALUES (?, ?, ?, 10, 10, 1)`, routeID, accountID, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	channelID, _ := res.LastInsertId()

	s := NewChannelRecoveryScheduler(testConfig())
	probe := &fakeProbe{outcomes: map[int64]ProbeOutcome{
		channelID: {Status: "success", LatencyMs: 12},
	}}
	rec := &fakeRecorder{}
	s.SetProbeExecutor(probe)
	s.SetHealthRecorder(rec)

	s.probeCandidate(db, recoveryCandidate{
		source:    "cooldown",
		channelID: channelID,
		modelName: "gpt-4o-mini",
	}, time.Now().UnixMilli())

	if got := s.LastProbeStatus(channelID, "gpt-4o-mini"); got != "success" {
		t.Fatalf("last status=%q want success", got)
	}
	if len(probe.calls) != 1 {
		t.Fatalf("probe calls=%d", len(probe.calls))
	}
	if len(rec.successCalls) != 1 || rec.successCalls[0].ChannelID != channelID {
		t.Fatalf("success calls=%+v", rec.successCalls)
	}
}

func TestChannelRecovery_ProbeCandidate_MissingDepsSkipped(t *testing.T) {
	s := NewChannelRecoveryScheduler(testConfig())
	// No DB needed when deps missing — still should mark skipped without panic.
	s.probeCandidate(nil, recoveryCandidate{
		source:    "active",
		channelID: 99,
		modelName: "m",
	}, time.Now().UnixMilli())
	if got := s.LastProbeStatus(99, "m"); got != "skipped" {
		t.Fatalf("status=%q want skipped", got)
	}
}

func TestChannelRecovery_ExecuteCandidateProbe_FailureRecords(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, _ := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES ('S','https://x.test','openai','active',?,?)`, now, now)
	siteID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, 'u', 't', 'active', 0, ?, ?)`, siteID, now, now)
	accountID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		VALUES ('m*','R','standard','weighted',1,?,?)`, now, now)
	routeID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO route_channels (route_id, account_id, source_model, priority, weight, enabled)
		VALUES (?, ?, 'm1', 1, 1, 1)`, routeID, accountID)
	channelID, _ := res.LastInsertId()

	s := NewChannelRecoveryScheduler(testConfig())
	probe := &fakeProbe{outcomes: map[int64]ProbeOutcome{
		channelID: {Status: "failure", HTTPStatus: 503, ErrorText: "down"},
	}}
	rec := &fakeRecorder{}
	status := s.executeCandidateProbe(db, recoveryCandidate{
		source:    "cooldown",
		channelID: channelID,
		modelName: "m1",
	}, probe, rec)
	if status != "failure" {
		t.Fatalf("status=%s", status)
	}
	if len(rec.failureCalls) != 1 {
		t.Fatalf("failure calls=%+v", rec.failureCalls)
	}
}

func TestModelProbeJobs_EnqueueWaitAndAsync(t *testing.T) {
	ResetModelProbeJobsForTest()
	t.Cleanup(ResetModelProbeJobsForTest)

	SetModelProbeTrigger(func() ProbeRunSummary {
		return ProbeRunSummary{Success: 3, CompletedAtMs: time.Now().UnixMilli()}
	})
	t.Cleanup(func() { SetModelProbeTrigger(nil) })

	job := EnqueueModelProbeJob(nil, true)
	if job == nil || job.ID == "" || job.ID == "stub-probe" {
		t.Fatalf("bad job: %+v", job)
	}
	if job.Status != "completed" {
		t.Fatalf("status=%s", job.Status)
	}
	if job.Summary == nil || job.Summary.Success != 3 {
		t.Fatalf("summary=%+v", job.Summary)
	}

	job2 := EnqueueModelProbeJob(nil, false)
	if job2.ID == job.ID {
		t.Fatalf("expected unique job ids")
	}
	// Give async goroutine a moment.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := GetModelProbeJob(job2.ID)
		if got != nil && got.Status == "completed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("async job did not complete: %+v", GetModelProbeJob(job2.ID))
}

func TestModelProbeScheduler_TriggerNow_NoDB(t *testing.T) {
	s := NewModelProbeScheduler(testConfig())
	// No store.GetDB set — should return empty summary without panic.
	sum := s.TriggerNow()
	if sum.AccountsProbed != 0 || sum.TargetsScanned != 0 {
		t.Fatalf("expected empty summary without DB, got %+v", sum)
	}
	_ = context.Background()
}
