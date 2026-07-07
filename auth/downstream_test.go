package auth

import (
	"database/sql"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// ---------------------------------------------------------------------------
// Shared test DB helpers (used by proxy_test.go and downstream_test.go)
// ---------------------------------------------------------------------------

// setupTestDB initializes an in-memory SQLite database and registers it
// as the singleton via store.EnsureRuntimeDatabase. Returns a cleanup function.
func setupTestDB(t *testing.T) {
	t.Helper()

	// If already initialized from a previous subtest, close first.
	_ = store.CloseDatabase()

	cfg := &config.Config{
		DbType:  "sqlite",
		DbUrl:   ":memory:",
		DataDir: t.TempDir(),
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("failed to init test DB: %v", err)
	}
	t.Cleanup(func() { _ = store.CloseDatabase() })
}

// testDB returns the current active test database. Fails the test if DB is nil.
func testDB(t *testing.T) *store.DB {
	t.Helper()
	db := store.GetDB()
	if db == nil {
		t.Fatal("test database not initialized — call setupTestDB first")
	}
	return db
}

// ---------------------------------------------------------------------------
// Downstream token auth — DB-backed tests
// ---------------------------------------------------------------------------

// Ensure the DB is initialized for all tests in this file and proxy_test.go.
// TestMain would be another option, but we use a simple init approach: each test
// that needs the DB calls setupTestDB.

func TestGetManagedKeyByToken_Found(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, expires_at, max_cost, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, NULL, NULL, 0, NULL, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"test-found", "sk-crud-found", now, now,
	)
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}

	view, err := getManagedKeyByToken("sk-crud-found")
	if err != nil {
		t.Fatalf("getManagedKeyByToken: %v", err)
	}
	if view == nil {
		t.Fatal("expected view to be non-nil")
	}
	if view.Name != "test-found" {
		t.Errorf("expected name=test-found, got %q", view.Name)
	}
	if !view.Enabled {
		t.Error("expected enabled=true")
	}
}

func TestGetManagedKeyByToken_NotFound(t *testing.T) {
	setupTestDB(t)

	view, err := getManagedKeyByToken("sk-does-not-exist")
	if err != nil {
		t.Fatalf("getManagedKeyByToken should not error on not found: %v", err)
	}
	if view != nil {
		t.Error("expected nil view for unknown key")
	}
}

func TestGetManagedKeyByToken_NoRows(t *testing.T) {
	setupTestDB(t)

	// Empty table → no rows
	view, err := getManagedKeyByToken("any-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view != nil {
		t.Error("expected nil view when table is empty")
	}
}

// ---------------------------------------------------------------------------
// Key CRUD tests — insert, update, delete via direct SQL + getManagedKeyByToken
// ---------------------------------------------------------------------------

