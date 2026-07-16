package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

func setupRouteRebuildDB(t *testing.T) *store.DB {
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

func seedSiteAccountToken(t *testing.T, db *store.DB, name string) (siteID, accountID, tokenID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES (?, ?, 'openai', 'active', ?, ?)`,
		name, "https://"+name+".example.com", now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ = res.LastInsertId()
	res, err = db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
		 VALUES (?, ?, 'tok', 'active', 1, ?, ?)`,
		siteID, name+"-user", now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ = res.LastInsertId()
	res, err = db.Exec(
		`INSERT INTO account_tokens (account_id, name, token, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, 'default', 'sk-test', 'ready', 'manual', 1, 1, ?, ?)`,
		accountID, now, now,
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	tokenID, _ = res.LastInsertId()
	return siteID, accountID, tokenID
}

func TestRebuildTokenRoutesFromAvailability_AddsMatchingChannels(t *testing.T) {
	db := setupRouteRebuildDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	_, accountMatch, tokenMatch := seedSiteAccountToken(t, db, "match")
	_, accountOther, tokenOther := seedSiteAccountToken(t, db, "other")

	// Matching availability
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'gpt-4o', 1, ?)`, tokenMatch, now); err != nil {
		t.Fatalf("token avail match: %v", err)
	}
	// Non-matching model
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'claude-3-opus', 1, ?)`, tokenOther, now); err != nil {
		t.Fatalf("token avail other: %v", err)
	}
	// Account-level model availability that should also match
	if _, err := db.Exec(
		`INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at)
		 VALUES (?, 'gpt-4o-mini', 1, 0, ?)`, accountMatch, now); err != nil {
		t.Fatalf("model avail: %v", err)
	}

	res, err := db.Exec(
		`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES ('gpt-*', 'pattern', 'weighted', 1, ?, ?)`, now, now,
	)
	if err != nil {
		t.Fatalf("insert pattern route: %v", err)
	}
	patternRouteID, _ := res.LastInsertId()

	// explicit_group referencing a future exact route — should not get materialised channels
	res, err = db.Exec(
		`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES ('my-group', 'my-group', 'explicit_group', 'weighted', 1, ?, ?)`, now, now,
	)
	if err != nil {
		t.Fatalf("insert group route: %v", err)
	}
	groupRouteID, _ := res.LastInsertId()

	stats, err := RebuildTokenRoutesFromAvailability(context.Background(), db.DB)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if stats.PatternRoutes != 1 {
		t.Fatalf("patternRoutes = %d, want 1", stats.PatternRoutes)
	}
	if stats.GroupRoutes != 1 {
		t.Fatalf("groupRoutes = %d, want 1", stats.GroupRoutes)
	}
	if stats.ChannelsInserted < 2 {
		t.Fatalf("channelsInserted = %d, want >= 2 (token+account availability)", stats.ChannelsInserted)
	}

	var patternCount int
	if err := db.Get(&patternCount, `SELECT COUNT(*) FROM route_channels WHERE route_id = ?`, patternRouteID); err != nil {
		t.Fatalf("count pattern channels: %v", err)
	}
	if patternCount < 2 {
		t.Fatalf("pattern route channels = %d, want >= 2", patternCount)
	}

	// Non-matching model must not appear under gpt-* route
	var claudeCount int
	if err := db.Get(&claudeCount,
		`SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND source_model = 'claude-3-opus'`, patternRouteID); err != nil {
		t.Fatalf("count claude: %v", err)
	}
	if claudeCount != 0 {
		t.Fatalf("non-matching model leaked into pattern route: %d", claudeCount)
	}

	var groupCount int
	if err := db.Get(&groupCount, `SELECT COUNT(*) FROM route_channels WHERE route_id = ?`, groupRouteID); err != nil {
		t.Fatalf("count group channels: %v", err)
	}
	if groupCount != 0 {
		t.Fatalf("explicit_group must not materialize channels, got %d", groupCount)
	}

	// Unrelated account (claude only) should not be attached
	var otherOnPattern int
	if err := db.Get(&otherOnPattern,
		`SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND account_id = ?`, patternRouteID, accountOther); err != nil {
		t.Fatalf("count other: %v", err)
	}
	if otherOnPattern != 0 {
		t.Fatalf("non-matching account attached to pattern route")
	}
}

