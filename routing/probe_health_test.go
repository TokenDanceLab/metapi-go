package routing

import (
	"context"
	"testing"
)

// =============================================================================
// #114 — Background channel health probe success/failure transitions
// =============================================================================

func TestRecordProbeSuccess_ClearsCooldownAndStampsProbeStatus(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	db := newIsolationDB()
	until := "2099-01-01T00:00:00Z"
	lastFail := "2020-01-01T00:00:00Z"
	ch := isolationChannel(101, 1, 11)
	ch.FailCount = 3
	ch.ConsecutiveFailCount = 2
	ch.CooldownLevel = 1
	ch.CooldownUntil = &until
	ch.LastFailAt = &lastFail
	account := isolationAccount(11, 7)
	route := isolationRoute(1, "weighted")
	db.seedChannel(ch, account, route)
	db.credentialScope[101] = []int64{101, 102}

	// Sibling also cooling — success should clear credential-scoped siblings.
	sibling := isolationChannel(102, 1, 11)
	sibling.CooldownUntil = &until
	sibling.LastFailAt = &lastFail
	sibling.ConsecutiveFailCount = 1
	sibling.CooldownLevel = 1
	db.seedChannel(sibling, account, route)

	tr := newIsolationRouter(db)
	model := "gpt-test"
	if err := tr.RecordProbeSuccess(context.Background(), 101, 120.0, &model, nil); err != nil {
		t.Fatalf("RecordProbeSuccess: %v", err)
	}

	got := db.getChannel(101)
	if got == nil {
		t.Fatal("channel 101 missing")
	}
	if got.CooldownUntil != nil || got.LastFailAt != nil || got.ConsecutiveFailCount != 0 || got.CooldownLevel != 0 {
		t.Fatalf("probe success did not clear channel cooldown: %+v", got)
	}
	sib := db.getChannel(102)
	if sib == nil || sib.CooldownUntil != nil || sib.ConsecutiveFailCount != 0 {
		t.Fatalf("probe success did not clear credential sibling: %+v", sib)
	}

	// Runtime health success + probe stamp.
	status := GetSiteProbeStatus(7)
	if status.Status != "success" {
		t.Fatalf("probe status = %q, want success", status.Status)
	}
	if status.AtMs <= 0 {
		t.Fatal("expected LastProbeAtMs stamped")
	}
	if status.ModelName != "gpt-test" {
		t.Fatalf("model = %q", status.ModelName)
	}
	if status.ChannelID == nil || *status.ChannelID != 101 {
		t.Fatalf("channel id stamp = %v", status.ChannelID)
	}
	if status.LatencyMs == nil || *status.LatencyMs != 120 {
		t.Fatalf("latency stamp = %v", status.LatencyMs)
	}

	// Success counter path reduced penalty / set last success.
	details := GetSiteRuntimeHealthDetails(7, "gpt-test")
	if details.RecentSuccessRate <= 0 {
		t.Fatalf("expected positive recent success rate, got %+v", details)
	}
}

func TestRecordProbeFailure_CoolsOnlyProbedChannelAndUpdatesHealth(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	db := newIsolationDB()
	ch := isolationChannel(201, 2, 21)
	sibling := isolationChannel(202, 2, 21)
	account := isolationAccount(21, 8)
	route := isolationRoute(2, "weighted")
	db.seedChannel(ch, account, route)
	db.seedChannel(sibling, account, route)
	// Even if credential scope would expand for usage-limit traffic, probe
	// failures must stay single-channel.
	db.credentialScope[201] = []int64{201, 202}

	tr := newIsolationRouter(db)
	status502 := 502
	errText := "bad gateway"
	model := "gpt-test"
	if err := tr.RecordProbeFailure(context.Background(), 201, SiteRuntimeFailureContext{
		Status:    &status502,
		ErrorText: &errText,
		ModelName: &model,
	}, nil); err != nil {
		t.Fatalf("RecordProbeFailure: %v", err)
	}

	failed := db.getChannel(201)
	if failed == nil {
		t.Fatal("channel 201 missing")
	}
	if failed.FailCount != 1 {
		t.Fatalf("failCount = %d, want 1", failed.FailCount)
	}
	if failed.CooldownUntil == nil {
		t.Fatal("expected cooldownUntil set on probed channel")
	}
	if failed.LastFailAt == nil {
		t.Fatal("expected lastFailAt set")
	}

	clean := db.getChannel(202)
	if clean == nil {
		t.Fatal("sibling missing")
	}
	if clean.FailCount != 0 || clean.CooldownUntil != nil || clean.LastFailAt != nil {
		t.Fatalf("probe failure cascaded to sibling: %+v", clean)
	}
	if db.cooldownCalls != 1 || len(db.lastCooldownIDs) != 1 || db.lastCooldownIDs[0] != 201 {
		t.Fatalf("cooldown write scope = ids %v calls %d", db.lastCooldownIDs, db.cooldownCalls)
	}

	probe := GetSiteProbeStatus(8)
	if probe.Status != "failure" {
		t.Fatalf("probe status = %q, want failure", probe.Status)
	}
	if probe.ErrorText == nil || *probe.ErrorText != "bad gateway" {
		t.Fatalf("probe error = %v", probe.ErrorText)
	}

	// Transient failure feeds runtime health (penalty / streak) without expiry.
	details := GetSiteRuntimeHealthDetails(8, "gpt-test")
	if details.GlobalMultiplier >= 1 {
		t.Fatalf("expected health penalty after probe failure, details=%+v", details)
	}
}

