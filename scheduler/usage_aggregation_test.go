package scheduler

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

func TestUsageAggregationProjection_SQLiteAccumulatesIncrementalLogs(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	runUsageAggregationProjectionLifecycle(t, db, "sqlite-"+strings.ReplaceAll(t.Name(), "/", "-"))
}

func TestUsageAggregationProjection_PostgresAccumulatesIncrementalLogs(t *testing.T) {
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

	runUsageAggregationProjectionLifecycle(t, db, "pg-"+strings.ReplaceAll(t.Name(), "/", "-")+"-"+fmt.Sprint(time.Now().UnixNano()))
}

func runUsageAggregationProjectionLifecycle(t *testing.T, db *store.DB, suffix string) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)
	baseWatermark := maxProxyLogID(t, db)
	seedProjectionCheckpoint(t, db, baseWatermark)

	siteID := insertProjectionSite(t, db, suffix, now)
	accountID := insertProjectionAccount(t, db, siteID, suffix, now)
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM proxy_logs WHERE account_id = ?", accountID)
		_, _ = db.Exec("DELETE FROM model_day_usage WHERE site_id = ?", siteID)
		_, _ = db.Exec("DELETE FROM site_hour_usage WHERE site_id = ?", siteID)
		_, _ = db.Exec("DELETE FROM site_day_usage WHERE site_id = ?", siteID)
		_, _ = db.Exec("DELETE FROM sites WHERE id = ?", siteID)
		seedProjectionCheckpoint(t, db, maxProxyLogID(t, db))
	})

	firstLogTime := time.Date(2026, 7, 6, 10, 37, 42, 0, time.UTC)
	secondLogTime := firstLogTime.Add(18 * time.Minute)
	insertProjectionProxyLog(t, db, accountID, "success", "gpt-4.1", 120, 0.42, 230, firstLogTime)

	previousDB := store.GetDB()
	store.OverrideDB(db)
	t.Cleanup(func() { store.OverrideDB(previousDB) })

	s := NewUsageAggregationScheduler(testConfig())
	first := s.RunProjectionPass()
	if first == nil {
		t.Fatal("first projection pass returned nil")
	}
	if first.ProcessedLogs != 1 {
		t.Fatalf("first ProcessedLogs = %d, want 1", first.ProcessedLogs)
	}

	insertProjectionProxyLog(t, db, accountID, "error", "gpt-4.1", 80, 0.11, 0, secondLogTime)
	second := s.RunProjectionPass()
	if second == nil {
		t.Fatal("second projection pass returned nil")
	}
	if second.ProcessedLogs != 1 {
		t.Fatalf("second ProcessedLogs = %d, want 1", second.ProcessedLogs)
	}

	day := firstLogTime.Format("2006-01-02")
	hour := firstLogTime.Truncate(time.Hour).Format(time.RFC3339)
	assertUsageTotals(t, db, siteID, day, hour)
}

func assertUsageTotals(t *testing.T, db *store.DB, siteID int64, day, hour string) {
	t.Helper()

	var dayUsage struct {
		TotalCalls   int     `db:"total_calls"`
		SuccessCalls int     `db:"success_calls"`
		FailedCalls  int     `db:"failed_calls"`
		TotalTokens  int64   `db:"total_tokens"`
		Spend        float64 `db:"total_summary_spend"`
		LatencyMs    int64   `db:"total_latency_ms"`
		LatencyCount int     `db:"latency_count"`
	}
	if err := db.Get(&dayUsage, `SELECT total_calls, success_calls, failed_calls, total_tokens,
		total_summary_spend, total_latency_ms, latency_count
		FROM site_day_usage WHERE site_id = ? AND local_day = ?`, siteID, day); err != nil {
		t.Fatalf("read site_day_usage: %v", err)
	}
	if dayUsage.TotalCalls != 2 || dayUsage.SuccessCalls != 1 || dayUsage.FailedCalls != 1 {
		t.Fatalf("site_day_usage calls = %+v, want total=2 success=1 failed=1", dayUsage)
	}
	if dayUsage.TotalTokens != 200 {
		t.Fatalf("site_day_usage total_tokens = %d, want 200", dayUsage.TotalTokens)
	}
	if math.Abs(dayUsage.Spend-0.53) > 0.000001 {
		t.Fatalf("site_day_usage spend = %.8f, want 0.53", dayUsage.Spend)
	}
	if dayUsage.LatencyMs != 230 || dayUsage.LatencyCount != 1 {
		t.Fatalf("site_day_usage latency = %d/%d, want 230/1", dayUsage.LatencyMs, dayUsage.LatencyCount)
	}

	var hourCalls int
	if err := db.Get(&hourCalls, `SELECT total_calls FROM site_hour_usage WHERE site_id = ? AND bucket_start_utc = ?`, siteID, hour); err != nil {
		t.Fatalf("read site_hour_usage: %v", err)
	}
	if hourCalls != 2 {
		t.Fatalf("site_hour_usage total_calls = %d, want 2", hourCalls)
	}

	var modelUsage struct {
		Model      string  `db:"model"`
		TotalCalls int     `db:"total_calls"`
		Spend      float64 `db:"total_spend"`
	}
	if err := db.Get(&modelUsage, `SELECT model, total_calls, total_spend
		FROM model_day_usage WHERE site_id = ? AND local_day = ? AND model = ?`, siteID, day, "gpt-4.1"); err != nil {
		t.Fatalf("read model_day_usage: %v", err)
	}
	if modelUsage.Model != "gpt-4.1" || modelUsage.TotalCalls != 2 || math.Abs(modelUsage.Spend-0.53) > 0.000001 {
		t.Fatalf("model_day_usage = %+v, want model gpt-4.1 total=2 spend=0.53", modelUsage)
	}
}

