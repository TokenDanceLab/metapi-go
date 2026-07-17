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

func TestStats_SQLiteUsageHeatmapFromSiteHourUsage(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC().Truncate(time.Hour)
	nowStr := now.Format(time.RFC3339)
	bucket := now.Format(time.RFC3339)
	oldBucket := now.Add(-48 * time.Hour).Format(time.RFC3339)

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "heat-site", "https://heat.example.test", "openai", nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var siteID int64
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "heat-site"); err != nil {
		t.Fatalf("site id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO site_hour_usage
		(bucket_start_utc, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bucket, siteID, 7, 6, 1, 700, 0.7, 0.7, 1000, 1, nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert recent hour usage: %v", err)
	}
	_, err = db.Exec(`INSERT INTO site_hour_usage
		(bucket_start_utc, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		oldBucket, siteID, 99, 99, 0, 9900, 9.9, 9.9, 100, 1, nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert old hour usage: %v", err)
	}

	resp := doGet(t, r, "/api/stats/usage-heatmap?days=1&dimension=site")
	if resp.Code != 200 {
		t.Fatalf("usage-heatmap returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["dimension"] != "site" {
		t.Fatalf("dimension = %v, want site", body["dimension"])
	}
	if body["source"] != "site_hour_usage" {
		t.Fatalf("source = %v, want site_hour_usage", body["source"])
	}
	cells, ok := body["cells"].([]any)
	if !ok || len(cells) != 1 {
		t.Fatalf("cells = %#v, want one recent cell", body["cells"])
	}
	cell := cells[0].(map[string]any)
	if cell["label"] != "heat-site" {
		t.Fatalf("label = %v, want heat-site", cell["label"])
	}
	if int64(cell["calls"].(float64)) != 7 {
		t.Fatalf("calls = %v, want 7", cell["calls"])
	}
	if int64(cell["tokens"].(float64)) != 700 {
		t.Fatalf("tokens = %v, want 700", cell["tokens"])
	}
	if _, hasContent := cell["content"]; hasContent {
		t.Fatalf("cells must not include chat content: %#v", cell)
	}
}

func TestStats_SQLiteUsageHeatmapModelFromProxyLogs(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	oldStr := now.Add(-72 * time.Hour).Format(time.RFC3339)

	_, err := db.Exec(`INSERT INTO proxy_logs (model_requested, model_actual, status, prompt_tokens, completion_tokens, total_tokens, estimated_cost, latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "gpt-hot", "gpt-hot", "success", 10, 20, 30, 0.03, 120, nowStr)
	if err != nil {
		t.Fatalf("insert recent model log: %v", err)
	}
	_, err = db.Exec(`INSERT INTO proxy_logs (model_requested, model_actual, status, prompt_tokens, completion_tokens, total_tokens, estimated_cost, latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "gpt-cold", "gpt-cold", "success", 1, 1, 2, 0.01, 50, oldStr)
	if err != nil {
		t.Fatalf("insert old model log: %v", err)
	}

	resp := doGet(t, r, "/api/stats/usage-heatmap?days=1&dimension=model")
	if resp.Code != 200 {
		t.Fatalf("usage-heatmap model returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["dimension"] != "model" {
		t.Fatalf("dimension = %v, want model", body["dimension"])
	}
	if body["source"] != "proxy_logs" {
		t.Fatalf("source = %v, want proxy_logs", body["source"])
	}
	cells, ok := body["cells"].([]any)
	if !ok || len(cells) != 1 {
		t.Fatalf("cells = %#v, want only gpt-hot", body["cells"])
	}
	cell := cells[0].(map[string]any)
	if cell["key"] != "gpt-hot" {
		t.Fatalf("key = %v, want gpt-hot", cell["key"])
	}
	if int64(cell["calls"].(float64)) != 1 {
		t.Fatalf("calls = %v, want 1", cell["calls"])
	}
	if int64(cell["tokens"].(float64)) != 30 {
		t.Fatalf("tokens = %v, want 30", cell["tokens"])
	}
}

func TestStats_SQLiteSlowRequestsRanking(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	oldStr := now.Add(-48 * time.Hour).Format(time.RFC3339)

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "slow-site", "https://slow.example.test", "openai", nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var siteID int64
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "slow-site"); err != nil {
		t.Fatalf("site id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, balance, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, 0, ?, ?)`, siteID, "slow-user", "sk-slow", 1.0, nowStr, nowStr)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	var accountID int64
	if err := db.Get(&accountID, "SELECT id FROM accounts WHERE username = ?", "slow-user"); err != nil {
		t.Fatalf("account id: %v", err)
	}

	// Fast request (below threshold) — must be excluded.
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, latency_ms, first_byte_latency_ms, http_status, request_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, accountID, "gpt-fast", "gpt-fast", "success", 200, 50, 200, "req-fast", nowStr)
	if err != nil {
		t.Fatalf("insert fast log: %v", err)
	}
	// Slow request (ranked).
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, latency_ms, first_byte_latency_ms, http_status, request_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, accountID, "gpt-slow", "gpt-slow", "success", 4500, 1200, 200, "req-slow", nowStr)
	if err != nil {
		t.Fatalf("insert slow log: %v", err)
	}
	// Even slower but outside hours window — excluded.
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, latency_ms, first_byte_latency_ms, http_status, request_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, accountID, "gpt-ancient", "gpt-ancient", "success", 9000, 3000, 200, "req-old", oldStr)
	if err != nil {
		t.Fatalf("insert old slow log: %v", err)
	}

	resp := doGet(t, r, "/api/stats/slow-requests?limit=10&minLatencyMs=1000&hours=24")
	if resp.Code != 200 {
		t.Fatalf("slow-requests returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if int(body["minLatencyMs"].(float64)) != 1000 {
		t.Fatalf("minLatencyMs = %v, want 1000", body["minLatencyMs"])
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want only in-window slow request", body["items"])
	}
	item := items[0].(map[string]any)
	if item["model"] != "gpt-slow" {
		t.Fatalf("model = %v, want gpt-slow", item["model"])
	}
	if int64(item["latencyMs"].(float64)) != 4500 {
		t.Fatalf("latencyMs = %v, want 4500", item["latencyMs"])
	}
	if item["siteName"] != "slow-site" {
		t.Fatalf("siteName = %v, want slow-site", item["siteName"])
	}
	if item["requestId"] != "req-slow" {
		t.Fatalf("requestId = %v, want req-slow", item["requestId"])
	}
	// Privacy: never surface bodies / chat content.
	for _, forbidden := range []string{"requestBody", "responseBody", "content", "messages", "prompt"} {
		if _, ok := item[forbidden]; ok {
			t.Fatalf("slow request item leaked %s: %#v", forbidden, item)
		}
	}
}

