package checkin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func TestCheckinAccount_PostgresDisabledSitePersistsRuntimeHealthAndLog(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("PG_TEST_DSN"))
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set; skipping PostgreSQL integration test")
	}
	db, err := store.Open(store.DialectPostgres, dsn, false)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}

	runDisabledSitePersistenceLifecycle(t, db, "pg-"+strings.ReplaceAll(t.Name(), "/", "-")+"-"+fmt.Sprint(time.Now().UnixNano()))
}

func TestCheckinAccount_PostgresDisabledAccountPersistsRuntimeHealthAndLog(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("PG_TEST_DSN"))
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set; skipping PostgreSQL integration test")
	}
	db, err := store.Open(store.DialectPostgres, dsn, false)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}

	runDisabledAccountPersistenceLifecycle(t, db, "pg-"+strings.ReplaceAll(t.Name(), "/", "-")+"-"+fmt.Sprint(time.Now().UnixNano()))
}

func runDisabledSitePersistenceLifecycle(t *testing.T, db *store.DB, suffix string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	siteID := insertCheckinSite(t, db, suffix, "disabled", now)
	accountID := insertCheckinAccount(t, db, siteID, suffix, now)
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM checkin_logs WHERE account_id = ?", accountID)
		_, _ = db.Exec("DELETE FROM events WHERE related_id = ? AND related_type = ?", accountID, "account")
		_, _ = db.Exec("DELETE FROM sites WHERE id = ?", siteID)
	})

	result := CheckinAccount(&config.Config{}, db.DB, accountID, &CheckinOptions{SkipEvent: true})
	if !result.Success || result.Status != CheckinSkipped || result.Reason != "site_disabled" {
		t.Fatalf("CheckinAccount result = %+v, want skipped site_disabled success", result)
	}

	var extraConfig *string
	if err := db.Get(&extraConfig, "SELECT extra_config FROM accounts WHERE id = ?", accountID); err != nil {
		t.Fatalf("read account extra_config: %v", err)
	}
	if extraConfig == nil || strings.TrimSpace(*extraConfig) == "" {
		t.Fatal("extra_config is empty; want runtimeHealth persisted")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(*extraConfig), &cfg); err != nil {
		t.Fatalf("decode extra_config: %v", err)
	}
	health, ok := cfg["runtimeHealth"].(map[string]any)
	if !ok {
		t.Fatalf("runtimeHealth missing from extra_config: %s", *extraConfig)
	}
	if health["state"] != "disabled" || health["source"] != "checkin" {
		t.Fatalf("runtimeHealth = %#v, want disabled/checkin", health)
	}

	var logCount int
	if err := db.Get(&logCount, "SELECT COUNT(*) FROM checkin_logs WHERE account_id = ? AND status = ? AND message = ?", accountID, "skipped", "site disabled"); err != nil {
		t.Fatalf("count checkin logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("checkin skipped log count = %d, want 1", logCount)
	}
}

func runDisabledAccountPersistenceLifecycle(t *testing.T, db *store.DB, suffix string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	siteID := insertCheckinSite(t, db, suffix, "active", now)
	accountID := insertCheckinAccount(t, db, siteID, suffix, now)
	if _, err := db.Exec(db.Rebind("UPDATE accounts SET status = ? WHERE id = ?"), "disabled", accountID); err != nil {
		t.Fatalf("disable account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM checkin_logs WHERE account_id = ?", accountID)
		_, _ = db.Exec("DELETE FROM events WHERE related_id = ? AND related_type = ?", accountID, "account")
		_, _ = db.Exec("DELETE FROM sites WHERE id = ?", siteID)
	})

	result := CheckinAccount(&config.Config{}, db.DB, accountID, &CheckinOptions{SkipEvent: true})
	if !result.Success || result.Status != CheckinSkipped || result.Reason != "account_disabled" {
		t.Fatalf("CheckinAccount result = %+v, want skipped account_disabled success", result)
	}

	var extraConfig *string
	if err := db.Get(&extraConfig, "SELECT extra_config FROM accounts WHERE id = ?", accountID); err != nil {
		t.Fatalf("read account extra_config: %v", err)
	}
	if extraConfig == nil || strings.TrimSpace(*extraConfig) == "" {
		t.Fatal("extra_config is empty; want runtimeHealth persisted")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(*extraConfig), &cfg); err != nil {
		t.Fatalf("decode extra_config: %v", err)
	}
	health, ok := cfg["runtimeHealth"].(map[string]any)
	if !ok {
		t.Fatalf("runtimeHealth missing from extra_config: %s", *extraConfig)
	}
	if health["state"] != "disabled" || health["source"] != "checkin" {
		t.Fatalf("runtimeHealth = %#v, want disabled/checkin", health)
	}

	var logCount int
	if err := db.Get(&logCount, "SELECT COUNT(*) FROM checkin_logs WHERE account_id = ? AND status = ? AND message = ?", accountID, "skipped", "account disabled"); err != nil {
		t.Fatalf("count checkin logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("checkin skipped log count = %d, want 1", logCount)
	}
}

func insertCheckinSite(t *testing.T, db *store.DB, suffix, status, now string) int64 {
	t.Helper()
	query := `INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	args := []any{"checkin-" + suffix, "https://checkin-" + suffix + ".example.test", "openai", status, now, now}
	if db.Dialect == store.DialectPostgres {
		var id int64
		if err := db.QueryRow(query+" RETURNING id", args...).Scan(&id); err != nil {
			t.Fatalf("insert postgres site: %v", err)
		}
		return id
	}
	result, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("insert sqlite site: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("sqlite site LastInsertId: %v", err)
	}
	return id
}

func insertCheckinAccount(t *testing.T, db *store.DB, siteID int64, suffix, now string) int64 {
	t.Helper()
	query := `INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?, ?)`
	checkinEnabled := any(1)
	if db.Dialect == store.DialectPostgres {
		checkinEnabled = true
	}
	args := []any{siteID, "checkin-" + suffix, "sk-checkin-" + suffix, checkinEnabled, now, now}
	if db.Dialect == store.DialectPostgres {
		var id int64
		if err := db.QueryRow(query+" RETURNING id", args...).Scan(&id); err != nil {
			t.Fatalf("insert postgres account: %v", err)
		}
		return id
	}
	result, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("insert sqlite account: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("sqlite account LastInsertId: %v", err)
	}
	return id
}
