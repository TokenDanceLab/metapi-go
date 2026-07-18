package proxyhandler

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

func setupManagedKeyCostTestDB(t *testing.T) *store.DB {
	t.Helper()
	_ = store.CloseDatabase()
	cfg := &config.Config{
		DbType:  "sqlite",
		DbUrl:   ":memory:",
		DataDir: t.TempDir(),
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("init test DB: %v", err)
	}
	t.Cleanup(func() { _ = store.CloseDatabase() })
	db := store.GetDB()
	if db == nil {
		t.Fatal("test database not initialized")
	}
	return db
}

func insertManagedKeyWithCost(t *testing.T, db *store.DB, name, key string, usedCost float64) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		 (name, key, enabled, used_cost, used_requests,
		  supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		  created_at, updated_at)
		 VALUES (?, ?, 1, ?, 0, '[]', '[]', '{}', '[]', '[]', ?, ?)`,
		name, key, usedCost, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT managed key: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

func readUsedCost(t *testing.T, db *store.DB, id int64) float64 {
	t.Helper()
	var usedCost float64
	if err := db.QueryRow(`SELECT used_cost FROM downstream_api_keys WHERE id = ?`, id).Scan(&usedCost); err != nil {
		t.Fatalf("SELECT used_cost: %v", err)
	}
	return usedCost
}

func TestRecordManagedKeyCostOnSuccess_IncrementsUsedCost(t *testing.T) {
	db := setupManagedKeyCostTestDB(t)
	id := insertManagedKeyWithCost(t, db, "cost-wire", "sk-cost-wire", 10.0)

	keyID := id
	recordManagedKeyCostOnSuccess(&keyID, 5.5)

	if got := readUsedCost(t, db, id); got != 15.5 {
		t.Fatalf("used_cost=%v want 15.5 after success cost wire", got)
	}
}

func TestRecordManagedKeyCostOnSuccess_NilKeyIDNoop(t *testing.T) {
	db := setupManagedKeyCostTestDB(t)
	id := insertManagedKeyWithCost(t, db, "cost-nil-key", "sk-cost-nil-key", 10.0)

	// Global/proxy token path: Auth.KeyID is nil — must not invent an owner or crash.
	recordManagedKeyCostOnSuccess(nil, 9.9)

	if got := readUsedCost(t, db, id); got != 10.0 {
		t.Fatalf("used_cost=%v want 10.0 when KeyID is nil", got)
	}
}

func TestRecordManagedKeyCostOnSuccess_SkipsZeroNaNInf(t *testing.T) {
	db := setupManagedKeyCostTestDB(t)
	id := insertManagedKeyWithCost(t, db, "cost-skip", "sk-cost-skip", 10.0)
	keyID := id

	recordManagedKeyCostOnSuccess(&keyID, 0)
	recordManagedKeyCostOnSuccess(&keyID, math.NaN())
	recordManagedKeyCostOnSuccess(&keyID, math.Inf(1))
	recordManagedKeyCostOnSuccess(&keyID, math.Inf(-1))

	if got := readUsedCost(t, db, id); got != 10.0 {
		t.Fatalf("used_cost=%v want 10.0 when cost is zero/NaN/Inf", got)
	}
}

type proxyLogCapture struct {
	cost  float64
	keyID *int64
}

func TestWriteSuccessProxyLog_RecordsManagedKeyCost(t *testing.T) {
	db := setupManagedKeyCostTestDB(t)
	id := insertManagedKeyWithCost(t, db, "cost-success-log", "sk-cost-success-log", 1.0)

	var logged []proxyLogCapture
	cfg := &UpstreamConfig{
		LogProxy: func(_ context.Context, entry proxy.ProxyLogEntry) error {
			logged = append(logged, proxyLogCapture{cost: entry.EstimatedCost, keyID: entry.DownstreamAPIKeyID})
			return nil
		},
	}
	keyID := id
	pctx := &Ctx{RequestedModel: "gpt-4o", Auth: &auth.ProxyAuthContext{KeyID: &keyID}}
	selected := &routing.SelectedChannel{
		Channel: store.RouteChannel{ID: 1, RouteID: 2, Enabled: true},
		Account: store.Account{ID: 3, Status: "active"},
		Site:    store.Site{ID: 4, Platform: "openai", Status: "active"},
	}
	usage := ParsedUsage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500, Found: true, Source: "upstream"}

	writeSuccessProxyLog(context.Background(), cfg, selected, pctx, "gpt-4o", "/v1/chat/completions", 12, 200, false, usage, 0, "req-cost-1")

	if len(logged) != 1 {
		t.Fatalf("expected 1 proxy log write, got %d", len(logged))
	}
	if logged[0].cost <= 0 {
		t.Fatalf("expected positive estimated cost in proxy log, got %v", logged[0].cost)
	}
	if logged[0].keyID == nil || *logged[0].keyID != id {
		t.Fatalf("proxy log DownstreamAPIKeyID=%v want %d", logged[0].keyID, id)
	}
	got := readUsedCost(t, db, id)
	if got <= 1.0 {
		t.Fatalf("used_cost=%v want > 1.0 after writeSuccessProxyLog", got)
	}
}

func TestWriteFailureProxyLog_DoesNotRecordManagedKeyCost(t *testing.T) {
	db := setupManagedKeyCostTestDB(t)
	id := insertManagedKeyWithCost(t, db, "cost-fail-log", "sk-cost-fail-log", 1.0)

	cfg := &UpstreamConfig{
		LogProxy: func(_ context.Context, _ proxy.ProxyLogEntry) error { return nil },
	}
	keyID := id
	pctx := &Ctx{RequestedModel: "gpt-4o", Auth: &auth.ProxyAuthContext{KeyID: &keyID}}
	selected := &routing.SelectedChannel{
		Channel: store.RouteChannel{ID: 1, RouteID: 2, Enabled: true},
		Account: store.Account{ID: 3, Status: "active"},
		Site:    store.Site{ID: 4, Platform: "openai", Status: "active"},
	}
	// Even with Found usage (and thus non-zero estimated cost on the log),
	// failure sink must not advance used_cost.
	usage := ParsedUsage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500, Found: true, Source: "upstream"}
	writeFailureProxyLog(context.Background(), cfg, selected, pctx, "gpt-4o", "/v1/chat/completions", 12, 500, false, usage, 0, "req-cost-fail", "upstream boom")

	if got := readUsedCost(t, db, id); got != 1.0 {
		t.Fatalf("used_cost=%v want 1.0 after failure path", got)
	}
}