func TestStats_UsageHeatmapAndSlowRequestsClampParams(t *testing.T) {
	_, r := setupStatsSQLiteTest(t)

	resp := doGet(t, r, "/api/stats/usage-heatmap?days=999&dimension=weird")
	if resp.Code != 200 {
		t.Fatalf("usage-heatmap clamp returned %d: %s", resp.Code, resp.Body.String())
	}
	var heat map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &heat); err != nil {
		t.Fatalf("unmarshal heatmap: %v", err)
	}
	if int(heat["days"].(float64)) != 31 {
		t.Fatalf("days clamp = %v, want 31", heat["days"])
	}
	if heat["dimension"] != "site" {
		t.Fatalf("dimension fallback = %v, want site", heat["dimension"])
	}
	if int(heat["cellLimit"].(float64)) != usageHeatmapCellLimit {
		t.Fatalf("cellLimit = %v, want %d", heat["cellLimit"], usageHeatmapCellLimit)
	}

	resp = doGet(t, r, "/api/stats/slow-requests?limit=9999&minLatencyMs=-5&hours=999")
	if resp.Code != 200 {
		t.Fatalf("slow-requests clamp returned %d: %s", resp.Code, resp.Body.String())
	}
	var slow map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &slow); err != nil {
		t.Fatalf("unmarshal slow: %v", err)
	}
	if int(slow["limit"].(float64)) != 200 {
		t.Fatalf("limit clamp = %v, want 200", slow["limit"])
	}
	if int(slow["minLatencyMs"].(float64)) != 0 {
		t.Fatalf("minLatencyMs clamp = %v, want 0", slow["minLatencyMs"])
	}
	if int(slow["hours"].(float64)) != 168 {
		t.Fatalf("hours clamp = %v, want 168", slow["hours"])
	}
	if items, ok := slow["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("empty fixtures should yield empty items, got %#v", slow["items"])
	}
}

