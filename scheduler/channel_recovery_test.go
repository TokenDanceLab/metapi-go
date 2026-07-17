package scheduler

import (
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

func TestSetActiveChannelIDsProvider(t *testing.T) {
	t.Cleanup(func() { SetActiveChannelIDsProvider(nil) })

	if GetActiveChannelIDsFromProvider() != nil {
		t.Fatal("expected nil IDs when provider unset")
	}

	SetActiveChannelIDsProvider(func() []int64 { return []int64{7, 9} })
	got := GetActiveChannelIDsFromProvider()
	if len(got) != 2 || got[0] != 7 || got[1] != 9 {
		t.Fatalf("provider IDs = %v, want [7 9]", got)
	}

	SetActiveChannelIDsProvider(nil)
	if GetActiveChannelIDsFromProvider() != nil {
		t.Fatal("expected nil after clearing provider")
	}
}

func TestLoadActiveCandidates_ProviderUsesIDs(t *testing.T) {
	db := openChannelRecoveryTestDB(t)
	s := NewChannelRecoveryScheduler(testConfig())

	now := time.Now().UTC()
	idA := seedRecoveryChannel(t, db, "provider-a", "gpt-a", true, nil)
	idB := seedRecoveryChannel(t, db, "provider-b", "gpt-b", true, nil)
	idC := seedRecoveryChannel(t, db, "provider-c", "gpt-c", true, nil)
	// Cooldown channel with source_model should still resolve from provider IDs.
	cooldownUntil := now.Add(10 * time.Minute).Format(time.RFC3339)
	idCooldown := seedRecoveryChannel(t, db, "provider-cd", "gpt-cd", true, &cooldownUntil)
	// Disabled channel should be ignored even if provider lists it.
	idDisabled := seedRecoveryChannel(t, db, "provider-off", "gpt-off", false, nil)

	t.Cleanup(func() { SetActiveChannelIDsProvider(nil) })
	SetActiveChannelIDsProvider(func() []int64 {
		return []int64{idB, idA, idCooldown, idDisabled, idC + 999}
	})

	got := s.loadActiveCandidates(db)
	if len(got) != 3 {
		t.Fatalf("candidates = %+v, want 3 (B,A,cooldown)", got)
	}
	if got[0].channelID != idB || got[0].modelName != "gpt-b" || got[0].source != "active" {
		t.Fatalf("first candidate = %+v, want channel %d gpt-b active", got[0], idB)
	}
	if got[1].channelID != idA || got[1].modelName != "gpt-a" {
		t.Fatalf("second candidate = %+v, want channel %d gpt-a", got[1], idA)
	}
	if got[2].channelID != idCooldown || got[2].modelName != "gpt-cd" {
		t.Fatalf("third candidate = %+v, want cooldown channel %d gpt-cd", got[2], idCooldown)
	}

	// Empty provider result must not fall through to residual SQL LIMIT 50.
	SetActiveChannelIDsProvider(func() []int64 { return []int64{} })
	if got := s.loadActiveCandidates(db); len(got) != 0 {
		t.Fatalf("empty provider should yield 0 candidates, got %+v", got)
	}
	_ = idC
}

func TestLoadActiveCandidates_NilProviderSQLFallback(t *testing.T) {
	db := openChannelRecoveryTestDB(t)
	s := NewChannelRecoveryScheduler(testConfig())
	t.Cleanup(func() { SetActiveChannelIDsProvider(nil) })
	SetActiveChannelIDsProvider(nil)

	now := time.Now().UTC()
	idActive := seedRecoveryChannel(t, db, "sql-active", "gpt-sql", true, nil)
	cooldownUntil := now.Add(5 * time.Minute).Format(time.RFC3339)
	_ = seedRecoveryChannel(t, db, "sql-cd", "gpt-cd", true, &cooldownUntil)
	_ = seedRecoveryChannel(t, db, "sql-off", "gpt-off", false, nil)

	got := s.loadActiveCandidates(db)
	if len(got) != 1 {
		t.Fatalf("SQL fallback candidates = %+v, want only null-cooldown enabled channel", got)
	}
	if got[0].channelID != idActive || got[0].modelName != "gpt-sql" || got[0].source != "active" {
		t.Fatalf("SQL fallback candidate = %+v, want channel %d gpt-sql active", got[0], idActive)
	}
}

func openChannelRecoveryTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedRecoveryChannel(t *testing.T, db *store.DB, suffix, model string, enabled bool, cooldownUntil *string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"site-"+suffix, "https://example.invalid/"+suffix, "anyrouter", "active", now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("site id: %v", err)
	}
	res, err = db.Exec(
		`INSERT INTO accounts (site_id, access_token, api_token, status, balance, quota, value_score, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		siteID, "tok-"+suffix, "tok-"+suffix, "active", 10.0, 100.0, 1.0, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("account id: %v", err)
	}
	res, err = db.Exec(
		`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		model, "pattern", "weighted", true, now, now,
	)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("route id: %v", err)
	}
	res, err = db.Exec(
		`INSERT INTO route_channels (route_id, account_id, source_model, priority, weight, enabled, cooldown_until)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		routeID, accountID, model, 0, 10, enabled, cooldownUntil,
	)
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	channelID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("channel id: %v", err)
	}
	if channelID <= 0 {
		t.Fatalf("unexpected channel id %d for %s", channelID, suffix)
	}
	return channelID
}