func TestRebuildTokenRoutesFromAvailability_PreservesManualOverride(t *testing.T) {
	db := setupRouteRebuildDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	_, accountID, tokenID := seedSiteAccountToken(t, db, "manual")

	res, err := db.Exec(
		`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES ('gpt-*', 'pattern', 'weighted', 1, ?, ?)`, now, now,
	)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, _ := res.LastInsertId()

	// Manual channel with a model that will NOT match after rebuild sources change
	if _, err := db.Exec(
		`INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override)
		 VALUES (?, ?, ?, 'manual-only-model', 5, 10, 1, 1)`, routeID, accountID, tokenID); err != nil {
		t.Fatalf("insert manual channel: %v", err)
	}
	// Auto channel that becomes stale (no availability for it)
	if _, err := db.Exec(
		`INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override)
		 VALUES (?, ?, ?, 'stale-auto', 0, 10, 1, 0)`, routeID, accountID, tokenID); err != nil {
		t.Fatalf("insert auto channel: %v", err)
	}
	// Fresh availability
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'gpt-4o', 1, ?)`, tokenID, now); err != nil {
		t.Fatalf("token avail: %v", err)
	}

	stats, err := RebuildTokenRoutesFromAvailability(context.Background(), db.DB)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if stats.ChannelsRemoved < 1 {
		t.Fatalf("expected stale auto channel removed, stats=%+v", stats)
	}

	var manualCount int
	if err := db.Get(&manualCount,
		`SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND source_model = 'manual-only-model' AND manual_override = 1`,
		routeID); err != nil {
		t.Fatalf("count manual: %v", err)
	}
	if manualCount != 1 {
		t.Fatalf("manual channel wiped by rebuild")
	}

	var staleCount int
	if err := db.Get(&staleCount,
		`SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND source_model = 'stale-auto'`, routeID); err != nil {
		t.Fatalf("count stale: %v", err)
	}
	if staleCount != 0 {
		t.Fatalf("stale auto channel still present")
	}

	var freshCount int
	if err := db.Get(&freshCount,
		`SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND source_model = 'gpt-4o'`, routeID); err != nil {
		t.Fatalf("count fresh: %v", err)
	}
	if freshCount != 1 {
		t.Fatalf("fresh availability channel missing")
	}
}

func TestRebuildTokenRoutesFromAvailability_GroupSourcesExpandAtSelectTime(t *testing.T) {
	// Verifies the #526/#588 path: after rebuild adds channels to source exact routes,
	// explicit_group membership (route_group_sources) remains intact and pattern sources grow.
	db := setupRouteRebuildDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	_, accountID, tokenID := seedSiteAccountToken(t, db, "group-src")

	res, err := db.Exec(
		`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES ('gpt-4o', 'pattern', 'weighted', 1, ?, ?)`, now, now,
	)
	if err != nil {
		t.Fatalf("exact route: %v", err)
	}
	exactID, _ := res.LastInsertId()

	res, err = db.Exec(
		`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES ('bundle', 'bundle', 'explicit_group', 'weighted', 1, ?, ?)`, now, now,
	)
	if err != nil {
		t.Fatalf("group route: %v", err)
	}
	groupID, _ := res.LastInsertId()

	if _, err := db.Exec(
		`INSERT INTO route_group_sources (group_route_id, source_route_id) VALUES (?, ?)`,
		groupID, exactID); err != nil {
		t.Fatalf("group source: %v", err)
	}

	// New site/model becomes available → rebuild should attach to exact source route
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'gpt-4o', 1, ?)`, tokenID, now); err != nil {
		t.Fatalf("avail: %v", err)
	}

	if _, err := RebuildTokenRoutesFromAvailability(context.Background(), db.DB); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	var exactChannels int
	if err := db.Get(&exactChannels, `SELECT COUNT(*) FROM route_channels WHERE route_id = ?`, exactID); err != nil {
		t.Fatalf("count exact: %v", err)
	}
	if exactChannels != 1 {
		t.Fatalf("exact source route channels = %d, want 1 (new site auto-added)", exactChannels)
	}

	var groupChannels int
	if err := db.Get(&groupChannels, `SELECT COUNT(*) FROM route_channels WHERE route_id = ?`, groupID); err != nil {
		t.Fatalf("count group: %v", err)
	}
	if groupChannels != 0 {
		t.Fatalf("group should not materialize channels; selector expands sources")
	}

	var sourceCount int
	if err := db.Get(&sourceCount,
		`SELECT COUNT(*) FROM route_group_sources WHERE group_route_id = ? AND source_route_id = ?`,
		groupID, exactID); err != nil {
		t.Fatalf("source link: %v", err)
	}
	if sourceCount != 1 {
		t.Fatalf("group source membership lost")
	}

	// Sanity: channel is reachable via source route for this account
	var accountOnSource int
	if err := db.Get(&accountOnSource,
		`SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND account_id = ?`, exactID, accountID); err != nil {
		t.Fatalf("account on source: %v", err)
	}
	if accountOnSource != 1 {
		t.Fatalf("new account not on source route used by group")
	}
}

func TestRebuildRoutesBestEffort_ConcurrentSafe(t *testing.T) {
	db := setupRouteRebuildDB(t)
	SetRouteRebuildDB(db.DB)
	t.Cleanup(func() { SetRouteRebuildDB(nil) })

	now := time.Now().UTC().Format(time.RFC3339)
	_, _, tokenID := seedSiteAccountToken(t, db, "conc")
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'gpt-4', 1, ?)`, tokenID, now); err != nil {
		t.Fatalf("avail: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES ('gpt-*', 'pattern', 'weighted', 1, ?, ?)`, now, now); err != nil {
		t.Fatalf("route: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RebuildRoutesBestEffort()
		}()
	}
	wg.Wait()

	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM route_channels`); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected channels after concurrent rebuild")
	}
}

func TestPopulateRouteChannelsByModelPattern_InsertOnly(t *testing.T) {
	db := setupRouteRebuildDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	_, accountID, tokenID := seedSiteAccountToken(t, db, "pop")
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'gpt-4o', 1, ?)`, tokenID, now); err != nil {
		t.Fatalf("avail: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES ('gpt-*', 'pattern', 'weighted', 1, ?, ?)`, now, now,
	)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	routeID, _ := res.LastInsertId()

	// Pre-existing manual channel
	if _, err := db.Exec(
		`INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override)
		 VALUES (?, ?, ?, 'manual-x', 1, 10, 1, 1)`, routeID, accountID, tokenID); err != nil {
		t.Fatalf("manual: %v", err)
	}

	inserted, err := PopulateRouteChannelsByModelPattern(context.Background(), db.DB, routeID, "gpt-*")
	if err != nil {
		t.Fatalf("populate: %v", err)
	}
	if inserted < 1 {
		t.Fatalf("inserted = %d, want >= 1", inserted)
	}

	var manual int
	_ = db.Get(&manual, `SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND source_model = 'manual-x'`, routeID)
	if manual != 1 {
		t.Fatalf("populate must not remove manual channels")
	}
}
