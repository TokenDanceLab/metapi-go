package oauth

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// setupTestDB creates an in-memory SQLite database and initializes schema.
// Returns a cleanup function that should be deferred.
func setupTestDB(t *testing.T) (*store.DB, func()) {
	t.Helper()

	// Close any previously active database to reset singleton state.
	store.CloseDatabase()

	// Use EnsureRuntimeDatabase with in-memory SQLite.
	cfg := &config.Config{
		DbType:  store.DialectSQLite,
		DbUrl:   ":memory:",
		DataDir: ".",
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("failed to initialize test database: %v", err)
	}

	db := store.GetDB()
	if db == nil {
		t.Fatal("GetDB returned nil after EnsureRuntimeDatabase")
	}

	cleanup := func() {
		store.CloseDatabase()
	}
	return db, cleanup
}

func setupPostgresRouteUnitDB(t *testing.T) (*store.DB, func()) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("PG_TEST_DSN"))
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set; skipping PostgreSQL integration test")
	}

	store.CloseDatabase()
	cfg := &config.Config{
		DbType:  store.DialectPostgres,
		DbUrl:   dsn,
		DataDir: ".",
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("failed to initialize PostgreSQL test database: %v", err)
	}

	db := store.GetDB()
	if db == nil {
		t.Fatal("GetDB returned nil after PostgreSQL initialization")
	}

	cleanup := func() {
		store.CloseDatabase()
	}
	return db, cleanup
}

// ---- normalizeStrategy Tests ----

func TestNormalizeStrategy_RoundRobin(t *testing.T) {
	tests := []string{"round_robin", "ROUND_ROBIN", "Round_Robin", "round_robin "}
	for _, input := range tests {
		result := normalizeStrategy(input)
		if result != RouteUnitStrategyRoundRobin {
			t.Errorf("normalizeStrategy(%q) = %q, want %q", input, result, RouteUnitStrategyRoundRobin)
		}
	}
}

func TestNormalizeStrategy_StickUntilUnavailable(t *testing.T) {
	tests := []string{"stick_until_unavailable", "STICK_UNTIL_UNAVAILABLE", "Stick_Until_Unavailable"}
	for _, input := range tests {
		result := normalizeStrategy(input)
		if result != RouteUnitStrategyStickUntilUnavailable {
			t.Errorf("normalizeStrategy(%q) = %q, want %q", input, result, RouteUnitStrategyStickUntilUnavailable)
		}
	}
}

func TestNormalizeStrategy_Unknown(t *testing.T) {
	result := normalizeStrategy("random")
	if result != RouteUnitStrategyRoundRobin {
		t.Errorf("normalizeStrategy('random') = %q, want %q (fallback default)", result, RouteUnitStrategyRoundRobin)
	}
}

func TestNormalizeStrategy_Empty(t *testing.T) {
	result := normalizeStrategy("")
	if result != RouteUnitStrategyRoundRobin {
		t.Errorf("normalizeStrategy('') = %q, want %q (fallback default)", result, RouteUnitStrategyRoundRobin)
	}
}

// ---- uniquePositiveIDs Tests ----

func TestUniquePositiveIDs_NoDuplicates(t *testing.T) {
	result := uniquePositiveIDs([]int64{1, 2, 3})
	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}
}

func TestUniquePositiveIDs_Duplicates(t *testing.T) {
	result := uniquePositiveIDs([]int64{1, 2, 2, 3, 1})
	if len(result) != 3 {
		t.Errorf("expected 3 items (deduplicated), got %d", len(result))
	}
}

func TestUniquePositiveIDs_FiltersNonPositive(t *testing.T) {
	result := uniquePositiveIDs([]int64{0, -1, 2, 3})
	if len(result) != 2 {
		t.Errorf("expected 2 items (filters 0 and negative), got %d", len(result))
	}
	if result[0] != 2 || result[1] != 3 {
		t.Errorf("expected [2, 3], got %v", result)
	}
}

