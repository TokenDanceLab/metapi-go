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