func TestRecordProbeFailure_AuthLookingErrorDoesNotCascadeCredentialScope(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	db := newIsolationDB()
	ch := isolationChannel(301, 3, 31)
	sibling := isolationChannel(302, 3, 31)
	account := isolationAccount(31, 9)
	route := isolationRoute(3, "weighted")
	db.seedChannel(ch, account, route)
	db.seedChannel(sibling, account, route)
	db.credentialScope[301] = []int64{301, 302}

	tr := newIsolationRouter(db)
	status401 := 401
	errText := "invalid api key" // must NOT mark expired; probe path never touches account status
	model := "gpt-test"
	if err := tr.RecordProbeFailure(context.Background(), 301, SiteRuntimeFailureContext{
		Status:    &status401,
		ErrorText: &errText,
		ModelName: &model,
	}, nil); err != nil {
		t.Fatalf("RecordProbeFailure: %v", err)
	}

	// Only probed channel cooled; no credential cascade (unlike short-window usage limit).
	if got := db.getChannel(302); got.FailCount != 0 || got.CooldownUntil != nil {
		t.Fatalf("auth-looking probe failure cascaded: %+v", got)
	}
	// Account row is not mutated by isolationDB (and production path never writes account.status).
	if account.Status != "active" {
		t.Fatalf("account status mutated to %q", account.Status)
	}

	probe := GetSiteProbeStatus(9)
	if probe.Status != "failure" {
		t.Fatalf("status=%q", probe.Status)
	}
}

func TestRecordProbeFailure_OpensBreakerAfterTransientStreak(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	db := newIsolationDB()
	ch := isolationChannel(401, 4, 41)
	account := isolationAccount(41, 10)
	route := isolationRoute(4, "weighted")
	db.seedChannel(ch, account, route)

	tr := newIsolationRouter(db)
	status503 := 503
	errText := "service unavailable"
	model := "gpt-test"

	for i := 0; i < 3; i++ {
		if err := tr.RecordProbeFailure(context.Background(), 401, SiteRuntimeFailureContext{
			Status:    &status503,
			ErrorText: &errText,
			ModelName: &model,
		}, nil); err != nil {
			t.Fatalf("RecordProbeFailure #%d: %v", i+1, err)
		}
	}

	if !IsSiteRuntimeBreakerOpen(10) {
		t.Fatal("expected site breaker open after 3 transient probe failures")
	}
	probe := GetSiteProbeStatus(10)
	if probe.Status != "failure" || !probe.BreakerOpen {
		t.Fatalf("probe status after streak: %+v", probe)
	}
}

func TestRecordSiteProbeOutcome_InconclusiveDoesNotChangeSuccessRateAlone(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	model := "m"
	channelID := int64(1)
	// Stamp only — no success/failure counters.
	RecordSiteProbeOutcome(55, "inconclusive", 0, &model, &channelID, nil)
	status := GetSiteProbeStatus(55)
	if status.Status != "inconclusive" {
		t.Fatalf("status=%q", status.Status)
	}
	details := GetSiteRuntimeHealthDetails(55, "m")
	// With no outcomes, prior is 0.5 success rate and low confidence.
	if details.RecentSampleCount != 0 {
		t.Fatalf("inconclusive stamp should not invent samples, got %v", details.RecentSampleCount)
	}
}