func TestUniquePositiveIDs_Empty(t *testing.T) {
	result := uniquePositiveIDs([]int64{})
	if len(result) != 0 {
		t.Errorf("expected empty, got %d items", len(result))
	}
}

func TestUniquePositiveIDs_AllNegativeOrZero(t *testing.T) {
	result := uniquePositiveIDs([]int64{0, -1, -2})
	if len(result) != 0 {
		t.Errorf("expected empty, got %d items", len(result))
	}
}

// ---- max64 Tests ----

func TestMax64_FirstLarger(t *testing.T) {
	if max64(10, 5) != 10 {
		t.Error("expected 10")
	}
}

func TestMax64_SecondLarger(t *testing.T) {
	if max64(5, 10) != 10 {
		t.Error("expected 10")
	}
}

func TestMax64_Equal(t *testing.T) {
	if max64(5, 5) != 5 {
		t.Error("expected 5")
	}
}

// ---- Route Unit Validation Tests (no DB needed) ----

func TestRouteUnitStrategyConstants(t *testing.T) {
	if RouteUnitStrategyRoundRobin != "round_robin" {
		t.Errorf("expected 'round_robin', got %q", RouteUnitStrategyRoundRobin)
	}
	if RouteUnitStrategyStickUntilUnavailable != "stick_until_unavailable" {
		t.Errorf("expected 'stick_until_unavailable', got %q", RouteUnitStrategyStickUntilUnavailable)
	}
}

// ---- CreateOauthRouteUnit Tests (requires DB) ----

func TestCreateOauthRouteUnit_RequiresAtLeastTwoAccounts(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{1},
		Name:       "my-unit",
		Strategy:   RouteUnitStrategyRoundRobin,
	})
	if err == nil {
		t.Fatal("expected error for less than 2 accounts")
	}
}

func TestCreateOauthRouteUnit_EmptyAccountList(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{},
		Name:       "my-unit",
		Strategy:   RouteUnitStrategyRoundRobin,
	})
	if err == nil {
		t.Fatal("expected error for empty account list")
	}
}

func TestCreateOauthRouteUnit_EmptyName(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{1, 2},
		Name:       "",
		Strategy:   RouteUnitStrategyRoundRobin,
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateOauthRouteUnit_WhitespaceName(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{1, 2},
		Name:       "   ",
		Strategy:   RouteUnitStrategyRoundRobin,
	})
	if err == nil {
		t.Fatal("expected error for whitespace name")
	}
}

func TestCreateOauthRouteUnit_InvalidStrategy(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{1, 2},
		Name:       "my-unit",
		Strategy:   "invalid",
	})
	if err == nil {
		t.Fatal("expected error for invalid strategy")
	}
}

func TestCreateOauthRouteUnit_AccountsNotFound(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{99999, 99998},
		Name:       "my-unit",
		Strategy:   RouteUnitStrategyRoundRobin,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent accounts")
	}
}

func TestCreateOauthRouteUnit_DedupAccountIDs(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	// [1,1,2] deduplicates to [1,2] = 2 valid account IDs.
	// Validation passes for count, but accounts 1 and 2 don't exist in this test DB.
	_, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{1, 1, 2},
		Name:       "my-unit",
		Strategy:   RouteUnitStrategyRoundRobin,
	})
	if err == nil {
		t.Fatal("expected error: accounts 1 and 2 don't exist in this DB")
	}
}

func TestCreateOauthRouteUnit_NegativeAccountIDsFiltered(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{-1, 0, 1, 2},
		Name:       "my-unit",
		Strategy:   RouteUnitStrategyRoundRobin,
	})
	if err == nil {
		t.Fatal("expected error: after filtering, valid accounts [1,2] exist but accounts not found")
	}
}

// ---- UpdateOauthRouteUnit Tests (requires DB) ----

func TestUpdateOauthRouteUnit_NotFound(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	name := "new-name"
	_, err := UpdateOauthRouteUnit(99999, &name, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent route unit")
	}
}