func seedModelsSurfacesFixture(t *testing.T, db *store.DB) (siteID, accountWithToken, accountWithoutToken, tokenID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "ms-site", "https://ms.example.test", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "ms-site"); err != nil {
		t.Fatalf("site id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, balance, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, 0, ?, ?)`, siteID, "ms-token-user", "sk-ms-token", 9.5, now, now)
	if err != nil {
		t.Fatalf("insert account with token: %v", err)
	}
	if err := db.Get(&accountWithToken, "SELECT id FROM accounts WHERE username = ?", "ms-token-user"); err != nil {
		t.Fatalf("account with token id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, balance, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, 0, ?, ?)`, siteID, "ms-bare-user", "sk-ms-bare", 1.25, now, now)
	if err != nil {
		t.Fatalf("insert bare account: %v", err)
	}
	if err := db.Get(&accountWithoutToken, "SELECT id FROM accounts WHERE username = ?", "ms-bare-user"); err != nil {
		t.Fatalf("bare account id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO account_tokens (account_id, name, token, token_group, value_status, source, enabled, is_default, created_at, updated_at)
		VALUES (?, ?, ?, NULL, 'ready', 'manual', 1, 1, ?, ?)`, accountWithToken, "default", "sk-ms-default", now, now)
	if err != nil {
		t.Fatalf("insert account token: %v", err)
	}
	if err := db.Get(&tokenID, "SELECT id FROM account_tokens WHERE account_id = ?", accountWithToken); err != nil {
		t.Fatalf("token id: %v", err)
	}

	// Token-scoped availability for gpt-4o.
	_, err = db.Exec(`INSERT INTO token_model_availability (token_id, model_name, available, latency_ms, checked_at)
		VALUES (?, ?, 1, ?, ?)`, tokenID, "gpt-4o", 320, now)
	if err != nil {
		t.Fatalf("insert token model availability: %v", err)
	}

	// Account-level availability on bare account (no managed tokens).
	_, err = db.Exec(`INSERT INTO model_availability (account_id, model_name, available, is_manual, latency_ms, checked_at)
		VALUES (?, ?, 1, 0, ?, ?)`, accountWithoutToken, "claude-3-5-sonnet", 410, now)
	if err != nil {
		t.Fatalf("insert bare model availability: %v", err)
	}

	// Account-level availability on token account whose tokens lack group labels.
	_, err = db.Exec(`INSERT INTO model_availability (account_id, model_name, available, is_manual, latency_ms, checked_at)
		VALUES (?, ?, 1, 0, ?, ?)`, accountWithToken, "gpt-4o-mini", 280, now)
	if err != nil {
		t.Fatalf("insert grouped model availability: %v", err)
	}

	// Exact route so marketplace still lists a route-only model name.
	_, err = db.Exec(`INSERT INTO token_routes (model_pattern, route_mode, routing_strategy, enabled, created_at, updated_at)
		VALUES ('route-only-model', 'exact', 'weighted', 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("insert exact route: %v", err)
	}

	// Success-rate signal for gpt-4o.
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, total_tokens, estimated_cost, latency_ms, created_at)
		VALUES (?, ?, ?, 'success', 10, 0.01, 300, ?)`, accountWithToken, "gpt-4o", "gpt-4o", now)
	if err != nil {
		t.Fatalf("insert success proxy log: %v", err)
	}
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, total_tokens, estimated_cost, latency_ms, created_at)
		VALUES (?, ?, ?, 'failed', 5, 0, 900, ?)`, accountWithToken, "gpt-4o", "gpt-4o", now)
	if err != nil {
		t.Fatalf("insert failed proxy log: %v", err)
	}

	return siteID, accountWithToken, accountWithoutToken, tokenID
}

func TestStats_SQLiteMarketplaceFromAvailability(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	_, accountWithToken, accountWithoutToken, tokenID := seedModelsSurfacesFixture(t, db)

	resp := doGet(t, r, "/api/models/marketplace?includePricing=1")
	if resp.Code != 200 {
		t.Fatalf("marketplace returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	models, ok := body["models"].([]any)
	if !ok || len(models) < 3 {
		t.Fatalf("models = %#v, want non-empty fixture models", body["models"])
	}
	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta missing: %#v", body)
	}
	if meta["includePricing"] != true {
		t.Fatalf("includePricing = %v, want true", meta["includePricing"])
	}
	if meta["pricingStatus"] != "unavailable" {
		t.Fatalf("pricingStatus = %v, want unavailable residual label", meta["pricingStatus"])
	}
	if meta["source"] != "db_availability" {
		t.Fatalf("source = %v, want db_availability", meta["source"])
	}

	byName := map[string]map[string]any{}
	for _, raw := range models {
		m := raw.(map[string]any)
		byName[m["name"].(string)] = m
	}
	gpt, ok := byName["gpt-4o"]
	if !ok {
		t.Fatalf("missing gpt-4o in marketplace: %#v", byName)
	}
	if int(gpt["accountCount"].(float64)) < 1 {
		t.Fatalf("gpt-4o accountCount = %v, want >=1", gpt["accountCount"])
	}
	if int(gpt["tokenCount"].(float64)) < 1 {
		t.Fatalf("gpt-4o tokenCount = %v, want >=1", gpt["tokenCount"])
	}
	if gpt["successRate"] == nil {
		t.Fatalf("gpt-4o successRate missing: %#v", gpt)
	}
	if pricing, ok := gpt["pricingSources"].([]any); !ok || len(pricing) != 0 {
		t.Fatalf("pricingSources must be empty residual, got %#v", gpt["pricingSources"])
	}
	accounts, ok := gpt["accounts"].([]any)
	if !ok || len(accounts) == 0 {
		t.Fatalf("gpt-4o accounts empty: %#v", gpt["accounts"])
	}
	acc0 := accounts[0].(map[string]any)
	if int64(acc0["id"].(float64)) != accountWithToken {
		t.Fatalf("account id = %v, want %d", acc0["id"], accountWithToken)
	}
	tokens, ok := acc0["tokens"].([]any)
	if !ok || len(tokens) == 0 {
		t.Fatalf("account tokens empty: %#v", acc0["tokens"])
	}
	if int64(tokens[0].(map[string]any)["id"].(float64)) != tokenID {
		t.Fatalf("token id = %v, want %d", tokens[0].(map[string]any)["id"], tokenID)
	}

	if _, ok := byName["claude-3-5-sonnet"]; !ok {
		t.Fatalf("missing bare-account model claude-3-5-sonnet: %#v", byName)
	}
	if _, ok := byName["route-only-model"]; !ok {
		t.Fatalf("missing exact route model route-only-model: %#v", byName)
	}
	claude := byName["claude-3-5-sonnet"]
	foundBare := false
	for _, raw := range claude["accounts"].([]any) {
		if int64(raw.(map[string]any)["id"].(float64)) == accountWithoutToken {
			foundBare = true
		}
	}
	if !foundBare {
		t.Fatalf("bare account %d missing from claude accounts: %#v", accountWithoutToken, claude["accounts"])
	}
}

func TestStats_SQLiteTokenCandidatesMaps(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	_, accountWithToken, accountWithoutToken, tokenID := seedModelsSurfacesFixture(t, db)

	resp := doGet(t, r, "/api/models/token-candidates")
	if resp.Code != 200 {
		t.Fatalf("token-candidates returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	models, ok := body["models"].(map[string]any)
	if !ok {
		t.Fatalf("models map missing: %#v", body["models"])
	}
	gptCandidates, ok := models["gpt-4o"].([]any)
	if !ok || len(gptCandidates) != 1 {
		t.Fatalf("gpt-4o candidates = %#v, want one token candidate", models["gpt-4o"])
	}
	c0 := gptCandidates[0].(map[string]any)
	if int64(c0["tokenId"].(float64)) != tokenID {
		t.Fatalf("tokenId = %v, want %d", c0["tokenId"], tokenID)
	}
	if int64(c0["accountId"].(float64)) != accountWithToken {
		t.Fatalf("accountId = %v, want %d", c0["accountId"], accountWithToken)
	}

	without, ok := body["modelsWithoutToken"].(map[string]any)
	if !ok {
		t.Fatalf("modelsWithoutToken missing: %#v", body["modelsWithoutToken"])
	}
	bare, ok := without["claude-3-5-sonnet"].([]any)
	if !ok || len(bare) != 1 {
		t.Fatalf("modelsWithoutToken claude = %#v, want bare account", without["claude-3-5-sonnet"])
	}
	if int64(bare[0].(map[string]any)["accountId"].(float64)) != accountWithoutToken {
		t.Fatalf("bare accountId = %v, want %d", bare[0].(map[string]any)["accountId"], accountWithoutToken)
	}

	missingGroups, ok := body["modelsMissingTokenGroups"].(map[string]any)
	if !ok {
		t.Fatalf("modelsMissingTokenGroups missing: %#v", body["modelsMissingTokenGroups"])
	}
	groupRows, ok := missingGroups["gpt-4o-mini"].([]any)
	if !ok || len(groupRows) != 1 {
		t.Fatalf("modelsMissingTokenGroups gpt-4o-mini = %#v", missingGroups["gpt-4o-mini"])
	}
	g0 := groupRows[0].(map[string]any)
	if g0["groupCoverageUncertain"] != true {
		t.Fatalf("groupCoverageUncertain = %v, want true", g0["groupCoverageUncertain"])
	}

	endpointTypes, ok := body["endpointTypesByModel"].(map[string]any)
	if !ok {
		t.Fatalf("endpointTypesByModel missing: %#v", body["endpointTypesByModel"])
	}
	if types, ok := endpointTypes["gpt-4o"].([]any); !ok || len(types) == 0 {
		t.Fatalf("endpointTypes gpt-4o = %#v", endpointTypes["gpt-4o"])
	}
	if types, ok := endpointTypes["claude-3-5-sonnet"].([]any); !ok || len(types) == 0 || types[0] != "anthropic" {
		t.Fatalf("endpointTypes claude = %#v, want anthropic", endpointTypes["claude-3-5-sonnet"])
	}
}

func TestStats_SQLiteModelCheckNoFakeSuccess(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "check-site", "https://check.example.test", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var siteID int64
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "check-site"); err != nil {
		t.Fatalf("site id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', 0, ?, ?)`, siteID, "check-user", "sk-check", now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	var accountID int64
	if err := db.Get(&accountID, "SELECT id FROM accounts WHERE username = ?", "check-user"); err != nil {
		t.Fatalf("account id: %v", err)
	}

	// Invalid id → Pattern C error, no fake success.
	resp := doPostJSON(t, r, "/api/models/check/not-a-number", map[string]any{})
	if resp.Code != 200 {
		t.Fatalf("invalid id status = %d, want 200 with body error", resp.Code)
	}
	var invalidBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &invalidBody); err != nil {
		t.Fatalf("unmarshal invalid: %v", err)
	}
	if invalidBody["success"] != false {
		t.Fatalf("invalid id success = %v, want false", invalidBody["success"])
	}
	if invalidBody["error"] != "Invalid account id" {
		t.Fatalf("invalid id error = %v", invalidBody["error"])
	}

	// Missing account → success=false.
	resp = doPostJSON(t, r, "/api/models/check/999999", map[string]any{})
	if resp.Code != 200 {
		t.Fatalf("missing account status = %d", resp.Code)
	}
	var missingBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &missingBody); err != nil {
		t.Fatalf("unmarshal missing: %v", err)
	}
	if missingBody["success"] != false {
		t.Fatalf("missing account success = %v, want false (no fake success)", missingBody["success"])
	}

	// Real account against unreachable upstream URL: must not return success=true.
	resp = doPostJSON(t, r, "/api/models/check/"+strconv.FormatInt(accountID, 10), map[string]any{})
	if resp.Code != 200 {
		t.Fatalf("model check status = %d: %s", resp.Code, resp.Body.String())
	}
	var checkBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &checkBody); err != nil {
		t.Fatalf("unmarshal check: %v", err)
	}
	if checkBody["success"] != false {
		t.Fatalf("unreachable upstream success = %v, want false (no fake success); body=%#v", checkBody["success"], checkBody)
	}
	refresh, ok := checkBody["refresh"].(map[string]any)
	if !ok {
		t.Fatalf("refresh missing: %#v", checkBody)
	}
	if refresh["status"] == "success" {
		t.Fatalf("refresh.status must not be success for unreachable upstream: %#v", refresh)
	}
	if refresh["errorCode"] == nil || refresh["errorCode"] == "" {
		t.Fatalf("refresh.errorCode required on failure: %#v", refresh)
	}
}
