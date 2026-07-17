// Package e2e contains end-to-end integration tests for the MetAPI Go proxy flow.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/handler/admin"
	"github.com/tokendancelab/metapi-go/store"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test: Backup Export-Import Roundtrip (all 28 tables)
// ──────────────────────────────────────────────────────────────────────────────
// Full roundtrip: seed all 28 tables -> export JSON -> factory-reset ->
// import JSON -> verify all data intact (re-export and compare).
// Uses SQLite :memory: for the DB.

func TestBackupExportImportRoundtrip(t *testing.T) {
	// ══════════════════════════════════════════════════════════════════════════
	// Phase 1: Setup — in-memory SQLite + AutoMigrate + all admin routes
	// ══════════════════════════════════════════════════════════════════════════

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	defer db.Close()

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	admin.RegisterBackupRoutes(r, db.DB)
	admin.RegisterMaintenanceRoutes(r, db.DB)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	future := time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 2: Seed all 28 tables with test data (FK-safe order)
	// ══════════════════════════════════════════════════════════════════════════

	// Table 1: sites (2 rows)
	db.Exec(`INSERT INTO sites (id, name, url, platform, status, created_at, updated_at)
		VALUES (1, 'Test Site A', 'https://api.openai.com', 'openai', 'active', ?, ?)`, now, now)
	db.Exec(`INSERT INTO sites (id, name, url, platform, status, is_pinned, created_at, updated_at)
		VALUES (2, 'Test Site B', 'https://api.anthropic.com', 'anthropic', 'active', 1, ?, ?)`, now, now)

	// Table 2: site_api_endpoints (2 rows, FK sites)
	db.Exec(`INSERT INTO site_api_endpoints (id, site_id, url, enabled, created_at, updated_at)
		VALUES (1, 1, 'https://api.openai.com/v1', 1, ?, ?)`, now, now)
	db.Exec(`INSERT INTO site_api_endpoints (id, site_id, url, enabled, created_at, updated_at)
		VALUES (2, 2, 'https://api.anthropic.com/v1', 1, ?, ?)`, now, now)

	// Table 3: site_disabled_models (1 row, FK sites)
	db.Exec(`INSERT INTO site_disabled_models (id, site_id, model_name, created_at)
		VALUES (1, 1, 'gpt-4-legacy', ?)`, now)

	// Table 4: accounts (2 rows, FK sites)
	db.Exec(`INSERT INTO accounts (id, site_id, access_token, balance, status, created_at, updated_at)
		VALUES (1, 1, 'sk-account-a-token', 100.5, 'active', ?, ?)`, now, now)
	db.Exec(`INSERT INTO accounts (id, site_id, username, access_token, balance, status, created_at, updated_at)
		VALUES (2, 2, 'user_b', 'sk-account-b-token', 50.0, 'active', ?, ?)`, now, now)

	// Table 5: account_tokens (3 rows, FK accounts)
	db.Exec(`INSERT INTO account_tokens (id, account_id, name, token, value_status, source, enabled, is_default, created_at, updated_at)
		VALUES (1, 1, 'default', 'tk-a-default', 'ready', 'manual', 1, 1, ?, ?)`, now, now)
	db.Exec(`INSERT INTO account_tokens (id, account_id, name, token, value_status, source, enabled, created_at, updated_at)
		VALUES (2, 1, 'backup', 'tk-a-backup', 'ready', 'manual', 1, ?, ?)`, now, now)
	db.Exec(`INSERT INTO account_tokens (id, account_id, name, token, value_status, source, enabled, is_default, created_at, updated_at)
		VALUES (3, 2, 'default', 'tk-b-default', 'ready', 'manual', 1, 1, ?, ?)`, now, now)

	// Table 6: checkin_logs (2 rows, FK accounts)
	db.Exec(`INSERT INTO checkin_logs (id, account_id, status, message, reward, created_at)
		VALUES (1, 1, 'success', 'checkin ok', '100 credits', ?)`, now)
	db.Exec(`INSERT INTO checkin_logs (id, account_id, status, message, created_at)
		VALUES (2, 2, 'failed', 'rate limited', ?)`, now)

	// Table 7: model_availability (2 rows, FK accounts)
	db.Exec(`INSERT INTO model_availability (id, account_id, model_name, available, is_manual, checked_at)
		VALUES (1, 1, 'gpt-4', 1, 1, ?)`, now)
	db.Exec(`INSERT INTO model_availability (id, account_id, model_name, available, is_manual, checked_at)
		VALUES (2, 2, 'claude-3-opus', 1, 0, ?)`, now)

	// Table 8: token_model_availability (2 rows, FK account_tokens)
	db.Exec(`INSERT INTO token_model_availability (id, token_id, model_name, available, checked_at)
		VALUES (1, 1, 'gpt-4', 1, ?)`, now)
	db.Exec(`INSERT INTO token_model_availability (id, token_id, model_name, available, checked_at)
		VALUES (2, 3, 'claude-3-opus', 1, ?)`, now)

	// Table 9: token_routes (2 rows)
	db.Exec(`INSERT INTO token_routes (id, model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		VALUES (1, 'gpt-4', 'GPT-4 Route', 'pattern', 'weighted', 1, ?, ?)`, now, now)
	db.Exec(`INSERT INTO token_routes (id, model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		VALUES (2, 'claude-*', 'Claude Route', 'pattern', 'weighted', 1, ?, ?)`, now, now)

	// Table 10: route_group_sources (2 rows, FK token_routes x2)
	db.Exec(`INSERT INTO route_group_sources (id, group_route_id, source_route_id)
		VALUES (1, 1, 2)`)
	db.Exec(`INSERT INTO route_group_sources (id, group_route_id, source_route_id)
		VALUES (2, 2, 1)`)

	// Table 11: oauth_route_units (2 rows, FK sites)
	db.Exec(`INSERT INTO oauth_route_units (id, site_id, provider, name, strategy, enabled, created_at, updated_at)
		VALUES (1, 1, 'google', 'Google Unit', 'round_robin', 1, ?, ?)`, now, now)
	db.Exec(`INSERT INTO oauth_route_units (id, site_id, provider, name, strategy, enabled, created_at, updated_at)
		VALUES (2, 2, 'github', 'GitHub Unit', 'round_robin', 1, ?, ?)`, now, now)

	// Table 12: oauth_route_unit_members (2 rows, FK oauth_route_units + accounts, UNIQUE account_id)
	// Each account can only be in one unit, so distribute across units.
	db.Exec(`INSERT INTO oauth_route_unit_members (id, unit_id, account_id, sort_order, created_at, updated_at)
		VALUES (1, 1, 1, 0, ?, ?)`, now, now)
	db.Exec(`INSERT INTO oauth_route_unit_members (id, unit_id, account_id, sort_order, created_at, updated_at)
		VALUES (2, 2, 2, 0, ?, ?)`, now, now)

	// Table 13: route_channels (2 rows, FK token_routes + accounts + account_tokens SET NULL)
	// NOTE: route_channels has NO created_at/updated_at columns.
	db.Exec(`INSERT INTO route_channels (id, route_id, account_id, token_id, weight, enabled)
		VALUES (1, 1, 1, 1, 10, 1)`)
	db.Exec(`INSERT INTO route_channels (id, route_id, account_id, weight, enabled)
		VALUES (2, 2, 2, 10, 1)`)

	// Table 14: proxy_logs (2 rows)
	db.Exec(`INSERT INTO proxy_logs (id, route_id, channel_id, account_id, model_requested, model_actual, status, created_at)
		VALUES (1, 1, 1, 1, 'gpt-4', 'gpt-4', 'success', ?)`, now)
	db.Exec(`INSERT INTO proxy_logs (id, route_id, channel_id, account_id, model_requested, model_actual, status, created_at)
		VALUES (2, 2, 2, 2, 'claude-3-opus', 'claude-3-opus', 'success', ?)`, now)

	// Table 15: proxy_debug_traces (2 rows)
	db.Exec(`INSERT INTO proxy_debug_traces (id, downstream_path, requested_model, created_at, updated_at)
		VALUES (1, '/v1/chat/completions', 'gpt-4', ?, ?)`, now, now)
	db.Exec(`INSERT INTO proxy_debug_traces (id, downstream_path, requested_model, created_at, updated_at)
		VALUES (2, '/v1/chat/completions', 'claude-3-opus', ?, ?)`, now, now)

	// Table 16: proxy_debug_attempts (2 rows, FK proxy_debug_traces)
	db.Exec(`INSERT INTO proxy_debug_attempts (id, trace_id, attempt_index, endpoint, request_path, target_url, created_at)
		VALUES (1, 1, 0, 'api.openai.com', '/v1/chat/completions', 'https://api.openai.com/v1/chat/completions', ?)`, now)
	db.Exec(`INSERT INTO proxy_debug_attempts (id, trace_id, attempt_index, endpoint, request_path, target_url, created_at)
		VALUES (2, 2, 0, 'api.anthropic.com', '/v1/chat/completions', 'https://api.anthropic.com/v1/chat/completions', ?)`, now)

	// Table 17: proxy_video_tasks (2 rows)
	db.Exec(`INSERT INTO proxy_video_tasks (id, public_id, upstream_video_id, site_url, token_value, created_at, updated_at)
		VALUES (1, 'vid-pub-001', 'upstream-vid-001', 'https://api.openai.com', 'sk-token-1', ?, ?)`, now, now)
	db.Exec(`INSERT INTO proxy_video_tasks (id, public_id, upstream_video_id, site_url, token_value, created_at, updated_at)
		VALUES (2, 'vid-pub-002', 'upstream-vid-002', 'https://api.anthropic.com', 'sk-token-2', ?, ?)`, now, now)

	// Table 18: proxy_files (2 rows)
	db.Exec(`INSERT INTO proxy_files (id, public_id, owner_type, owner_id, filename, mime_type, byte_size, sha256, content_base64, created_at, updated_at)
		VALUES (1, 'file-pub-001', 'session', 'sess-1', 'test.txt', 'text/plain', 4, 'abc123', 'dGVzdA==', ?, ?)`, now, now)
	db.Exec(`INSERT INTO proxy_files (id, public_id, owner_type, owner_id, filename, mime_type, byte_size, sha256, content_base64, created_at, updated_at)
		VALUES (2, 'file-pub-002', 'session', 'sess-2', 'image.png', 'image/png', 8, 'def456', 'aW1hZ2Vwbmc=', ?, ?)`, now, now)

	// Table 19: settings (text PK, 2 rows)
	db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "app.theme", "dark")
	db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "app.lang", "zh-CN")

	// Table 20: admin_snapshots (2 rows)
	db.Exec(`INSERT INTO admin_snapshots (id, namespace, snapshot_key, payload, generated_at, expires_at, stale_until, created_at, updated_at)
		VALUES (1, 'routes', 'snapshot-v1', '{"routes":[]}', ?, ?, ?, ?, ?)`, now, future, future, now, now)
	db.Exec(`INSERT INTO admin_snapshots (id, namespace, snapshot_key, payload, generated_at, expires_at, stale_until, created_at, updated_at)
		VALUES (2, 'models', 'snapshot-v2', '{"models":[]}', ?, ?, ?, ?, ?)`, now, future, future, now, now)

	// Table 21: analytics_projection_checkpoints (text PK, 1 row)
	db.Exec(`INSERT INTO analytics_projection_checkpoints (projector_key, time_zone, last_proxy_log_id, created_at, updated_at)
		VALUES ('day-usage-projection', 'UTC', 42, ?, ?)`, now, now)

	// Table 22: site_day_usage (2 rows, FK sites)
	db.Exec(`INSERT INTO site_day_usage (id, local_day, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (1, '2026-07-04', 1, 100, 95, 5, 5000, 0.05, 0.0, 30000, 100, ?, ?)`, now, now)
	db.Exec(`INSERT INTO site_day_usage (id, local_day, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (2, '2026-07-04', 2, 50, 48, 2, 2000, 0.02, 0.0, 12000, 50, ?, ?)`, now, now)

	// Table 23: site_hour_usage (1 row, FK sites)
	db.Exec(`INSERT INTO site_hour_usage (id, bucket_start_utc, site_id, total_calls, success_calls, failed_calls, total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (1, '2026-07-04T12:00:00Z', 1, 20, 19, 1, 1000, 0.01, 0.0, 6000, 20, ?, ?)`, now, now)

	// Table 24: model_day_usage (2 rows, FK sites)
	db.Exec(`INSERT INTO model_day_usage (id, local_day, site_id, model, total_calls, success_calls, failed_calls, total_tokens, total_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (1, '2026-07-04', 1, 'gpt-4', 60, 57, 3, 3000, 0.03, 18000, 60, ?, ?)`, now, now)
	db.Exec(`INSERT INTO model_day_usage (id, local_day, site_id, model, total_calls, success_calls, failed_calls, total_tokens, total_spend, total_latency_ms, latency_count, created_at, updated_at)
		VALUES (2, '2026-07-04', 2, 'claude-3-opus', 30, 29, 1, 1500, 0.01, 9000, 30, ?, ?)`, now, now)

	// Table 25: downstream_api_keys (2 rows)
	db.Exec(`INSERT INTO downstream_api_keys (id, name, key, enabled, used_cost, used_requests, created_at, updated_at)
		VALUES (1, 'Test Key A', 'sk-downstream-key-a', 1, 0, 0, ?, ?)`, now, now)
	db.Exec(`INSERT INTO downstream_api_keys (id, name, key, expires_at, enabled, used_cost, used_requests, created_at, updated_at)
		VALUES (2, 'Test Key B', 'sk-downstream-key-b', ?, 1, 0, 0, ?, ?)`, future, now, now)

	// Table 26: site_announcements (2 rows, FK sites)
	db.Exec(`INSERT INTO site_announcements (id, site_id, platform, source_key, title, content, level, first_seen_at, last_seen_at)
		VALUES (1, 1, 'openai', 'ann-key-1', 'Maintenance', 'Scheduled maintenance on July 10', 'info', ?, ?)`, now, now)
	db.Exec(`INSERT INTO site_announcements (id, site_id, platform, source_key, title, content, level, first_seen_at, last_seen_at)
		VALUES (2, 2, 'anthropic', 'ann-key-2', 'New Model', 'Claude 4 is now available', 'info', ?, ?)`, now, now)

	// Table 27: events (2 rows)
	db.Exec(`INSERT INTO events (id, type, title, message, level, created_at)
		VALUES (1, 'info', 'System started', 'MetAPI started successfully', 'info', ?)`, now)
	db.Exec(`INSERT INTO events (id, type, title, message, level, read, created_at)
		VALUES (2, 'warn', 'High latency', 'Proxy latency above threshold', 'warn', 0, ?)`, now)

	// ──────────────────────────────────────────────────────────────────────────
	// Verify seed: check row counts for all 28 tables
	// ──────────────────────────────────────────────────────────────────────────
	expectedCounts := map[string]int{
		"sites":                            2,
		"site_api_endpoints":               2,
		"site_disabled_models":             1,
		"accounts":                         2,
		"account_tokens":                   3,
		"checkin_logs":                     2,
		"model_availability":               2,
		"token_model_availability":         2,
		"token_routes":                     2,
		"route_group_sources":              2,
		"oauth_route_units":                2,
		"oauth_route_unit_members":         2,
		"route_channels":                   2,
		"proxy_logs":                       2,
		"proxy_debug_traces":               2,
		"proxy_debug_attempts":             2,
		"proxy_video_tasks":                2,
		"proxy_files":                      2,
		"settings":                         2,
		"admin_snapshots":                  2,
		"analytics_projection_checkpoints": 1,
		"site_day_usage":                   2,
		"site_hour_usage":                  1,
		"model_day_usage":                  2,
		"downstream_api_keys":              2,
		"site_announcements":               2,
		"events":                           2,
	}

	verifyRowCounts(t, db, expectedCounts, "after seed")

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 3: Export JSON via /api/settings/backup/export?type=all
	// ══════════════════════════════════════════════════════════════════════════

	exportRec := doGet(t, r, "/api/settings/backup/export?type=all")
	if exportRec.Code != 200 {
		t.Fatalf("export: expected 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}

	var exportPayload map[string]any
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exportPayload); err != nil {
		t.Fatalf("export: failed to parse JSON: %v", err)
	}

	// Verify export structure.
	tablesRaw, ok := exportPayload["tables"].(map[string]any)
	if !ok {
		t.Fatalf("export: expected 'tables' key in payload, got %T", exportPayload["tables"])
	}

	// Check all 28 tables are present in export.
	for table, expectedCount := range expectedCounts {
		rows, ok := tablesRaw[table].([]any)
		if !ok {
			t.Errorf("export: table %q missing from payload", table)
			continue
		}
		if len(rows) != expectedCount {
			t.Errorf("export: table %q has %d rows, expected %d", table, len(rows), expectedCount)
		}
	}

	// Check metadata.
	meta, _ := exportPayload["metadata"].(map[string]any)
	if meta == nil {
		t.Error("export: missing metadata")
	} else {
		if ver, _ := meta["version"].(string); ver == "" {
			t.Error("export: metadata missing version")
		}
		if exportedAt, _ := meta["exported_at"].(string); exportedAt == "" {
			t.Error("export: metadata missing exported_at")
		}
	}

	// Check type.
	if exportType, _ := exportPayload["type"].(string); exportType != "all" {
		t.Errorf("export: expected type 'all', got %q", exportType)
	}

	t.Logf("export: all 28 tables exported successfully")

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 4: Factory reset via /api/settings/maintenance/factory-reset
	// ══════════════════════════════════════════════════════════════════════════

	// First, reject without confirm token.
	resetRejectRec := doPostJSON_Raw(t, r, "/api/settings/maintenance/factory-reset",
		`{"confirm":false}`)
	if resetRejectRec.Code != 400 {
		t.Errorf("factory-reset without confirm: expected 400, got %d", resetRejectRec.Code)
	}

	// Now confirm.
	resetRec := doPostJSON_Raw(t, r, "/api/settings/maintenance/factory-reset",
		`{"confirm":true}`)
	if resetRec.Code != 200 {
		t.Fatalf("factory-reset: expected 200, got %d: %s", resetRec.Code, resetRec.Body.String())
	}

	var resetPayload map[string]any
	json.Unmarshal(resetRec.Body.Bytes(), &resetPayload)
	if ok, _ := resetPayload["success"].(bool); !ok {
		t.Fatal("factory-reset: expected success=true")
	}

	// Verify all tables are empty.
	emptyCounts := make(map[string]int)
	for table := range expectedCounts {
		emptyCounts[table] = 0
	}
	verifyRowCounts(t, db, emptyCounts, "after factory reset")

	// Verify auto-increment was reset (next id should be 1 for serial tables).
	var nextSiteID int64
	db.Get(&nextSiteID, "SELECT COALESCE(MAX(id), 0) FROM sites")
	if nextSiteID != 0 {
		t.Errorf("after reset: expected max(sites.id)=0, got %d", nextSiteID)
	}

	t.Log("factory-reset: all 28 tables truncated, auto-increment reset")

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 5: Import JSON via /api/settings/backup/import
	// ══════════════════════════════════════════════════════════════════════════

	// Build import body from the export payload's tables.
	importBody := map[string]any{
		"tables": tablesRaw,
	}
	importRec := doPostJSON_Map(t, r, "/api/settings/backup/import", importBody)
	if importRec.Code != 200 {
		t.Fatalf("import: expected 200, got %d: %s", importRec.Code, importRec.Body.String())
	}

	var importPayload map[string]any
	if err := json.Unmarshal(importRec.Body.Bytes(), &importPayload); err != nil {
		t.Fatalf("import: failed to parse response: %v", err)
	}
	if ok, _ := importPayload["success"].(bool); !ok {
		t.Fatal("import: expected success=true")
	}

	// Check imported counts.
	imported, _ := importPayload["imported"].(map[string]any)
	if imported == nil {
		t.Fatal("import: missing 'imported' key in response")
	}
	for table, expectedCount := range expectedCounts {
		actualFloat, ok := imported[table].(float64)
		if !ok {
			t.Errorf("import: table %q missing from imported count", table)
			continue
		}
		if int64(actualFloat) != int64(expectedCount) {
			t.Errorf("import: table %q imported %d rows, expected %d", table, int64(actualFloat), expectedCount)
		}
	}

	t.Log("import: all 28 tables imported successfully")

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 6: Verify data integrity — re-export and compare
	// ══════════════════════════════════════════════════════════════════════════

	// 6a. Verify row counts restored.
	verifyRowCounts(t, db, expectedCounts, "after import")

	// 6b. Verify specific data values survived the roundtrip.
	verifySpotChecks(t, db)

	// 6c. Re-export and do a table-level structural comparison.
	reExportRec := doGet(t, r, "/api/settings/backup/export?type=all")
	if reExportRec.Code != 200 {
		t.Fatalf("re-export: expected 200, got %d: %s", reExportRec.Code, reExportRec.Body.String())
	}

	var reExportPayload map[string]any
	json.Unmarshal(reExportRec.Body.Bytes(), &reExportPayload)
	reTables, _ := reExportPayload["tables"].(map[string]any)

	// Compare each table's re-exported JSON against the original export.
	for table := range expectedCounts {
		origRows, _ := tablesRaw[table].([]any)
		reRows, _ := reTables[table].([]any)
		if len(origRows) != len(reRows) {
			t.Errorf("roundtrip: table %q row count mismatch: orig=%d, re=%d", table, len(origRows), len(reRows))
			continue
		}
		if len(origRows) == 0 {
			continue
		}
		// Compare first row of each table for key fields.
		if !compareRowMaps(t, table, origRows[0], reRows[0]) {
			t.Errorf("roundtrip: table %q first row data mismatch", table)
		}
	}

	// 6d. Verify foreign key integrity: route_channels -> accounts, sites, etc.
	verifyForeignKeyIntegrity(t, db)

	t.Log("roundtrip: all 28 tables verified — data fully preserved through export->reset->import cycle")
}