func TestUpdateOauthRouteUnit_EmptyName(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	name := ""
	_, err := UpdateOauthRouteUnit(1, &name, nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestUpdateOauthRouteUnit_InvalidStrategy(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	strategy := OAuthRouteUnitStrategy("bad")
	_, err := UpdateOauthRouteUnit(1, nil, &strategy)
	if err == nil {
		t.Fatal("expected error for invalid strategy")
	}
}

// ---- DeleteOauthRouteUnit Tests (requires DB) ----

func TestDeleteOauthRouteUnit_NotFound(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	err := DeleteOauthRouteUnit(99999)
	if err == nil {
		t.Fatal("expected error for nonexistent route unit")
	}
}

// ---- ListOauthRouteUnitsByAccountIDs Tests (no DB / empty) ----

func TestListOauthRouteUnitsByAccountIDs_EmptyInput(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	result := ListOauthRouteUnitsByAccountIDs([]int64{})
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestListOauthRouteUnitsByAccountIDs_NoMatches(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	result := ListOauthRouteUnitsByAccountIDs([]int64{99999})
	if len(result) != 0 {
		t.Errorf("expected empty map for no matches, got %d entries", len(result))
	}
}

// ---- ListEnabledOauthRouteUnitsWithMembers Tests (requires DB) ----

func TestListEnabledOauthRouteUnitsWithMembers_NoData(t *testing.T) {
	_, cleanup := setupTestDB(t)
	defer cleanup()

	result := ListEnabledOauthRouteUnitsWithMembers()
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

func TestOauthRouteUnit_SQLiteLifecycle(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	runOauthRouteUnitLifecycle(t, db, "sqlite-"+strings.ReplaceAll(t.Name(), "/", "-"))
}

func TestOauthRouteUnit_PostgresLifecycle(t *testing.T) {
	db, cleanup := setupPostgresRouteUnitDB(t)
	defer cleanup()

	suffix := "pg-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM sites WHERE url = ?", "https://route-unit-"+suffix+".example.com")
	})

	runOauthRouteUnitLifecycle(t, db, suffix)
}

func runOauthRouteUnitLifecycle(t *testing.T, db *store.DB, suffix string) {
	t.Helper()

	previousHooks := workflowHooks
	SetOAuthWorkflowHooks(nil)
	t.Cleanup(func() { SetOAuthWorkflowHooks(previousHooks) })

	siteID := insertRouteUnitSite(t, db, suffix)
	accountID1 := insertRouteUnitAccount(t, db, siteID, "codex", suffix+"-1")
	accountID2 := insertRouteUnitAccount(t, db, siteID, "codex", suffix+"-2")

	created, err := CreateOauthRouteUnit(CreateRouteUnitInput{
		AccountIDs: []int64{accountID1, accountID2},
		Name:       "unit-" + suffix,
		Strategy:   RouteUnitStrategyRoundRobin,
	})
	if err != nil {
		t.Fatalf("CreateOauthRouteUnit: %v", err)
	}
	if created == nil || created.RouteUnit == nil || created.RouteUnit.ID <= 0 {
		t.Fatalf("unexpected create result: %+v", created)
	}
	if created.RouteUnit.MemberCount != 2 {
		t.Fatalf("created member count = %d, want 2", created.RouteUnit.MemberCount)
	}

	participation := ListOauthRouteUnitsByAccountIDs([]int64{accountID1, accountID2})
	if len(participation) != 2 {
		t.Fatalf("participation count = %d, want 2", len(participation))
	}
	if participation[accountID1].ID != created.RouteUnit.ID || participation[accountID1].MemberCount != 2 {
		t.Fatalf("unexpected participation for account 1: %+v", participation[accountID1])
	}

	enabled := ListEnabledOauthRouteUnitsWithMembers()
	if len(enabled) != 1 {
		t.Fatalf("enabled route units = %d, want 1", len(enabled))
	}
	if got := len(enabled[0].Members); got != 2 {
		t.Fatalf("enabled member count = %d, want 2", got)
	}

	newName := "unit-updated-" + suffix
	newStrategy := RouteUnitStrategyStickUntilUnavailable
	updated, err := UpdateOauthRouteUnit(created.RouteUnit.ID, &newName, &newStrategy)
	if err != nil {
		t.Fatalf("UpdateOauthRouteUnit: %v", err)
	}
	if updated.Name != newName || updated.Strategy != newStrategy || updated.MemberCount != 2 {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	routeID := insertRouteUnitTokenRoute(t, db, suffix)
	insertRouteUnitChannel(t, db, routeID, accountID1, created.RouteUnit.ID)

	if err := DeleteOauthRouteUnit(created.RouteUnit.ID); err != nil {
		t.Fatalf("DeleteOauthRouteUnit: %v", err)
	}

	assertRouteUnitCount(t, db, "oauth_route_units", "id", created.RouteUnit.ID, 0)
	assertRouteUnitCount(t, db, "oauth_route_unit_members", "unit_id", created.RouteUnit.ID, 0)
	assertRouteUnitCount(t, db, "route_channels", "oauth_route_unit_id", created.RouteUnit.ID, 0)
}

func insertRouteUnitSite(t *testing.T, db *store.DB, suffix string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if db.Dialect == store.DialectPostgres {
		var id int64
		err := db.QueryRowx(
			`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
			"Route Unit "+suffix,
			"https://route-unit-"+suffix+".example.com",
			"openai",
			"active",
			now,
			now,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert postgres site: %v", err)
		}
		return id
	}

	result, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"Route Unit "+suffix,
		"https://route-unit-"+suffix+".example.com",
		"openai",
		"active",
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert sqlite site: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("sqlite site LastInsertId: %v", err)
	}
	return id
}

func insertRouteUnitAccount(t *testing.T, db *store.DB, siteID int64, provider string, suffix string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if db.Dialect == store.DialectPostgres {
		var id int64
		err := db.QueryRowx(
			`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled,
			 oauth_provider, oauth_account_key, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			siteID,
			"account-"+suffix,
			"access-"+suffix,
			"active",
			true,
			provider,
			"oauth-key-"+suffix,
			now,
			now,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert postgres account: %v", err)
		}
		return id
	}

	result, err := db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled,
		 oauth_provider, oauth_account_key, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		siteID,
		"account-"+suffix,
		"access-"+suffix,
		"active",
		true,
		provider,
		"oauth-key-"+suffix,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert sqlite account: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("sqlite account LastInsertId: %v", err)
	}
	return id
}

func insertRouteUnitTokenRoute(t *testing.T, db *store.DB, suffix string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if db.Dialect == store.DialectPostgres {
		var id int64
		err := db.QueryRowx(
			`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			"model-"+suffix,
			"Route "+suffix,
			"single",
			"priority",
			true,
			now,
			now,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert postgres token route: %v", err)
		}
		return id
	}

	result, err := db.Exec(
		`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"model-"+suffix,
		"Route "+suffix,
		"single",
		"priority",
		true,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert sqlite token route: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("sqlite token route LastInsertId: %v", err)
	}
	return id
}

func insertRouteUnitChannel(t *testing.T, db *store.DB, routeID int64, accountID int64, unitID int64) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO route_channels (route_id, account_id, oauth_route_unit_id, priority, weight, enabled, manual_override)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		routeID,
		accountID,
		unitID,
		0,
		10,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("insert route channel: %v", err)
	}
}

func assertRouteUnitCount(t *testing.T, db *store.DB, table string, column string, id int64, want int) {
	t.Helper()
	var got int
	query := "SELECT COUNT(*) FROM " + table + " WHERE " + column + " = ?"
	if err := db.Get(&got, query, id); err != nil {
		t.Fatalf("count %s.%s: %v", table, column, err)
	}
	if got != want {
		t.Fatalf("%s.%s count = %d, want %d", table, column, got, want)
	}
}