func TestDownstreamKeyCRUD_Insert(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, max_cost, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, ?, ?, ?, ?, '["gpt-4"]', '[1,2]', '{"1":1.5}', '[3]', '[{"kind":"account_token","site_id":1,"account_id":2,"token_id":3}]', ?, ?)`,
		"crud-insert", "sk-crud-insert", 100.0, 0.0, int64(1000), int64(0), now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}

	// Read back via getManagedKeyByToken (our auth function)
	view, err := getManagedKeyByToken("sk-crud-insert")
	if err != nil {
		t.Fatalf("getManagedKeyByToken: %v", err)
	}
	if view == nil {
		t.Fatal("expected view to be non-nil after insert")
	}
	if view.Name != "crud-insert" {
		t.Errorf("expected name=crud-insert, got %q", view.Name)
	}
	if view.ID != id {
		t.Errorf("expected ID=%d, got %d", id, view.ID)
	}
	if *view.MaxCost != 100.0 {
		t.Errorf("expected max_cost=100.0, got %v", *view.MaxCost)
	}
	if *view.MaxRequests != 1000 {
		t.Errorf("expected max_requests=1000, got %v", *view.MaxRequests)
	}

	// Verify JSON columns parsed correctly
	if len(view.SupportedModels) != 1 || view.SupportedModels[0] != "gpt-4" {
		t.Errorf("expected supported_models=[gpt-4], got %v", view.SupportedModels)
	}
	if len(view.AllowedRouteIDs) != 2 || view.AllowedRouteIDs[0] != 1 || view.AllowedRouteIDs[1] != 2 {
		t.Errorf("expected allowed_route_ids=[1,2], got %v", view.AllowedRouteIDs)
	}
	if view.SiteWeightMultipliers[1] != 1.5 {
		t.Errorf("expected site_weight_multipliers[1]=1.5, got %v", view.SiteWeightMultipliers[1])
	}
}

func TestDownstreamKeyCRUD_Update(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"crud-update", "sk-crud-update", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// Update name and enabled
	_, err = db.Exec(
		`UPDATE downstream_api_keys SET name = ?, enabled = 0, updated_at = ? WHERE key = ?`,
		"crud-updated", now, "sk-crud-update",
	)
	if err != nil {
		t.Fatalf("UPDATE: %v", err)
	}

	// Verify
	view, err := getManagedKeyByToken("sk-crud-update")
	if err != nil {
		t.Fatalf("getManagedKeyByToken: %v", err)
	}
	if view == nil {
		t.Fatal("expected view to exist after update")
	}
	if view.Name != "crud-updated" {
		t.Errorf("expected updated name=crud-updated, got %q", view.Name)
	}
	if view.Enabled {
		t.Error("expected enabled=false after update")
	}
}

func TestDownstreamKeyCRUD_Delete(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"crud-delete", "sk-crud-delete", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// Verify it exists
	view, err := getManagedKeyByToken("sk-crud-delete")
	if err != nil {
		t.Fatalf("getManagedKeyByToken before delete: %v", err)
	}
	if view == nil {
		t.Fatal("expected key to exist before delete")
	}

	// Delete
	_, err = db.Exec(`DELETE FROM downstream_api_keys WHERE key = ?`, "sk-crud-delete")
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}

	// Verify it no longer exists
	view, err = getManagedKeyByToken("sk-crud-delete")
	if err != nil {
		t.Fatalf("getManagedKeyByToken after delete: %v", err)
	}
	if view != nil {
		t.Error("expected nil view after delete")
	}
}

func TestDownstreamKeyCRUD_UniqueConstraint(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"crud-dup", "sk-crud-dup", now, now,
	)
	if err != nil {
		t.Fatalf("first INSERT: %v", err)
	}

	// Second insert with same key should fail (UNIQUE constraint)
	_, err = db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"crud-dup2", "sk-crud-dup", now, now,
	)
	if err == nil {
		t.Error("expected UNIQUE constraint violation on duplicate key")
	}
}

// ---------------------------------------------------------------------------
// Expiration check tests
// ---------------------------------------------------------------------------

func TestExpiration_PastDate(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	expiredAt := "2020-01-01T00:00:00Z" // 6+ years ago
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, expires_at, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, ?, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"exp-past", "sk-exp-past", expiredAt, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-exp-past", &config.Config{ProxyToken: "global"})
	if result.OK {
		t.Error("expected OK=false for past expiration")
	}
	if result.Reason != "expired" {
		t.Errorf("expected reason=expired, got %q", result.Reason)
	}
}

func TestExpiration_FutureDate(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	futureExpiry := "2099-12-31T23:59:59Z"
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, expires_at, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, ?, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"exp-future", "sk-exp-future", futureExpiry, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-exp-future", &config.Config{ProxyToken: "global"})
	if !result.OK {
		t.Fatalf("expected OK=true for future expiration, got: %s", result.Error)
	}
	if result.Source != "managed" {
		t.Errorf("expected source=managed, got %q", result.Source)
	}
}

func TestExpiration_NilExpiresAt(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, expires_at, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, NULL, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"exp-null", "sk-exp-null", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-exp-null", &config.Config{ProxyToken: "global"})
	if !result.OK {
		t.Fatalf("expected OK=true for nil expires_at, got: %s", result.Error)
	}
}

func TestExpiration_InvalidDateFormat(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	invalidDate := "not-a-date"
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, expires_at, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, ?, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"exp-invalid", "sk-exp-invalid", invalidDate, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// Invalid date → parse error → expiration check is skipped → key passes
	result := AuthorizeDownstreamToken("sk-exp-invalid", &config.Config{ProxyToken: "global"})
	if !result.OK {
		t.Fatalf("expected OK=true when expires_at is unparseable (TS: parseErr → skip), got: %s", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Cost quota tests
// ---------------------------------------------------------------------------

func TestCostQuota_UnderLimit(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	maxCost := 100.0
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, max_cost, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, ?, 50.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-under", "sk-cost-under", maxCost, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-cost-under", &config.Config{ProxyToken: "global"})
	if !result.OK {
		t.Fatalf("expected OK=true for under cost limit, got: %s", result.Error)
	}
}

func TestCostQuota_AtLimit(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	maxCost := 50.0
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, max_cost, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, ?, 50.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-at", "sk-cost-at", maxCost, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-cost-at", &config.Config{ProxyToken: "global"})
	if result.OK {
		t.Error("expected OK=false when usedCost == maxCost")
	}
	if result.Reason != "over_cost" {
		t.Errorf("expected reason=over_cost, got %q", result.Reason)
	}
}

func TestCostQuota_OverLimit(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	maxCost := 50.0
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, max_cost, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, ?, 100.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-over", "sk-cost-over", maxCost, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-cost-over", &config.Config{ProxyToken: "global"})
	if result.OK {
		t.Error("expected OK=false for over cost limit")
	}
	if result.Reason != "over_cost" {
		t.Errorf("expected reason=over_cost, got %q", result.Reason)
	}
}

func TestCostQuota_NilMaxCost(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, max_cost, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, NULL, 999999.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-nil", "sk-cost-nil", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// nil max_cost means unlimited — should pass even with high used_cost
	result := AuthorizeDownstreamToken("sk-cost-nil", &config.Config{ProxyToken: "global"})
	if !result.OK {
		t.Fatalf("expected OK=true for nil max_cost (unlimited), got: %s", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Request quota tests
// ---------------------------------------------------------------------------

func TestRequestQuota_UnderLimit(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	maxReqs := int64(1000)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, ?, 500, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"req-under", "sk-req-under", maxReqs, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-req-under", &config.Config{ProxyToken: "global"})
	if !result.OK {
		t.Fatalf("expected OK=true for under request limit, got: %s", result.Error)
	}
}

func TestRequestQuota_AtLimit(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	maxReqs := int64(500)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, ?, 500, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"req-at", "sk-req-at", maxReqs, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-req-at", &config.Config{ProxyToken: "global"})
	if result.OK {
		t.Error("expected OK=false when usedRequests == maxRequests")
	}
	if result.Reason != "over_requests" {
		t.Errorf("expected reason=over_requests, got %q", result.Reason)
	}
}

func TestRequestQuota_OverLimit(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	maxReqs := int64(100)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, ?, 200, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"req-over", "sk-req-over", maxReqs, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-req-over", &config.Config{ProxyToken: "global"})
	if result.OK {
		t.Error("expected OK=false for over request limit")
	}
	if result.Reason != "over_requests" {
		t.Errorf("expected reason=over_requests, got %q", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// Atomic increment tests — consumeManagedKeyRequest and RecordManagedKeyCostUsage
// ---------------------------------------------------------------------------

func TestConsumeManagedKeyRequest_AtomicIncrement(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, NULL, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"incr-req", "sk-incr-req", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()

	// Call consumeManagedKeyRequest multiple times
	consumeManagedKeyRequest(id)
	consumeManagedKeyRequest(id)
	consumeManagedKeyRequest(id)

	// Read back used_requests
	var usedRequests int64
	err = db.QueryRow(`SELECT used_requests FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedRequests)
	if err != nil {
		t.Fatalf("SELECT used_requests: %v", err)
	}
	if usedRequests != 3 {
		t.Errorf("expected used_requests=3 after 3 increments, got %d", usedRequests)
	}

	// last_used_at should be updated (not empty)
	var lastUsedAt sql.NullString
	err = db.QueryRow(`SELECT last_used_at FROM downstream_api_keys WHERE id = ?`, id).Scan(&lastUsedAt)
	if err != nil {
		t.Fatalf("SELECT last_used_at: %v", err)
	}
	if !lastUsedAt.Valid || lastUsedAt.String == "" {
		t.Error("expected last_used_at to be updated after consumeManagedKeyRequest")
	}
}

