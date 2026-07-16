package scheduler

import (
	"context"
	"testing"

	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

type fakeProbeRecorder struct {
	successes []int64
	failures  []int64
}

func (f *fakeProbeRecorder) RecordProbeSuccess(ctx context.Context, channelID int64, latencyMs float64, modelName *string, actualAccountID *int64) error {
	f.successes = append(f.successes, channelID)
	return nil
}

func (f *fakeProbeRecorder) RecordFailure(ctx context.Context, channelID int64, failureCtx routing.SiteRuntimeFailureContext, actualAccountID *int64) error {
	f.failures = append(f.failures, channelID)
	return nil
}

func TestApplyProbeOutcome_RecordsSuccessAndFailure(t *testing.T) {
	rec := &fakeProbeRecorder{}
	ApplyProbeOutcome(context.Background(), nil, rec, ProbeChannelOutcome{
		ChannelID: 7, AccountID: 1, SiteID: 2, ModelName: "gpt-4o", OK: true, LatencyMs: 120,
	})
	ApplyProbeOutcome(context.Background(), nil, rec, ProbeChannelOutcome{
		ChannelID: 8, AccountID: 1, SiteID: 2, ModelName: "gpt-4o", OK: false, LatencyMs: 0, ErrorText: "timeout",
	})
	if len(rec.successes) != 1 || rec.successes[0] != 7 {
		t.Fatalf("successes=%v", rec.successes)
	}
	if len(rec.failures) != 1 || rec.failures[0] != 8 {
		t.Fatalf("failures=%v", rec.failures)
	}
}

func TestApplyProbeOutcome_UpsertsModelAvailability(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	now := "2026-07-17T00:00:00Z"
	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES ('s','https://s.test','openai','active',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	siteID, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, created_at, updated_at) VALUES (?,?,?, 'active', ?, ?)`, siteID, "u", "tok", now, now)
	if err != nil {
		t.Fatal(err)
	}
	accountID, _ := res.LastInsertId()

	ApplyProbeOutcome(context.Background(), db, nil, ProbeChannelOutcome{
		AccountID: accountID, SiteID: siteID, ModelName: "gpt-4o", OK: true, LatencyMs: 200,
	})
	var available bool
	var latency int64
	if err := db.QueryRow(`SELECT available, COALESCE(latency_ms,0) FROM model_availability WHERE account_id = ? AND model_name = ?`, accountID, "gpt-4o").Scan(&available, &latency); err != nil {
		t.Fatal(err)
	}
	if !available || latency != 200 {
		t.Fatalf("available=%v latency=%d", available, latency)
	}

	ApplyProbeOutcome(context.Background(), db, nil, ProbeChannelOutcome{
		AccountID: accountID, SiteID: siteID, ModelName: "gpt-4o", OK: false, LatencyMs: 0,
	})
	if err := db.QueryRow(`SELECT available FROM model_availability WHERE account_id = ? AND model_name = ?`, accountID, "gpt-4o").Scan(&available); err != nil {
		t.Fatal(err)
	}
	if available {
		t.Fatal("expected unavailable after failed probe")
	}
}

func TestModelProbeScheduler_SetProbeRecorder(t *testing.T) {
	s := &ModelProbeScheduler{accountLeases: map[int64]bool{}}
	rec := &fakeProbeRecorder{}
	s.SetProbeRecorder(rec)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probeRecorder == nil {
		t.Fatal("expected recorder")
	}
}
