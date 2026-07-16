package admin

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/store"
)

func setupStatsPostgresTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("PG_TEST_DSN"))
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set; skipping PostgreSQL integration test")
	}

	db, err := store.Open(store.DialectPostgres, dsn, false)
	if err != nil {
		t.Fatalf("failed to open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	RegisterStatsRoutes(r, db.DB)
	return db, r
}

func setupStatsSQLiteTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	r := chi.NewRouter()
	RegisterStatsRoutes(r, db.DB)
	return db, r
}

func TestStats_PostgresProxyLogsFilteredTotals(t *testing.T) {
	db, r := setupStatsPostgresTest(t)

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	modelMatch := "pg-stats-match-" + suffix
	modelOther := "pg-stats-other-" + suffix
	now := time.Now().UTC().Format(time.RFC3339)
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM proxy_logs WHERE model_requested IN (?, ?)", modelMatch, modelOther)
	})

	_, err := db.Exec(
		`INSERT INTO proxy_logs (model_requested, model_actual, status, total_tokens, estimated_cost, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		modelMatch,
		modelMatch,
		"success",
		123,
		0.456789,
		now,
	)
	if err != nil {
		t.Fatalf("insert matching proxy log: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO proxy_logs (model_requested, model_actual, status, total_tokens, estimated_cost, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		modelOther,
		modelOther,
		"failed",
		99,
		1.25,
		now,
	)
	if err != nil {
		t.Fatalf("insert non-matching proxy log: %v", err)
	}

	resp := doGet(t, r, "/api/stats/proxy-logs?view=full&search="+modelMatch)
	if resp.Code != 200 {
		t.Fatalf("proxy logs returned %d: %s", resp.Code, resp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one matching log", body["items"])
	}
	if got := int(body["total"].(float64)); got != 1 {
		t.Fatalf("total = %d, want 1", got)
	}

	summary, ok := body["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary missing or wrong type: %#v", body["summary"])
	}
	if got := int(summary["totalCount"].(float64)); got != 1 {
		t.Fatalf("summary.totalCount = %d, want 1", got)
	}
	if got := int(summary["successCount"].(float64)); got != 1 {
		t.Fatalf("summary.successCount = %d, want 1", got)
	}
	if got := int(summary["failedCount"].(float64)); got != 0 {
		t.Fatalf("summary.failedCount = %d, want 0", got)
	}
}

func TestStats_SQLiteProxyLogsEffectiveTokenTotals(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC().Format(time.RFC3339)

	// total_tokens present → use it (do not also add prompt+completion).
	_, err := db.Exec(`INSERT INTO proxy_logs (model_requested, model_actual, status, prompt_tokens, completion_tokens, total_tokens, estimated_cost, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "full-total", "full-total", "success", 40, 60, 100, 0.1, now)
	if err != nil {
		t.Fatalf("insert full-total log: %v", err)
	}
	// total_tokens missing → fallback prompt+completion.
	_, err = db.Exec(`INSERT INTO proxy_logs (model_requested, model_actual, status, prompt_tokens, completion_tokens, estimated_cost, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "partial-tokens", "partial-tokens", "success", 25, 15, 0.05, now)
	if err != nil {
		t.Fatalf("insert partial log: %v", err)
	}

	resp := doGet(t, r, "/api/stats/proxy-logs?view=meta")
	if resp.Code != 200 {
		t.Fatalf("proxy logs meta returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	summary := body["summary"].(map[string]any)
	// 100 (prefer total) + 40 (25+15 fallback) = 140; never 100+40+60=200.
	if got := int64(summary["totalTokensAll"].(float64)); got != 140 {
		t.Fatalf("summary.totalTokensAll = %d, want 140 (no double count)", got)
	}
	if got := int(summary["totalCount"].(float64)); got != 2 {
		t.Fatalf("summary.totalCount = %d, want 2", got)
	}
}

func TestStats_SQLiteDashboardProxy24hTokens(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "stats-site", "https://stats.example.test", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var siteID int64
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "stats-site"); err != nil {
		t.Fatalf("site id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, balance, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, 0, ?, ?)`, siteID, "stats-user", "sk-stats", 12.5, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	var accountID int64
	if err := db.Get(&accountID, "SELECT id FROM accounts WHERE username = ?", "stats-user"); err != nil {
		t.Fatalf("account id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, prompt_tokens, completion_tokens, total_tokens, estimated_cost, latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, accountID, "gpt-x", "gpt-x", "success", 10, 20, 0, 0.2, 100, now)
	if err != nil {
		t.Fatalf("insert proxy log: %v", err)
	}

	resp := doGet(t, r, "/api/stats/dashboard?view=full")
	if resp.Code != 200 {
		t.Fatalf("dashboard returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	proxy24h, ok := body["proxy24h"].(map[string]any)
	if !ok {
		t.Fatalf("proxy24h missing: %#v", body)
	}
	if got := int64(proxy24h["totalTokens"].(float64)); got != 30 {
		t.Fatalf("proxy24h.totalTokens = %d, want 30 from prompt+completion", got)
	}
	if got := int(proxy24h["total"].(float64)); got != 1 {
		t.Fatalf("proxy24h.total = %d, want 1", got)
	}
	if got := int(proxy24h["success"].(float64)); got != 1 {
		t.Fatalf("proxy24h.success = %d, want 1", got)
	}
}

func TestStats_SQLiteModelBySiteRespectsDaysFilter(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	today := now.Format("2006-01-02")
	oldDay := now.AddDate(0, 0, -30).Format("2006-01-02")

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "model-site", "https://model.example.test", "openai", nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var siteID int64
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "model-site"); err != nil {
		t.Fatalf("site id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO model_day_usage
		(local_day, site_id, model, total_calls, success_calls, failed_calls, total_tokens, total_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		today, siteID, "recent-model", 3, 3, 0, 300, 0.3, 0, 0, nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert recent model usage: %v", err)
	}
	_, err = db.Exec(`INSERT INTO model_day_usage
		(local_day, site_id, model, total_calls, success_calls, failed_calls, total_tokens, total_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		oldDay, siteID, "old-model", 9, 9, 0, 900, 0.9, 0, 0, nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert old model usage: %v", err)
	}

	resp := doGet(t, r, "/api/stats/model-by-site?days=7&siteId="+strconv.FormatInt(siteID, 10))
	if resp.Code != 200 {
		t.Fatalf("model-by-site returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	models, ok := body["models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("models = %#v, want only recent-model", body["models"])
	}
	item := models[0].(map[string]any)
	if item["model"] != "recent-model" {
		t.Fatalf("model = %v, want recent-model", item["model"])
	}
	if int(item["tokens"].(float64)) != 300 {
		t.Fatalf("tokens = %v, want 300", item["tokens"])
	}
}

func TestStats_SQLiteSiteTrendFromProjectedUsage(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	today := now.Format("2006-01-02")

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "trend-site", "https://trend.example.test", "openai", nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var siteID int64
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "trend-site"); err != nil {
		t.Fatalf("site id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO site_day_usage
		(local_day, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		today, siteID, 4, 3, 1, 400, 1.25, 1.25, 100, 1, nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert site day usage: %v", err)
	}

	resp := doGet(t, r, "/api/stats/site-trend?days=7")
	if resp.Code != 200 {
		t.Fatalf("site-trend returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := body["trend"].([]any)
	if !ok || len(trend) != 1 {
		t.Fatalf("trend = %#v, want one day", body["trend"])
	}
	entry := trend[0].(map[string]any)
	if entry["date"] != today {
		t.Fatalf("date = %v, want %s", entry["date"], today)
	}
	sites := entry["sites"].(map[string]any)
	site := sites["trend-site"].(map[string]any)
	if site["calls"].(float64) != 4 {
		t.Fatalf("calls = %v, want 4", site["calls"])
	}
	if site["spend"].(float64) != 1.25 {
		t.Fatalf("spend = %v, want 1.25", site["spend"])
	}
}