func TestConsumeManagedKeyRequest_RespectsMaxRequests(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, 1, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"incr-limited", "sk-incr-limited", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()

	if !consumeManagedKeyRequest(id) {
		t.Fatal("expected first request reservation to succeed")
	}
	if consumeManagedKeyRequest(id) {
		t.Fatal("expected second request reservation to fail at max_requests=1")
	}

	var usedRequests int64
	err = db.QueryRow(`SELECT used_requests FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedRequests)
	if err != nil {
		t.Fatalf("SELECT used_requests: %v", err)
	}
	if usedRequests != 1 {
		t.Errorf("expected used_requests to stay at max_requests=1, got %d", usedRequests)
	}
}

func TestConsumeManagedKeyRequest_FromNull(t *testing.T) {
	// used_requests starts as NULL from schema default — COALESCE should handle it
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	// Insert with explicit NULL used_requests
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, max_requests, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 0, NULL, NULL, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"incr-null", "sk-incr-null", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()

	consumeManagedKeyRequest(id)

	var usedRequests sql.NullInt64
	err = db.QueryRow(`SELECT used_requests FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedRequests)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if !usedRequests.Valid || usedRequests.Int64 != 1 {
		t.Errorf("expected used_requests=1 from NULL start, got %v", usedRequests)
	}
}