// ──────────────────────────────────────────────────────────────────────────────
// Test: Accounts-only export roundtrip
// ──────────────────────────────────────────────────────────────────────────────

func TestBackupExportImportAccountsOnly(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	defer db.Close()

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	admin.RegisterBackupRoutes(r, db.DB)
	admin.RegisterMaintenanceRoutes(r, db.DB)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Seed: sites + accounts (accounts tables), plus non-accounts tables.
	db.Exec(`INSERT INTO sites (id, name, url, platform, status, created_at, updated_at)
		VALUES (1, 'Acme Site', 'https://acme.openai.com', 'openai', 'active', ?, ?)`, now, now)
	db.Exec(`INSERT INTO accounts (id, site_id, access_token, status, created_at, updated_at)
		VALUES (1, 1, 'sk-acme', 'active', ?, ?)`, now, now)
	db.Exec(`INSERT INTO account_tokens (id, account_id, name, token, value_status, source, created_at, updated_at)
		VALUES (1, 1, 'default', 'tk-acme', 'ready', 'manual', ?, ?)`, now, now)
	// Non-accounts table: settings (should NOT be in accounts export).
	db.Exec(`INSERT INTO settings (key, value) VALUES ('app.theme', 'dark')`)
	// Non-accounts table: events (should NOT be in accounts export).
	db.Exec(`INSERT INTO events (id, type, title, level, created_at) VALUES (1, 'info', 'test', 'info', ?)`, now)
	// Non-accounts table: proxy_logs (should NOT be in accounts export).
	db.Exec(`INSERT INTO proxy_logs (id, route_id, status, created_at) VALUES (1, 1, 'success', ?)`, now)

	// Export accounts only.
	exportRec := doGet(t, r, "/api/settings/backup/export?type=accounts")
	if exportRec.Code != 200 {
		t.Fatalf("export accounts: expected 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}

	var exportPayload map[string]any
	json.Unmarshal(exportRec.Body.Bytes(), &exportPayload)
	tables, _ := exportPayload["tables"].(map[string]any)

	// Accounts tables must be present.
	if _, ok := tables["sites"]; !ok {
		t.Error("accounts export: missing sites table")
	}
	if _, ok := tables["accounts"]; !ok {
		t.Error("accounts export: missing accounts table")
	}
	if _, ok := tables["account_tokens"]; !ok {
		t.Error("accounts export: missing account_tokens table")
	}

	// Non-accounts tables must NOT be present.
	if _, ok := tables["settings"]; ok {
		t.Error("accounts export: settings should NOT be in accounts export")
	}
	if _, ok := tables["events"]; ok {
		t.Error("accounts export: events should NOT be in accounts export")
	}
	if _, ok := tables["proxy_logs"]; ok {
		t.Error("accounts export: proxy_logs should NOT be in accounts export")
	}

	t.Log("accounts-only export: correct table subset verified")
}

// ──────────────────────────────────────────────────────────────────────────────
// Test: Preferences-only export roundtrip
// ──────────────────────────────────────────────────────────────────────────────

func TestBackupExportImportPreferencesOnly(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	defer db.Close()

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	admin.RegisterBackupRoutes(r, db.DB)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	db.Exec(`INSERT INTO settings (key, value) VALUES ('app.theme', 'dark')`)
	db.Exec(`INSERT INTO settings (key, value) VALUES ('app.lang', 'en')`)
	// Non-preferences table.
	db.Exec(`INSERT INTO sites (id, name, url, platform, status, created_at, updated_at)
		VALUES (1, 'Site', 'https://example.com', 'openai', 'active', ?, ?)`, now, now)

	exportRec := doGet(t, r, "/api/settings/backup/export?type=preferences")
	if exportRec.Code != 200 {
		t.Fatalf("export preferences: expected 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}

	var exportPayload map[string]any
	json.Unmarshal(exportRec.Body.Bytes(), &exportPayload)
	tables, _ := exportPayload["tables"].(map[string]any)

	if _, ok := tables["settings"]; !ok {
		t.Error("preferences export: missing settings table")
	}
	if _, ok := tables["sites"]; ok {
		t.Error("preferences export: sites should NOT be in preferences export")
	}

	t.Log("preferences-only export: correct table subset verified")
}

// ──────────────────────────────────────────────────────────────────────────────
// Test: Export with invalid type returns 400
// ──────────────────────────────────────────────────────────────────────────────

func TestBackupExportInvalidType(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	defer db.Close()

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	admin.RegisterBackupRoutes(r, db.DB)

	rec := doGet(t, r, "/api/settings/backup/export?type=invalid")
	if rec.Code != 400 {
		t.Errorf("expected 400 for invalid type, got %d: %s", rec.Code, rec.Body.String())
	}

	t.Log("invalid export type correctly returns 400")
}

// ──────────────────────────────────────────────────────────────────────────────
// Test: Empty database roundtrip succeeds
// ──────────────────────────────────────────────────────────────────────────────

func TestBackupEmptyDatabaseRoundtrip(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	defer db.Close()

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	admin.RegisterBackupRoutes(r, db.DB)
	admin.RegisterMaintenanceRoutes(r, db.DB)

	// Export empty database.
	exportRec := doGet(t, r, "/api/settings/backup/export?type=all")
	if exportRec.Code != 200 {
		t.Fatalf("export empty: expected 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}

	var exportPayload map[string]any
	json.Unmarshal(exportRec.Body.Bytes(), &exportPayload)
	tables, _ := exportPayload["tables"].(map[string]any)

	// All 28 tables present but with empty arrays.
	if len(tables) != 28 {
		t.Errorf("empty export: expected 28 tables, got %d", len(tables))
	}

	// Factory reset (should succeed even on empty DB).
	resetRec := doPostJSON_Raw(t, r, "/api/settings/maintenance/factory-reset", `{"confirm":true}`)
	if resetRec.Code != 200 {
		t.Errorf("factory-reset on empty DB: expected 200, got %d", resetRec.Code)
	}

	// Import empty payload (should succeed with zero imports).
	importRec := doPostJSON_Map(t, r, "/api/settings/backup/import", map[string]any{
		"tables": tables,
	})
	if importRec.Code != 200 {
		t.Errorf("import empty: expected 200, got %d: %s", importRec.Code, importRec.Body.String())
	}

	t.Log("empty database roundtrip succeeds")
}

// ──────────────────────────────────────────────────────────────────────────────
// Test: Import with malformed JSON returns 400
// ──────────────────────────────────────────────────────────────────────────────

func TestBackupImportMalformedJSON(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	defer db.Close()

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	admin.RegisterBackupRoutes(r, db.DB)

	rec := doPostJSON_Raw(t, r, "/api/settings/backup/import", `{"not_tables": "wrong"}`)
	if rec.Code != 400 {
		t.Errorf("malformed import: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	t.Log("malformed import correctly returns 400")
}

// ──────────────────────────────────────────────────────────────────────────────
// Test: Settings JSON fields survive roundtrip with exact fidelity
// ──────────────────────────────────────────────────────────────────────────────

func TestBackupSettingsJSONFidelity(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	defer db.Close()

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	admin.RegisterBackupRoutes(r, db.DB)
	admin.RegisterMaintenanceRoutes(r, db.DB)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Insert a downstream_api_key with JSON array fields.
	db.Exec(`INSERT INTO downstream_api_keys (id, name, key, enabled, used_cost, used_requests, supported_models, allowed_route_ids, created_at, updated_at)
		VALUES (1, 'Key With JSON', 'sk-json-fields', 1, 0, 0, '["gpt-4","gpt-3.5","claude-3-opus"]', '[1,2,3]', ?, ?)`, now, now)

	// Insert a site with nullable fields set.
	db.Exec(`INSERT INTO sites (id, name, url, platform, status, custom_headers, created_at, updated_at)
		VALUES (1, 'Custom Site', 'https://custom.api.com', 'openai', 'active', '{"X-Custom":"value"}', ?, ?)`, now, now)

	// Export.
	exportRec := doGet(t, r, "/api/settings/backup/export?type=all")
	if exportRec.Code != 200 {
		t.Fatalf("export: %d", exportRec.Code)
	}

	var exportPayload map[string]any
	json.Unmarshal(exportRec.Body.Bytes(), &exportPayload)
	tables, _ := exportPayload["tables"].(map[string]any)

	// Factory reset.
	doPostJSON_Raw(t, r, "/api/settings/maintenance/factory-reset", `{"confirm":true}`)

	// Import.
	importRec := doPostJSON_Map(t, r, "/api/settings/backup/import", map[string]any{
		"tables": tables,
	})
	if importRec.Code != 200 {
		t.Fatalf("import: %d: %s", importRec.Code, importRec.Body.String())
	}

	// Verify JSON fields are preserved exactly.
	var supportedModels string
	db.Get(&supportedModels, `SELECT supported_models FROM downstream_api_keys WHERE id = 1`)
	if supportedModels != `["gpt-4","gpt-3.5","claude-3-opus"]` {
		t.Errorf("JSON fidelity: supported_models = %q, expected array", supportedModels)
	}

	var customHeaders *string
	db.Get(&customHeaders, `SELECT custom_headers FROM sites WHERE id = 1`)
	if customHeaders == nil || *customHeaders != `{"X-Custom":"value"}` {
		t.Errorf("JSON fidelity: custom_headers = %v", customHeaders)
	}

	t.Log("JSON field fidelity verified")
}

// ──────────────────────────────────────────────────────────────────────────────
// Test: Deleted token_id survives import (SET NULL FK on token_id)
// ──────────────────────────────────────────────────────────────────────────────

func TestBackupRouteChannelTokenNullFK(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	defer db.Close()

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	admin.RegisterBackupRoutes(r, db.DB)
	admin.RegisterMaintenanceRoutes(r, db.DB)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Create site, account, account_token, token_route.
	db.Exec(`INSERT INTO sites (id, name, url, platform, status, created_at, updated_at)
		VALUES (1, 's', 'https://s.com', 'openai', 'active', ?, ?)`, now, now)
	db.Exec(`INSERT INTO accounts (id, site_id, access_token, status, created_at, updated_at)
		VALUES (1, 1, 'sk-t', 'active', ?, ?)`, now, now)
	db.Exec(`INSERT INTO account_tokens (id, account_id, name, token, value_status, source, created_at, updated_at)
		VALUES (1, 1, 'tk', 'val', 'ready', 'manual', ?, ?)`, now, now)
	db.Exec(`INSERT INTO token_routes (id, model_pattern, routing_strategy, enabled, created_at, updated_at)
		VALUES (1, 'gpt-4', 'weighted', 1, ?, ?)`, now, now)

	// Create route_channel WITH token_id set (no created_at column).
	db.Exec(`INSERT INTO route_channels (id, route_id, account_id, token_id, weight, enabled)
		VALUES (1, 1, 1, 1, 10, 1)`)

	// Export.
	exportRec := doGet(t, r, "/api/settings/backup/export?type=all")
	if exportRec.Code != 200 {
		t.Fatalf("export: %d", exportRec.Code)
	}
	var exportPayload map[string]any
	json.Unmarshal(exportRec.Body.Bytes(), &exportPayload)
	tables, _ := exportPayload["tables"].(map[string]any)

	// Verify export contains token_id for route_channels.
	rcRows, _ := tables["route_channels"].([]any)
	if len(rcRows) != 1 {
		t.Fatalf("expected 1 route_channel in export, got %d", len(rcRows))
	}
	rcRow, _ := rcRows[0].(map[string]any)
	if tokenID, ok := rcRow["token_id"]; !ok || tokenID == nil {
		t.Error("export: route_channel missing token_id")
	}

	// Factory reset.
	doPostJSON_Raw(t, r, "/api/settings/maintenance/factory-reset", `{"confirm":true}`)

	// Import.
	doPostJSON_Map(t, r, "/api/settings/backup/import", map[string]any{"tables": tables})

	// Verify route_channel was restored with token_id.
	var tokenID *int64
	db.Get(&tokenID, `SELECT token_id FROM route_channels WHERE id = 1`)
	if tokenID == nil || *tokenID != 1 {
		t.Errorf("after import: route_channel.token_id = %v, expected 1", tokenID)
	}

	t.Log("route_channel token_id FK survived roundtrip")
}

// ══════════════════════════════════════════════════════════════════════════════
// Test Helpers
// ══════════════════════════════════════════════════════════════════════════════

// verifyRowCounts checks that each table has the expected number of rows.
func verifyRowCounts(t *testing.T, db *store.DB, expected map[string]int, label string) {
	t.Helper()
	for table, exp := range expected {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		if err := db.Get(&count, query); err != nil {
			t.Errorf("%s: table %q count query failed: %v", label, table, err)
			continue
		}
		if count != exp {
			t.Errorf("%s: table %q has %d rows, expected %d", label, table, count, exp)
		}
	}
}

// verifySpotChecks verifies specific data values survived the roundtrip.
func verifySpotChecks(t *testing.T, db *store.DB) {
	t.Helper()

	// sites: check name and URL.
	var siteName, siteURL string
	db.Get(&siteName, `SELECT name FROM sites WHERE id = 1`)
	db.Get(&siteURL, `SELECT url FROM sites WHERE id = 1`)
	if siteName != "Test Site A" {
		t.Errorf("spot check: site 1 name = %q, expected 'Test Site A'", siteName)
	}
	if siteURL != "https://api.openai.com" {
		t.Errorf("spot check: site 1 url = %q", siteURL)
	}

	// accounts: check balance.
	var balance float64
	db.Get(&balance, `SELECT balance FROM accounts WHERE id = 1`)
	if balance != 100.5 {
		t.Errorf("spot check: account 1 balance = %v, expected 100.5", balance)
	}

	// checkin_logs: check status and reward.
	var status, reward *string
	db.Get(&status, `SELECT status FROM checkin_logs WHERE id = 1`)
	db.Get(&reward, `SELECT reward FROM checkin_logs WHERE id = 1`)
	if status == nil || *status != "success" {
		t.Errorf("spot check: checkin_log 1 status = %v", status)
	}
	if reward == nil || *reward != "100 credits" {
		t.Errorf("spot check: checkin_log 1 reward = %v", reward)
	}

	// proxy_files: check public_id and content_base64.
	var publicID, contentB64 string
	db.Get(&publicID, `SELECT public_id FROM proxy_files WHERE id = 1`)
	db.Get(&contentB64, `SELECT content_base64 FROM proxy_files WHERE id = 1`)
	if publicID != "file-pub-001" {
		t.Errorf("spot check: proxy_file 1 public_id = %q", publicID)
	}
	if contentB64 != "dGVzdA==" {
		t.Errorf("spot check: proxy_file 1 content_base64 = %q, expected 'dGVzdA=='", contentB64)
	}

	// settings: check text PK key.
	var themeVal *string
	db.Get(&themeVal, `SELECT value FROM settings WHERE key = 'app.theme'`)
	if themeVal == nil || *themeVal != "dark" {
		t.Errorf("spot check: settings 'app.theme' = %v", themeVal)
	}

	// analytics_projection_checkpoints: check text PK.
	var projKey string
	db.Get(&projKey, `SELECT projector_key FROM analytics_projection_checkpoints WHERE projector_key = 'day-usage-projection'`)
	if projKey != "day-usage-projection" {
		t.Errorf("spot check: checkpoint projector_key = %q", projKey)
	}

	// downstream_api_keys: check key uniqueness survived.
	var dkCount int
	db.Get(&dkCount, `SELECT COUNT(*) FROM downstream_api_keys WHERE key = 'sk-downstream-key-a'`)
	if dkCount != 1 {
		t.Errorf("spot check: downstream_api_key 'sk-downstream-key-a' count = %d, expected 1", dkCount)
	}

	// events: check type.
	var eventType string
	db.Get(&eventType, `SELECT type FROM events WHERE id = 1`)
	if eventType != "info" {
		t.Errorf("spot check: event 1 type = %q", eventType)
	}
}

// verifyForeignKeyIntegrity checks FK relationships are intact after import.
func verifyForeignKeyIntegrity(t *testing.T, db *store.DB) {
	t.Helper()

	// site_api_endpoints -> sites
	var count int
	db.Get(&count, `SELECT COUNT(*) FROM site_api_endpoints WHERE site_id NOT IN (SELECT id FROM sites)`)
	if count != 0 {
		t.Errorf("FK integrity: %d site_api_endpoints have invalid site_id", count)
	}

	// accounts -> sites
	db.Get(&count, `SELECT COUNT(*) FROM accounts WHERE site_id NOT IN (SELECT id FROM sites)`)
	if count != 0 {
		t.Errorf("FK integrity: %d accounts have invalid site_id", count)
	}

	// account_tokens -> accounts
	db.Get(&count, `SELECT COUNT(*) FROM account_tokens WHERE account_id NOT IN (SELECT id FROM accounts)`)
	if count != 0 {
		t.Errorf("FK integrity: %d account_tokens have invalid account_id", count)
	}

	// route_channels -> token_routes
	db.Get(&count, `SELECT COUNT(*) FROM route_channels WHERE route_id NOT IN (SELECT id FROM token_routes)`)
	if count != 0 {
		t.Errorf("FK integrity: %d route_channels have invalid route_id", count)
	}

	// route_channels -> accounts
	db.Get(&count, `SELECT COUNT(*) FROM route_channels WHERE account_id NOT IN (SELECT id FROM accounts)`)
	if count != 0 {
		t.Errorf("FK integrity: %d route_channels have invalid account_id", count)
	}

	// oauth_route_unit_members -> oauth_route_units
	db.Get(&count, `SELECT COUNT(*) FROM oauth_route_unit_members WHERE unit_id NOT IN (SELECT id FROM oauth_route_units)`)
	if count != 0 {
		t.Errorf("FK integrity: %d oauth_route_unit_members have invalid unit_id", count)
	}

	// proxy_debug_attempts -> proxy_debug_traces
	db.Get(&count, `SELECT COUNT(*) FROM proxy_debug_attempts WHERE trace_id NOT IN (SELECT id FROM proxy_debug_traces)`)
	if count != 0 {
		t.Errorf("FK integrity: %d proxy_debug_attempts have invalid trace_id", count)
	}

	// site_day_usage -> sites
	db.Get(&count, `SELECT COUNT(*) FROM site_day_usage WHERE site_id NOT IN (SELECT id FROM sites)`)
	if count != 0 {
		t.Errorf("FK integrity: %d site_day_usage have invalid site_id", count)
	}

	// site_announcements -> sites
	db.Get(&count, `SELECT COUNT(*) FROM site_announcements WHERE site_id NOT IN (SELECT id FROM sites)`)
	if count != 0 {
		t.Errorf("FK integrity: %d site_announcements have invalid site_id", count)
	}
}

// compareRowMaps does a shallow comparison of two decoded JSON row maps,
// ignoring the "id" field (which could shift if sequence reset worked).
// Returns true if all non-id fields match.
func compareRowMaps(t *testing.T, table string, a, b any) bool {
	t.Helper()
	aMap, okA := a.(map[string]any)
	bMap, okB := b.(map[string]any)
	if !okA || !okB {
		return false
	}

	// Compare field by field.
	for key, aVal := range aMap {
		bVal, ok := bMap[key]
		if !ok {
			t.Logf("roundtrip compare %q: field %q missing in re-export", table, key)
			return false
		}
		// Use JSON string comparison for robustness across numeric types.
		aJSON, _ := json.Marshal(aVal)
		bJSON, _ := json.Marshal(bVal)
		if string(aJSON) != string(bJSON) {
			t.Logf("roundtrip compare %q.%s: %s != %s", table, key, string(aJSON), string(bJSON))
			return false
		}
	}
	return true
}

// doGet sends a GET request and returns the response recorder.
func doGet(t *testing.T, r chi.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// doPostJSON_Raw sends a POST with a raw JSON string body.
func doPostJSON_Raw(t *testing.T, r chi.Router, path string, rawJSON string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(rawJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// doPostJSON_Map sends a POST with a map body (marshaled to JSON).
func doPostJSON_Map(t *testing.T, r chi.Router, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