func maxProxyLogID(t *testing.T, db *store.DB) int64 {
	t.Helper()
	var id int64
	if err := db.Get(&id, "SELECT COALESCE(MAX(id), 0) FROM proxy_logs"); err != nil {
		t.Fatalf("read max proxy log id: %v", err)
	}
	return id
}

func seedProjectionCheckpoint(t *testing.T, db *store.DB, watermark int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	switch db.Dialect {
	case store.DialectPostgres:
		if _, err := db.Exec(`INSERT INTO analytics_projection_checkpoints
			(projector_key, time_zone, last_proxy_log_id, lease_owner, lease_token, lease_expires_at, created_at, updated_at)
			VALUES (?, 'UTC', ?, NULL, NULL, NULL, ?, ?)
			ON CONFLICT (projector_key) DO UPDATE SET
				last_proxy_log_id = excluded.last_proxy_log_id,
				lease_owner = NULL,
				lease_token = NULL,
				lease_expires_at = NULL,
				last_error = NULL,
				updated_at = excluded.updated_at`, usageProjectorKey, watermark, now, now); err != nil {
			t.Fatalf("seed postgres checkpoint: %v", err)
		}
	default:
		if _, err := db.Exec(`INSERT INTO analytics_projection_checkpoints
			(projector_key, time_zone, last_proxy_log_id, lease_owner, lease_token, lease_expires_at, created_at, updated_at)
			VALUES (?, 'UTC', ?, NULL, NULL, NULL, ?, ?)
			ON CONFLICT (projector_key) DO UPDATE SET
				last_proxy_log_id = excluded.last_proxy_log_id,
				lease_owner = NULL,
				lease_token = NULL,
				lease_expires_at = NULL,
				last_error = NULL,
				updated_at = excluded.updated_at`, usageProjectorKey, watermark, now, now); err != nil {
			t.Fatalf("seed sqlite checkpoint: %v", err)
		}
	}
}

func insertProjectionSite(t *testing.T, db *store.DB, suffix, now string) int64 {
	t.Helper()
	query := `INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`
	args := []any{"usage-" + suffix, "https://usage-" + suffix + ".example.test", "openai", now, now}
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

func insertProjectionAccount(t *testing.T, db *store.DB, siteID int64, suffix, now string) int64 {
	t.Helper()
	query := `INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?, ?)`
	checkinEnabled := any(1)
	if db.Dialect == store.DialectPostgres {
		checkinEnabled = true
	}
	args := []any{siteID, "usage-" + suffix, "sk-usage-" + suffix, checkinEnabled, now, now}
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

func insertProjectionProxyLog(t *testing.T, db *store.DB, accountID int64, status, model string, tokens int64, cost float64, latencyMs int64, createdAt time.Time) int64 {
	t.Helper()
	query := `INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, total_tokens, estimated_cost, latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{accountID, model, model, status, tokens, cost, latencyMs, createdAt.UTC().Format(time.RFC3339)}
	if db.Dialect == store.DialectPostgres {
		var id int64
		if err := db.QueryRow(query+" RETURNING id", args...).Scan(&id); err != nil {
			t.Fatalf("insert postgres proxy log: %v", err)
		}
		return id
	}
	result, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("insert sqlite proxy log: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("sqlite proxy log LastInsertId: %v", err)
	}
	return id
}