func TestRecordManagedKeyCostUsage_PositiveCost(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 10.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-incr", "sk-cost-incr", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()

	RecordManagedKeyCostUsage(id, 5.5)

	var usedCost float64
	err = db.QueryRow(`SELECT used_cost FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedCost)
	if err != nil {
		t.Fatalf("SELECT used_cost: %v", err)
	}
	if usedCost != 15.5 {
		t.Errorf("expected used_cost=15.5 (10.0+5.5), got %v", usedCost)
	}
}

func TestRecordManagedKeyCostUsage_ZeroCost(t *testing.T) {
	// Zero cost should be skipped (no-op)
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 10.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-zero", "sk-cost-zero", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()

	RecordManagedKeyCostUsage(id, 0) // should be no-op

	var usedCost float64
	err = db.QueryRow(`SELECT used_cost FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedCost)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if usedCost != 10.0 {
		t.Errorf("expected used_cost=10.0 (unchanged), got %v", usedCost)
	}
}

func TestRecordManagedKeyCostUsage_NegativeCost(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 10.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-neg", "sk-cost-neg", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()

	RecordManagedKeyCostUsage(id, -5.0) // negative → should be no-op

	var usedCost float64
	err = db.QueryRow(`SELECT used_cost FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedCost)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if usedCost != 10.0 {
		t.Errorf("expected used_cost=10.0 (unchanged for negative cost), got %v", usedCost)
	}
}

func TestRecordManagedKeyCostUsage_NaN(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 10.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-nan", "sk-cost-nan", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()

	// NaN should be skipped (not > 0)
	RecordManagedKeyCostUsage(id, math.NaN())

	var usedCost float64
	err = db.QueryRow(`SELECT used_cost FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedCost)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if usedCost != 10.0 {
		t.Errorf("expected used_cost=10.0 (unchanged for NaN), got %v", usedCost)
	}
}

