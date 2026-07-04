package oauth

import (
	"testing"

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