func TestRecordManagedKeyCostUsage_Inf(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, 10.0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"cost-inf", "sk-cost-inf", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, _ := res.LastInsertId()

	// Inf should be skipped (not valid)
	RecordManagedKeyCostUsage(id, math.Inf(1))
	// Neg Inf
	RecordManagedKeyCostUsage(id, math.Inf(-1))

	var usedCost float64
	err = db.QueryRow(`SELECT used_cost FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedCost)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if usedCost != 10.0 {
		t.Errorf("expected used_cost=10.0 (unchanged for Inf), got %v", usedCost)
	}
}

// ---------------------------------------------------------------------------
// Disabled key tests
// ---------------------------------------------------------------------------

func TestDisabledKey_Rejected(t *testing.T) {
	setupTestDB(t)
	db := testDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 0, 0, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		"disabled-key", "sk-disabled", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	result := AuthorizeDownstreamToken("sk-disabled", &config.Config{ProxyToken: "global"})
	if result.OK {
		t.Error("expected OK=false for disabled key")
	}
	if result.StatusCode != 403 {
		t.Errorf("expected 403, got %d", result.StatusCode)
	}
	if result.Reason != "disabled" {
		t.Errorf("expected reason=disabled, got %q", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// toPolicyFromView tests
// ---------------------------------------------------------------------------

func TestToPolicyFromView_DenyAllWhenEmpty(t *testing.T) {
	view := &managedKeyView{
		ID:   1,
		Name: "test-deny",
	}

	policy := toPolicyFromView(view)
	if !policy.DenyAllWhenEmpty {
		t.Error("expected DenyAllWhenEmpty=true for managed key policy")
	}
	// Empty slices/maps should be non-nil after normalization
	if policy.SupportedModels == nil {
		t.Error("expected SupportedModels to be non-nil (empty slice)")
	}
	if policy.AllowedRouteIDs == nil {
		t.Error("expected AllowedRouteIDs to be non-nil (empty slice)")
	}
	if policy.SiteWeightMultipliers == nil {
		t.Error("expected SiteWeightMultipliers to be non-nil (empty map)")
	}
}

func TestToPolicyFromView_WithData(t *testing.T) {
	view := &managedKeyView{
		ID:                    2,
		Name:                  "test-with-data",
		SupportedModels:       []string{"gpt-4", "claude-3"},
		AllowedRouteIDs:       []int64{1, 2, 3},
		SiteWeightMultipliers: map[int64]float64{1: 1.5, 2: 2.0},
		ExcludedSiteIDs:       []int64{99},
		ExcludedCredentialRefs: []ExcludedCredentialRef{
			{Kind: CredentialRefAccountToken, SiteID: 1, AccountID: 2, TokenID: int64Ptr(3)},
		},
	}

	policy := toPolicyFromView(view)
	if !policy.DenyAllWhenEmpty {
		t.Error("expected DenyAllWhenEmpty=true")
	}
	if len(policy.SupportedModels) != 2 {
		t.Errorf("expected 2 supported models, got %d", len(policy.SupportedModels))
	}
	if len(policy.AllowedRouteIDs) != 3 {
		t.Errorf("expected 3 route IDs, got %d", len(policy.AllowedRouteIDs))
	}
	if policy.SiteWeightMultipliers[1] != 1.5 {
		t.Errorf("expected multiplier 1.5 for site 1, got %v", policy.SiteWeightMultipliers[1])
	}
	if len(policy.ExcludedCredentialRefs) != 1 {
		t.Errorf("expected 1 excluded cred ref, got %d", len(policy.ExcludedCredentialRefs))
	}
}

// ---------------------------------------------------------------------------
// GetProxyResourceOwner tests
// ---------------------------------------------------------------------------

func TestGetProxyResourceOwner_Nil(t *testing.T) {
	owner := GetProxyResourceOwner(nil)
	if owner != nil {
		t.Error("expected nil for nil auth context")
	}
}

func TestGetProxyResourceOwner_ManagedKey(t *testing.T) {
	keyID := int64(42)
	pac := &ProxyAuthContext{
		Token:   "sk-managed-123",
		Source:  "managed",
		KeyID:   &keyID,
		KeyName: "my-key",
	}

	owner := GetProxyResourceOwner(pac)
	if owner == nil {
		t.Fatal("expected non-nil owner")
	}
	if owner.OwnerType != "managed_key" {
		t.Errorf("expected OwnerType=managed_key, got %q", owner.OwnerType)
	}
	if owner.OwnerID != "42" {
		t.Errorf("expected OwnerID=42, got %q", owner.OwnerID)
	}
}

func TestGetProxyResourceOwner_ManagedKeyNilID(t *testing.T) {
	pac := &ProxyAuthContext{
		Token:   "sk-managed-fallback",
		Source:  "managed",
		KeyID:   nil,
		KeyName: "my-key",
	}

	owner := GetProxyResourceOwner(pac)
	if owner == nil {
		t.Fatal("expected non-nil owner")
	}
	if owner.OwnerType != "managed_key" {
		t.Errorf("expected OwnerType=managed_key, got %q", owner.OwnerType)
	}
	// KeyID is nil → falls back to token
	if owner.OwnerID != "sk-managed-fallback" {
		t.Errorf("expected OwnerID=sk-managed-fallback (token fallback), got %q", owner.OwnerID)
	}
}

func TestGetProxyResourceOwner_GlobalProxyToken(t *testing.T) {
	pac := &ProxyAuthContext{
		Token:   "global-token",
		Source:  "global",
		KeyName: "global",
	}

	owner := GetProxyResourceOwner(pac)
	if owner == nil {
		t.Fatal("expected non-nil owner")
	}
	if owner.OwnerType != "global_proxy_token" {
		t.Errorf("expected OwnerType=global_proxy_token, got %q", owner.OwnerType)
	}
	if owner.OwnerID != "global" {
		t.Errorf("expected OwnerID=global, got %q", owner.OwnerID)
	}
}

// ---------------------------------------------------------------------------
// parseISO8601 tests
// ---------------------------------------------------------------------------

func TestParseISO8601_RFC3339(t *testing.T) {
	ts, err := parseISO8601("2026-07-04T12:00:00Z")
	if err != nil {
		t.Fatalf("parseISO8601 failed: %v", err)
	}
	if ts.Year() != 2026 || ts.Month() != 7 || ts.Day() != 4 {
		t.Errorf("expected 2026-07-04, got %v", ts)
	}
}

func TestParseISO8601_RFC3339Nano(t *testing.T) {
	ts, err := parseISO8601("2026-07-04T12:00:00.123456789Z")
	if err != nil {
		t.Fatalf("parseISO8601 failed: %v", err)
	}
	if ts.Nanosecond() == 0 {
		t.Error("expected non-zero nanoseconds")
	}
}

func TestParseISO8601_WithOffset(t *testing.T) {
	ts, err := parseISO8601("2026-07-04T12:00:00+08:00")
	if err != nil {
		t.Fatalf("parseISO8601 failed: %v", err)
	}
	if ts.Year() != 2026 {
		t.Errorf("expected 2026, got %d", ts.Year())
	}
}

func TestParseISO8601_NoT(t *testing.T) {
	ts, err := parseISO8601("2026-07-04 12:00:00")
	if err != nil {
		t.Fatalf("parseISO8601 failed: %v", err)
	}
	if ts.Year() != 2026 || ts.Month() != 7 || ts.Day() != 4 {
		t.Errorf("expected 2026-07-04, got %v", ts)
	}
}

func TestParseISO8601_Invalid(t *testing.T) {
	_, err := parseISO8601("not-a-date")
	if err == nil {
		t.Error("expected error for invalid date")
	}
}

// ---------------------------------------------------------------------------
// isNoRows tests
// ---------------------------------------------------------------------------

func TestIsNoRows_NilError(t *testing.T) {
	if isNoRows(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsNoRows_NoRowsError(t *testing.T) {
	err := sql.ErrNoRows
	if !isNoRows(err) {
		t.Error("expected true for sql.ErrNoRows")
	}
}

func TestIsNoRows_OtherError(t *testing.T) {
	// A non-no-rows error (e.g., a connection error)
	err := sql.ErrConnDone
	if isNoRows(err) {
		t.Error("expected false for non-no-rows error")
	}
}

// ---------------------------------------------------------------------------
// formatInt64 tests
// ---------------------------------------------------------------------------

func TestFormatInt64_Zero(t *testing.T) {
	if got := formatInt64(0); got != "0" {
		t.Errorf("expected '0', got %q", got)
	}
}

func TestFormatInt64_Positive(t *testing.T) {
	if got := formatInt64(42); got != "42" {
		t.Errorf("expected '42', got %q", got)
	}
}

func TestFormatInt64_Large(t *testing.T) {
	if got := formatInt64(1234567890); got != "1234567890" {
		t.Errorf("expected '1234567890', got %q", got)
	}
}

func TestFormatInt64_Negative(t *testing.T) {
	if got := formatInt64(-42); got != "-42" {
		t.Errorf("expected '-42', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// AuthContext tests
// ---------------------------------------------------------------------------

func TestWithAdminAuth_IsAdmin(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/sites", nil)
	req = AdminAuthFromRequest(req)

	if !IsAdmin(req.Context()) {
		t.Error("expected IsAdmin=true after WithAdminAuth")
	}
}

func TestIsAdmin_False(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/sites", nil)
	if IsAdmin(req.Context()) {
		t.Error("expected IsAdmin=false without WithAdminAuth")
	}
}

func TestWithProxyAuth_GetProxyAuth(t *testing.T) {
	pac := &ProxyAuthContext{
		Token:   "test-token",
		Source:  "managed",
		KeyName: "test-key",
	}
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req = ProxyAuthFromRequest(req, pac)

	got := GetProxyAuth(req.Context())
	if got == nil {
		t.Fatal("expected non-nil ProxyAuthContext")
	}
	if got.Token != "test-token" {
		t.Errorf("expected Token=test-token, got %q", got.Token)
	}
	if got.Source != "managed" {
		t.Errorf("expected Source=managed, got %q", got.Source)
	}
}

func TestGetProxyAuth_Nil(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/models", nil)
	got := GetProxyAuth(req.Context())
	if got != nil {
		t.Error("expected nil ProxyAuthContext without WithProxyAuth")
	}
}

// ---------------------------------------------------------------------------
// EmptyDownstreamRoutingPolicy tests
// ---------------------------------------------------------------------------

func TestEmptyDownstreamRoutingPolicy_DenyAllWhenEmpty(t *testing.T) {
	if EmptyDownstreamRoutingPolicy.DenyAllWhenEmpty {
		t.Error("expected DenyAllWhenEmpty=false for global default policy")
	}
}

func TestEmptyDownstreamRoutingPolicy_FieldsNonNil(t *testing.T) {
	if EmptyDownstreamRoutingPolicy.SupportedModels == nil {
		t.Error("expected SupportedModels to be non-nil")
	}
	if EmptyDownstreamRoutingPolicy.AllowedRouteIDs == nil {
		t.Error("expected AllowedRouteIDs to be non-nil")
	}
	if EmptyDownstreamRoutingPolicy.SiteWeightMultipliers == nil {
		t.Error("expected SiteWeightMultipliers to be non-nil")
	}
}

// ---------------------------------------------------------------------------
// JSON parsing helper tests
// ---------------------------------------------------------------------------

func TestParseStringArray_Valid(t *testing.T) {
	raw := `["a","b","c"]`
	result := parseStringArray(&raw)
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("unexpected values: %v", result)
	}
}

func TestParseStringArray_Nil(t *testing.T) {
	result := parseStringArray(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestParseStringArray_EmptyString(t *testing.T) {
	raw := ""
	result := parseStringArray(&raw)
	if result != nil {
		t.Errorf("expected nil for empty string, got %v", result)
	}
}

func TestParseStringArray_Null(t *testing.T) {
	raw := "null"
	result := parseStringArray(&raw)
	if result != nil {
		t.Errorf("expected nil for null, got %v", result)
	}
}

func TestParseStringArray_Invalid(t *testing.T) {
	raw := "not-json"
	result := parseStringArray(&raw)
	if result != nil {
		t.Errorf("expected nil for invalid JSON, got %v", result)
	}
}

func TestParseInt64Array_Valid(t *testing.T) {
	raw := `[1,2,3]`
	result := parseInt64Array(&raw)
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0] != 1 || result[1] != 2 || result[2] != 3 {
		t.Errorf("unexpected values: %v", result)
	}
}

func TestParseInt64Array_Nil(t *testing.T) {
	result := parseInt64Array(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestParseExcludedCredentialRefs_Valid(t *testing.T) {
	raw := `[{"kind":"account_token","site_id":1,"account_id":2,"token_id":3}]`
	result := parseExcludedCredentialRefs(&raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Kind != CredentialRefAccountToken {
		t.Errorf("expected kind=account_token, got %q", result[0].Kind)
	}
	if result[0].SiteID != 1 {
		t.Errorf("expected site_id=1, got %d", result[0].SiteID)
	}
	if result[0].TokenID == nil || *result[0].TokenID != 3 {
		t.Errorf("expected token_id=3, got %v", result[0].TokenID)
	}
}

func TestParseExcludedCredentialRefs_DefaultApiKey(t *testing.T) {
	raw := `[{"kind":"default_api_key","site_id":5,"account_id":10}]`
	result := parseExcludedCredentialRefs(&raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Kind != CredentialRefDefaultApiKey {
		t.Errorf("expected kind=default_api_key, got %q", result[0].Kind)
	}
	if result[0].TokenID != nil {
		t.Errorf("expected token_id=nil for default_api_key, got %v", result[0].TokenID)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func int64Ptr(n int64) *int64 {
	return &n
}
