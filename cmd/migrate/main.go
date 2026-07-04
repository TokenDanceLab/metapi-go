// Command metapi-migrate is a standalone tool that transfers all 18 application
// tables from a SQLite source database to a PostgreSQL target database.
//
// Usage:
//
//	metapi-migrate --from sqlite://data/hub.db --to postgres://user:pass@host:5432/db
//	metapi-migrate --from sqlite://data/hub.db --to postgres://user:pass@host:5432/db --dry-run
//	metapi-migrate --from sqlite://data/hub.db --to postgres://user:pass@host:5432/db --overwrite --progress --verify
//
// The migration matches the TS databaseMigrationService.ts behaviour:
//   - Per-column type coercion with fallback defaults
//   - JSON column serialization (13 columns across 5 tables)
//   - FK-safe DELETE order during overwrite
//   - PostgreSQL sequence synchronization after insert
//   - Single-transaction boundary with rollback on error
//   - Settings key filtering (skips db_type, db_url, db_ssl)
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// ---- CLI flags ----

var (
	flagFrom     = flag.String("from", "", "Source SQLite database (sqlite://path or plain path)")
	flagTo       = flag.String("to", "", "Target PostgreSQL connection string (postgres://user:pass@host:port/db)")
	flagDryRun   = flag.Bool("dry-run", false, "Validate and print migration plan without writing data")
	flagProgress = flag.Bool("progress", false, "Show per-table progress during transfer")
	flagVerify   = flag.Bool("verify", false, "Compute row-count + hash checksum after migration")
	flagOverwrite = flag.Bool("overwrite", true, "Clear target data before inserting (default true, matches TS)")
	flagBatchSize = flag.Int("batch-size", 1, "Rows per multi-row INSERT batch (1 = row-by-row, matching TS default)")
)

// ---- Type coercion helpers (matching TS) ----

func asString(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func asBoolean(v interface{}, fallback bool) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case int64:
		return val != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}

func asNumber(v interface{}, fallback interface{}) interface{} {
	if v == nil {
		return fallback
	}
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return fallback
		}
		return f
	}
	return fallback
}

func asNullableString(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	return fmt.Sprintf("%v", v)
}

// ---- JSON column serialization (matching TS serializeColumnValue) ----

// jsonColumnSet records which columns have logical type 'json'.
// 13 columns across 5 tables, matching the TS schemaContract.json logical types.
var jsonColumnSet = map[string]bool{
	"sites.custom_headers":                        true,
	"accounts.extra_config":                       true,
	"token_routes.model_mapping":                  true,
	"token_routes.decision_snapshot":              true,
	"proxy_logs.billing_details":                  true,
	"proxy_video_tasks.status_snapshot":           true,
	"proxy_video_tasks.upstream_response_meta":    true,
	"downstream_api_keys.supported_models":        true,
	"downstream_api_keys.allowed_route_ids":       true,
	"downstream_api_keys.site_weight_multipliers":  true,
	"downstream_api_keys.excluded_site_ids":       true,
	"downstream_api_keys.excluded_credential_refs": true,
}

func isJSONColumn(table, column string) bool {
	return jsonColumnSet[table+"."+column]
}

func serializeColumnValue(table, column string, v interface{}) interface{} {
	if isJSONColumn(table, column) {
		return serializeJSONValue(v)
	}
	return asNullableString(v)
}

func serializeJSONValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(b)
}

// ---- Runtime DB setting keys to skip (matching TS RUNTIME_DATABASE_SETTING_KEYS) ----

var runtimeDBSettingKeys = map[string]bool{
	"db_type": true,
	"db_url":  true,
	"db_ssl":  true,
}

// ---- FK-safe clear order (children before parents, matching TS clearTargetData) ----

var clearOrder = []string{
	"route_channels",
	"route_group_sources",
	"token_model_availability",
	"model_availability",
	"checkin_logs",
	"proxy_logs",
	"proxy_video_tasks",
	"proxy_files",
	"account_tokens",
	"accounts",
	"site_announcements",
	"site_disabled_models",
	"site_api_endpoints",
	"token_routes",
	"sites",
	"downstream_api_keys",
	"events",
	"settings",
}

// ---- Tables that have auto-increment id for PG sequence sync (matching TS) ----

var sequenceTables = []string{
	"sites",
	"site_api_endpoints",
	"site_announcements",
	"site_disabled_models",
	"accounts",
	"account_tokens",
	"checkin_logs",
	"model_availability",
	"token_model_availability",
	"token_routes",
	"route_channels",
	"route_group_sources",
	"proxy_logs",
	"proxy_video_tasks",
	"proxy_files",
	"downstream_api_keys",
	"events",
	// settings is excluded (no serial id in migration path)
}

// ---- Migration summary ----

type MigrationSummary struct {
	Dialect    string         `json:"dialect"`
	Connection string         `json:"connection"`
	Overwrite  bool           `json:"overwrite"`
	Version    string         `json:"version"`
	Timestamp  int64          `json:"timestamp"`
	Rows       map[string]int `json:"rows"`
}

// ---- SQL helpers ----

func quoteIdentPG(s string) string {
	return `"` + s + `"`
}

func maskPassword(connStr string) string {
	u, err := url.Parse(connStr)
	if err != nil {
		return connStr
	}
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
	}
	return u.String()
}

// ---- Migration flow ----

func runMigration(fromPath, toURL string, overwrite, dryRun, progress, verify bool, batchSize int) (*MigrationSummary, error) {
	// 1. Normalize source path
	sourcePath, err := normalizeSQLitePath(fromPath)
	if err != nil {
		return nil, fmt.Errorf("invalid --from: %w", err)
	}

	// 2. Validate target URL
	if err := validatePGURL(toURL); err != nil {
		return nil, fmt.Errorf("invalid --to: %w", err)
	}

	// 3. Open source SQLite
	srcDB, err := sql.Open("sqlite", sourcePath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open source SQLite: %w", err)
	}
	defer srcDB.Close()

	// 4. Read all 18 tables into memory (matching TS toBackupSnapshot)
	fmt.Fprintf(os.Stderr, "Reading source SQLite database...\n")
	snapshot, err := readAllTables(srcDB)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}

	// Print per-table row counts
	for _, t := range orderedTableNames() {
		fmt.Fprintf(os.Stderr, "  %-28s %d rows\n", t+":", len(snapshot[t]))
	}

	// 5. Build insert statements with full type coercion (matching TS buildStatements)
	inserts := buildStatements(snapshot)

	if dryRun {
		fmt.Fprintf(os.Stderr, "\n[Dry-run] Would insert %d rows across %d tables.\n", len(inserts), len(snapshot))
		fmt.Fprintf(os.Stderr, "[Dry-run] No data written.\n")
		return buildSummary(snapshot, toURL, overwrite), nil
	}

	// 6. Open target PG
	tgtDB, err := sql.Open("pgx", toURL)
	if err != nil {
		return nil, fmt.Errorf("open target PG: %w", err)
	}
	defer tgtDB.Close()

	if err := tgtDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping target PG: %w", err)
	}

	// 7. Ensure schema exists in target
	if err := ensureTargetSchema(tgtDB); err != nil {
		return nil, fmt.Errorf("ensure target schema: %w", err)
	}

	// 8. Check target state (reject if data exists and !overwrite, matching TS ensureTargetState)
	if !overwrite {
		var count int
		if err := tgtDB.QueryRow(`SELECT COUNT(*) FROM "sites"`).Scan(&count); err == nil && count > 0 {
			return nil, fmt.Errorf("target database already contains data. Use --overwrite to replace")
		}
	}

	// 9. Begin transaction
	tx, err := tgtDB.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() // safe no-op after commit

	// 10. Clear target data in FK-safe order (if overwrite)
	if overwrite {
		fmt.Fprintf(os.Stderr, "\nClearing target data (FK-safe order)...\n")
		for _, table := range clearOrder {
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM "%s"`, table)); err != nil {
				return nil, fmt.Errorf("clear %s: %w", table, err)
			}
		}
	}

	// 11. Insert all rows
	fmt.Fprintf(os.Stderr, "\nInserting %d rows...\n", len(inserts))
	inserted := 0
	start := time.Now()

	for _, stmt := range inserts {
		sqlText, args := buildInsertPG(stmt)
		if _, err := tx.Exec(sqlText, args...); err != nil {
			return nil, fmt.Errorf("insert into %s: %w", stmt.table, err)
		}
		inserted++
		if progress && inserted%100 == 0 {
			elapsed := time.Since(start)
			fmt.Fprintf(os.Stderr, "  %d/%d rows inserted (%s elapsed)\n", inserted, len(inserts), elapsed.Round(time.Millisecond))
		}
	}

	if progress {
		elapsed := time.Since(start)
		fmt.Fprintf(os.Stderr, "  Done: %d rows in %s\n", inserted, elapsed.Round(time.Millisecond))
	}

	// 12. Sync PostgreSQL sequences
	fmt.Fprintf(os.Stderr, "\nSyncing PostgreSQL sequences...\n")
	for _, table := range sequenceTables {
		q := fmt.Sprintf(`SELECT setval(pg_get_serial_sequence('%s', 'id'), COALESCE((SELECT MAX("id") FROM "%s"), 1), TRUE)`, table, table)
		if _, err := tx.Exec(q); err != nil {
			// Table might not exist if migrations haven't run; warn but continue
			fmt.Fprintf(os.Stderr, "  Warning: sync sequence for %s: %v\n", table, err)
		}
	}

	// 13. Commit
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	summary := buildSummary(snapshot, toURL, overwrite)

	// 14. Verify checksums (if requested)
	if verify {
		fmt.Fprintf(os.Stderr, "\nVerifying checksums...\n")
		if err := verifyChecksums(srcDB, tgtDB, snapshot); err != nil {
			fmt.Fprintf(os.Stderr, "  Verification warning: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  All checksums match.\n")
		}
	}

	return summary, nil
}

// normalizeSQLitePath handles sqlite:// prefix and plain paths (matching TS normalizeSqliteTarget).
func normalizeSQLitePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	if raw == ":memory:" {
		return raw, nil
	}

	lower := strings.ToLower(raw)

	// file:// prefix
	if strings.HasPrefix(lower, "file://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("invalid file:// URL: %w", err)
		}
		return u.Path, nil
	}

	// sqlite:// prefix
	if strings.HasPrefix(lower, "sqlite://") {
		return strings.TrimSpace(raw[len("sqlite://"):]), nil
	}

	// Guard against network URLs
	if strings.Contains(raw, "://") {
		return "", fmt.Errorf("SQLite connection cannot be a network URL; use plain file path or sqlite:// prefix")
	}

	return raw, nil
}

func validatePGURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("connection string cannot be empty")
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "postgres:") && !strings.HasPrefix(lower, "postgresql:") {
		return fmt.Errorf("must start with postgres:// or postgresql://")
	}
	return nil
}

// readAllTables reads all 18 tables from the SQLite source into memory.
func readAllTables(db *sql.DB) (map[string][]map[string]interface{}, error) {
	tables := []string{
		"sites",
		"site_api_endpoints",
		"site_announcements",
		"site_disabled_models",
		"accounts",
		"account_tokens",
		"checkin_logs",
		"model_availability",
		"token_model_availability",
		"token_routes",
		"route_channels",
		"route_group_sources",
		"proxy_logs",
		"proxy_video_tasks",
		"proxy_files",
		"downstream_api_keys",
		"events",
		"settings",
	}

	snapshot := make(map[string][]map[string]interface{})

	for _, table := range tables {
		rows, err := db.Query(fmt.Sprintf(`SELECT * FROM "%s"`, table))
		if err != nil {
			// Table might not exist if migrations haven't created it
			snapshot[table] = nil
			continue
		}

		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("columns for %s: %w", table, err)
		}

		var tableRows []map[string]interface{}
		for rows.Next() {
			values := make([]interface{}, len(cols))
			valuePtrs := make([]interface{}, len(cols))
			for i := range values {
				valuePtrs[i] = &values[i]
			}
			if err := rows.Scan(valuePtrs...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan %s: %w", table, err)
			}
			row := make(map[string]interface{})
			for i, col := range cols {
				row[col] = values[i]
			}
			tableRows = append(tableRows, row)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iter %s: %w", table, err)
		}

		snapshot[table] = tableRows
	}

	return snapshot, nil
}

// orderedTableNames returns table names in a stable order for display.
func orderedTableNames() []string {
	return []string{
		"sites",
		"site_api_endpoints",
		"site_announcements",
		"site_disabled_models",
		"accounts",
		"account_tokens",
		"checkin_logs",
		"model_availability",
		"token_model_availability",
		"token_routes",
		"route_channels",
		"route_group_sources",
		"proxy_logs",
		"proxy_video_tasks",
		"proxy_files",
		"downstream_api_keys",
		"events",
		"settings",
	}
}

// ---- Insert statement builder (matching TS buildStatements) ----

type insertStmt struct {
	table   string
	columns []string
	values  []interface{}
}

func buildStatements(snapshot map[string][]map[string]interface{}) []insertStmt {
	var stmts []insertStmt

	stmts = append(stmts, buildSites(snapshot["sites"])...)
	stmts = append(stmts, buildSiteAPIEndpoints(snapshot["site_api_endpoints"])...)
	stmts = append(stmts, buildSiteDisabledModels(snapshot["site_disabled_models"])...)
	stmts = append(stmts, buildSiteAnnouncements(snapshot["site_announcements"])...)
	stmts = append(stmts, buildAccounts(snapshot["accounts"])...)
	stmts = append(stmts, buildAccountTokens(snapshot["account_tokens"])...)
	stmts = append(stmts, buildCheckinLogs(snapshot["checkin_logs"])...)
	stmts = append(stmts, buildModelAvailability(snapshot["model_availability"])...)
	stmts = append(stmts, buildTokenModelAvailability(snapshot["token_model_availability"])...)
	stmts = append(stmts, buildTokenRoutes(snapshot["token_routes"])...)
	stmts = append(stmts, buildRouteChannels(snapshot["route_channels"])...)
	stmts = append(stmts, buildRouteGroupSources(snapshot["route_group_sources"])...)
	stmts = append(stmts, buildProxyLogs(snapshot["proxy_logs"])...)
	stmts = append(stmts, buildProxyVideoTasks(snapshot["proxy_video_tasks"])...)
	stmts = append(stmts, buildProxyFiles(snapshot["proxy_files"])...)
	stmts = append(stmts, buildDownstreamAPIKeys(snapshot["downstream_api_keys"])...)
	stmts = append(stmts, buildEvents(snapshot["events"])...)
	stmts = append(stmts, buildSettings(snapshot["settings"])...)

	return stmts
}

func v(row map[string]interface{}, key string) interface{} {
	return row[key]
}

func buildSites(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "name", "url", "external_checkin_url", "platform", "proxy_url", "use_system_proxy", "custom_headers", "status", "is_pinned", "sort_order", "global_weight", "api_key", "created_at", "updated_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "sites", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNullableString(v(row, "name")),
				asNullableString(v(row, "url")),
				asNullableString(v(row, "external_checkin_url")),
				asNullableString(v(row, "platform")),
				asNullableString(v(row, "proxy_url")),
				asBoolean(v(row, "use_system_proxy"), false),
				serializeColumnValue("sites", "custom_headers", v(row, "custom_headers")),
				coalesceNullString(asNullableString(v(row, "status")), "active"),
				asBoolean(v(row, "is_pinned"), false),
				asNumber(v(row, "sort_order"), float64(0)),
				asNumber(v(row, "global_weight"), float64(1)),
				asNullableString(v(row, "api_key")),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
			},
		})
	}
	return stmts
}

func coalesceNullString(v interface{}, fallback string) interface{} {
	if v == nil {
		return fallback
	}
	return v
}

func buildSiteAPIEndpoints(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "site_id", "url", "enabled", "sort_order", "cooldown_until", "last_selected_at", "last_failed_at", "last_failure_reason", "created_at", "updated_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "site_api_endpoints", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "site_id"), float64(0)),
				asNullableString(v(row, "url")),
				asBoolean(v(row, "enabled"), true),
				asNumber(v(row, "sort_order"), float64(0)),
				asNullableString(v(row, "cooldown_until")),
				asNullableString(v(row, "last_selected_at")),
				asNullableString(v(row, "last_failed_at")),
				asNullableString(v(row, "last_failure_reason")),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
			},
		})
	}
	return stmts
}

func buildSiteDisabledModels(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "site_id", "model_name", "created_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "site_disabled_models", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "site_id"), float64(0)),
				asNullableString(v(row, "model_name")),
				asNullableString(v(row, "created_at")),
			},
		})
	}
	return stmts
}

func buildSiteAnnouncements(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "site_id", "platform", "source_key", "title", "content", "level", "source_url", "starts_at", "ends_at", "upstream_created_at", "upstream_updated_at", "first_seen_at", "last_seen_at", "read_at", "dismissed_at", "raw_payload", "created_at", "updated_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "site_announcements", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "site_id"), float64(0)),
				asNullableString(v(row, "platform")),
				asNullableString(v(row, "source_key")),
				asNullableString(v(row, "title")),
				asNullableString(v(row, "content")),
				coalesceNullString(asNullableString(v(row, "level")), "info"),
				asNullableString(v(row, "source_url")),
				asNullableString(v(row, "starts_at")),
				asNullableString(v(row, "ends_at")),
				asNullableString(v(row, "upstream_created_at")),
				asNullableString(v(row, "upstream_updated_at")),
				asNullableString(v(row, "first_seen_at")),
				asNullableString(v(row, "last_seen_at")),
				asNullableString(v(row, "read_at")),
				asNullableString(v(row, "dismissed_at")),
				asNullableString(v(row, "raw_payload")),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
			},
		})
	}
	return stmts
}

func buildAccounts(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "site_id", "username", "access_token", "api_token", "balance", "balance_used", "quota", "unit_cost", "value_score", "status", "is_pinned", "sort_order", "checkin_enabled", "last_checkin_at", "last_balance_refresh", "extra_config", "created_at", "updated_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "accounts", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "site_id"), float64(0)),
				asNullableString(v(row, "username")),
				asNullableString(v(row, "access_token")),
				asNullableString(v(row, "api_token")),
				asNumber(v(row, "balance"), float64(0)),
				asNumber(v(row, "balance_used"), float64(0)),
				asNumber(v(row, "quota"), float64(0)),
				asNumber(v(row, "unit_cost"), nil),
				asNumber(v(row, "value_score"), float64(0)),
				coalesceNullString(asNullableString(v(row, "status")), "active"),
				asBoolean(v(row, "is_pinned"), false),
				asNumber(v(row, "sort_order"), float64(0)),
				asBoolean(v(row, "checkin_enabled"), true),
				asNullableString(v(row, "last_checkin_at")),
				asNullableString(v(row, "last_balance_refresh")),
				serializeColumnValue("accounts", "extra_config", v(row, "extra_config")),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
			},
		})
	}
	return stmts
}

func buildAccountTokens(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "account_id", "name", "token", "token_group", "value_status", "source", "enabled", "is_default", "created_at", "updated_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "account_tokens", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "account_id"), float64(0)),
				asNullableString(v(row, "name")),
				asNullableString(v(row, "token")),
				asNullableString(v(row, "token_group")),
				coalesceNullString(asNullableString(v(row, "value_status")), "ready"),
				coalesceNullString(asNullableString(v(row, "source")), "manual"),
				asBoolean(v(row, "enabled"), true),
				asBoolean(v(row, "is_default"), false),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
			},
		})
	}
	return stmts
}

func buildCheckinLogs(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "account_id", "status", "message", "reward", "created_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "checkin_logs", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "account_id"), float64(0)),
				coalesceNullString(asNullableString(v(row, "status")), "success"),
				asNullableString(v(row, "message")),
				asNullableString(v(row, "reward")),
				asNullableString(v(row, "created_at")),
			},
		})
	}
	return stmts
}

func buildModelAvailability(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "account_id", "model_name", "available", "is_manual", "latency_ms", "checked_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "model_availability", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "account_id"), float64(0)),
				asNullableString(v(row, "model_name")),
				asBoolean(v(row, "available"), false),
				asBoolean(v(row, "is_manual"), false),
				asNumber(v(row, "latency_ms"), nil),
				asNullableString(v(row, "checked_at")),
			},
		})
	}
	return stmts
}

func buildTokenModelAvailability(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "token_id", "model_name", "available", "latency_ms", "checked_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "token_model_availability", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "token_id"), float64(0)),
				asNullableString(v(row, "model_name")),
				asBoolean(v(row, "available"), false),
				asNumber(v(row, "latency_ms"), nil),
				asNullableString(v(row, "checked_at")),
			},
		})
	}
	return stmts
}

func buildTokenRoutes(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "model_pattern", "display_name", "display_icon", "model_mapping", "route_mode", "decision_snapshot", "decision_refreshed_at", "routing_strategy", "enabled", "created_at", "updated_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "token_routes", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNullableString(v(row, "model_pattern")),
				asNullableString(v(row, "display_name")),
				asNullableString(v(row, "display_icon")),
				serializeColumnValue("token_routes", "model_mapping", v(row, "model_mapping")),
				coalesceNullString(asNullableString(v(row, "route_mode")), "pattern"),
				serializeColumnValue("token_routes", "decision_snapshot", v(row, "decision_snapshot")),
				asNullableString(v(row, "decision_refreshed_at")),
				asNullableString(v(row, "routing_strategy")),
				asBoolean(v(row, "enabled"), true),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
			},
		})
	}
	return stmts
}

func buildRouteChannels(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "route_id", "account_id", "token_id", "oauth_route_unit_id", "source_model", "priority", "weight", "enabled", "manual_override", "success_count", "fail_count", "total_latency_ms", "total_cost", "last_used_at", "last_selected_at", "last_fail_at", "consecutive_fail_count", "cooldown_level", "cooldown_until"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "route_channels", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "route_id"), float64(0)),
				asNumber(v(row, "account_id"), float64(0)),
				asNumber(v(row, "token_id"), nil),
				asNumber(v(row, "oauth_route_unit_id"), nil),
				asNullableString(v(row, "source_model")),
				asNumber(v(row, "priority"), float64(0)),
				asNumber(v(row, "weight"), float64(10)),
				asBoolean(v(row, "enabled"), true),
				asBoolean(v(row, "manual_override"), false),
				asNumber(v(row, "success_count"), float64(0)),
				asNumber(v(row, "fail_count"), float64(0)),
				asNumber(v(row, "total_latency_ms"), float64(0)),
				asNumber(v(row, "total_cost"), float64(0)),
				asNullableString(v(row, "last_used_at")),
				asNullableString(v(row, "last_selected_at")),
				asNullableString(v(row, "last_fail_at")),
				asNumber(v(row, "consecutive_fail_count"), float64(0)),
				asNumber(v(row, "cooldown_level"), float64(0)),
				asNullableString(v(row, "cooldown_until")),
			},
		})
	}
	return stmts
}

func buildRouteGroupSources(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "group_route_id", "source_route_id"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "route_group_sources", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "group_route_id"), float64(0)),
				asNumber(v(row, "source_route_id"), float64(0)),
			},
		})
	}
	return stmts
}

func buildProxyLogs(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "route_id", "channel_id", "account_id", "downstream_api_key_id", "model_requested", "model_actual", "status", "http_status", "is_stream", "first_byte_latency_ms", "latency_ms", "prompt_tokens", "completion_tokens", "total_tokens", "estimated_cost", "billing_details", "client_family", "client_app_id", "client_app_name", "client_confidence", "error_message", "retry_count", "created_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "proxy_logs", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNumber(v(row, "route_id"), nil),
				asNumber(v(row, "channel_id"), nil),
				asNumber(v(row, "account_id"), nil),
				asNumber(v(row, "downstream_api_key_id"), nil),
				asNullableString(v(row, "model_requested")),
				asNullableString(v(row, "model_actual")),
				asNullableString(v(row, "status")),
				asNumber(v(row, "http_status"), nil),
				asBoolean(v(row, "is_stream"), false),
				asNumber(v(row, "first_byte_latency_ms"), nil),
				asNumber(v(row, "latency_ms"), nil),
				asNumber(v(row, "prompt_tokens"), nil),
				asNumber(v(row, "completion_tokens"), nil),
				asNumber(v(row, "total_tokens"), nil),
				asNumber(v(row, "estimated_cost"), nil),
				serializeColumnValue("proxy_logs", "billing_details", v(row, "billing_details")),
				asNullableString(v(row, "client_family")),
				asNullableString(v(row, "client_app_id")),
				asNullableString(v(row, "client_app_name")),
				asNullableString(v(row, "client_confidence")),
				asNullableString(v(row, "error_message")),
				asNumber(v(row, "retry_count"), float64(0)),
				asNullableString(v(row, "created_at")),
			},
		})
	}
	return stmts
}

func buildProxyVideoTasks(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "public_id", "upstream_video_id", "site_url", "token_value", "requested_model", "actual_model", "channel_id", "account_id", "status_snapshot", "upstream_response_meta", "last_upstream_status", "last_polled_at", "created_at", "updated_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "proxy_video_tasks", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNullableString(v(row, "public_id")),
				asNullableString(v(row, "upstream_video_id")),
				asNullableString(v(row, "site_url")),
				asNullableString(v(row, "token_value")),
				asNullableString(v(row, "requested_model")),
				asNullableString(v(row, "actual_model")),
				asNumber(v(row, "channel_id"), nil),
				asNumber(v(row, "account_id"), nil),
				serializeColumnValue("proxy_video_tasks", "status_snapshot", v(row, "status_snapshot")),
				serializeColumnValue("proxy_video_tasks", "upstream_response_meta", v(row, "upstream_response_meta")),
				asNumber(v(row, "last_upstream_status"), nil),
				asNullableString(v(row, "last_polled_at")),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
			},
		})
	}
	return stmts
}

func buildProxyFiles(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "public_id", "owner_type", "owner_id", "filename", "mime_type", "purpose", "byte_size", "sha256", "content_base64", "created_at", "updated_at", "deleted_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "proxy_files", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNullableString(v(row, "public_id")),
				asNullableString(v(row, "owner_type")),
				asNullableString(v(row, "owner_id")),
				asNullableString(v(row, "filename")),
				asNullableString(v(row, "mime_type")),
				asNullableString(v(row, "purpose")),
				asNumber(v(row, "byte_size"), float64(0)),
				asNullableString(v(row, "sha256")),
				asNullableString(v(row, "content_base64")),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
				asNullableString(v(row, "deleted_at")),
			},
		})
	}
	return stmts
}

func buildDownstreamAPIKeys(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "name", "key", "description", "group_name", "tags", "enabled", "expires_at", "max_cost", "used_cost", "max_requests", "used_requests", "supported_models", "allowed_route_ids", "site_weight_multipliers", "excluded_site_ids", "excluded_credential_refs", "last_used_at", "created_at", "updated_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "downstream_api_keys", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNullableString(v(row, "name")),
				asNullableString(v(row, "key")),
				asNullableString(v(row, "description")),
				asNullableString(v(row, "group_name")),
				asNullableString(v(row, "tags")),
				asBoolean(v(row, "enabled"), true),
				asNullableString(v(row, "expires_at")),
				asNumber(v(row, "max_cost"), nil),
				asNumber(v(row, "used_cost"), float64(0)),
				asNumber(v(row, "max_requests"), nil),
				asNumber(v(row, "used_requests"), float64(0)),
				serializeColumnValue("downstream_api_keys", "supported_models", v(row, "supported_models")),
				serializeColumnValue("downstream_api_keys", "allowed_route_ids", v(row, "allowed_route_ids")),
				serializeColumnValue("downstream_api_keys", "site_weight_multipliers", v(row, "site_weight_multipliers")),
				serializeColumnValue("downstream_api_keys", "excluded_site_ids", v(row, "excluded_site_ids")),
				serializeColumnValue("downstream_api_keys", "excluded_credential_refs", v(row, "excluded_credential_refs")),
				asNullableString(v(row, "last_used_at")),
				asNullableString(v(row, "created_at")),
				asNullableString(v(row, "updated_at")),
			},
		})
	}
	return stmts
}

func buildEvents(rows []map[string]interface{}) []insertStmt {
	cols := []string{"id", "type", "title", "message", "level", "read", "related_id", "related_type", "created_at"}
	var stmts []insertStmt
	for _, row := range rows {
		stmts = append(stmts, insertStmt{
			table: "events", columns: cols,
			values: []interface{}{
				asNumber(v(row, "id"), float64(0)),
				asNullableString(v(row, "type")),
				asNullableString(v(row, "title")),
				asNullableString(v(row, "message")),
				coalesceNullString(asNullableString(v(row, "level")), "info"),
				asBoolean(v(row, "read"), false),
				asNumber(v(row, "related_id"), nil),
				asNullableString(v(row, "related_type")),
				asNullableString(v(row, "created_at")),
			},
		})
	}
	return stmts
}

func buildSettings(rows []map[string]interface{}) []insertStmt {
	cols := []string{"key", "value"}
	var stmts []insertStmt
	for _, row := range rows {
		key := asString(v(row, "key"))
		if runtimeDBSettingKeys[key] {
			continue
		}
		stmts = append(stmts, insertStmt{
			table: "settings", columns: cols,
			values: []interface{}{
				key,
				asNullableString(v(row, "value")),
			},
		})
	}
	return stmts
}

// ---- PG INSERT builder (matching TS buildInsertSql for postgres) ----

func buildInsertPG(s insertStmt) (string, []interface{}) {
	table := quoteIdentPG(s.table)
	quotedCols := make([]string, len(s.columns))
	for i, c := range s.columns {
		quotedCols[i] = quoteIdentPG(c)
	}
	placeholders := make([]string, len(s.columns))
	for i := range s.columns {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)
	return sql, s.values
}

// ---- Ensure target schema (matching TS ensureRuntimeDatabaseSchema) ----

func ensureTargetSchema(db *sql.DB) error {
	// Minimal DDL: CREATE TABLE IF NOT EXISTS for the 18 migration-target tables.
	// The full schema is handled by store.Migrate() in the server binary.
	// Here we only need the tables to exist for data insertion.
	tables := map[string]string{
		"sites":                 `CREATE TABLE IF NOT EXISTS "sites" (id BIGSERIAL PRIMARY KEY, name TEXT, url TEXT, external_checkin_url TEXT, platform TEXT, proxy_url TEXT, use_system_proxy BOOLEAN DEFAULT FALSE, custom_headers JSONB, status TEXT DEFAULT 'active', is_pinned BOOLEAN DEFAULT FALSE, sort_order BIGINT DEFAULT 0, global_weight DOUBLE PRECISION DEFAULT 1, api_key TEXT, post_refresh_probe_enabled BOOLEAN DEFAULT FALSE, post_refresh_probe_model TEXT DEFAULT '', post_refresh_probe_scope TEXT DEFAULT '', post_refresh_probe_latency_threshold_ms BIGINT DEFAULT 0, created_at TEXT, updated_at TEXT)`,
		"site_api_endpoints":    `CREATE TABLE IF NOT EXISTS "site_api_endpoints" (id BIGSERIAL PRIMARY KEY, site_id BIGINT, url TEXT, enabled BOOLEAN DEFAULT TRUE, sort_order BIGINT DEFAULT 0, cooldown_until TEXT, last_selected_at TEXT, last_failed_at TEXT, last_failure_reason TEXT, created_at TEXT, updated_at TEXT)`,
		"site_announcements":    `CREATE TABLE IF NOT EXISTS "site_announcements" (id BIGSERIAL PRIMARY KEY, site_id BIGINT, platform TEXT, source_key TEXT, title TEXT, content TEXT, level TEXT DEFAULT 'info', source_url TEXT, starts_at TEXT, ends_at TEXT, upstream_created_at TEXT, upstream_updated_at TEXT, first_seen_at TEXT, last_seen_at TEXT, read_at TEXT, dismissed_at TEXT, raw_payload TEXT, created_at TEXT, updated_at TEXT)`,
		"site_disabled_models":  `CREATE TABLE IF NOT EXISTS "site_disabled_models" (id BIGSERIAL PRIMARY KEY, site_id BIGINT, model_name TEXT, created_at TEXT)`,
		"accounts":              `CREATE TABLE IF NOT EXISTS "accounts" (id BIGSERIAL PRIMARY KEY, site_id BIGINT, username TEXT, access_token TEXT, api_token TEXT, balance DOUBLE PRECISION DEFAULT 0, balance_used DOUBLE PRECISION DEFAULT 0, quota DOUBLE PRECISION DEFAULT 0, unit_cost DOUBLE PRECISION, value_score DOUBLE PRECISION DEFAULT 0, status TEXT DEFAULT 'active', is_pinned BOOLEAN DEFAULT FALSE, sort_order BIGINT DEFAULT 0, checkin_enabled BOOLEAN DEFAULT TRUE, last_checkin_at TEXT, last_balance_refresh TEXT, oauth_provider TEXT, oauth_account_key TEXT, oauth_project_id TEXT, extra_config JSONB, created_at TEXT, updated_at TEXT)`,
		"account_tokens":        `CREATE TABLE IF NOT EXISTS "account_tokens" (id BIGSERIAL PRIMARY KEY, account_id BIGINT, name TEXT, token TEXT, token_group TEXT, value_status TEXT DEFAULT 'ready', source TEXT DEFAULT 'manual', enabled BOOLEAN DEFAULT TRUE, is_default BOOLEAN DEFAULT FALSE, created_at TEXT, updated_at TEXT)`,
		"checkin_logs":          `CREATE TABLE IF NOT EXISTS "checkin_logs" (id BIGSERIAL PRIMARY KEY, account_id BIGINT, status TEXT DEFAULT 'success', message TEXT, reward TEXT, created_at TEXT)`,
		"model_availability":    `CREATE TABLE IF NOT EXISTS "model_availability" (id BIGSERIAL PRIMARY KEY, account_id BIGINT, model_name TEXT, available BOOLEAN DEFAULT FALSE, is_manual BOOLEAN DEFAULT FALSE, latency_ms BIGINT, checked_at TEXT)`,
		"token_model_availability": `CREATE TABLE IF NOT EXISTS "token_model_availability" (id BIGSERIAL PRIMARY KEY, token_id BIGINT, model_name TEXT, available BOOLEAN DEFAULT FALSE, latency_ms BIGINT, checked_at TEXT)`,
		"token_routes":          `CREATE TABLE IF NOT EXISTS "token_routes" (id BIGSERIAL PRIMARY KEY, model_pattern TEXT, display_name TEXT, display_icon TEXT, route_mode TEXT DEFAULT 'pattern', model_mapping JSONB, decision_snapshot JSONB, decision_refreshed_at TEXT, routing_strategy TEXT DEFAULT '', enabled BOOLEAN DEFAULT TRUE, created_at TEXT, updated_at TEXT)`,
		"route_channels":        `CREATE TABLE IF NOT EXISTS "route_channels" (id BIGSERIAL PRIMARY KEY, route_id BIGINT, account_id BIGINT, token_id BIGINT, oauth_route_unit_id BIGINT, source_model TEXT, priority BIGINT DEFAULT 0, weight BIGINT DEFAULT 10, enabled BOOLEAN DEFAULT TRUE, manual_override BOOLEAN DEFAULT FALSE, success_count BIGINT DEFAULT 0, fail_count BIGINT DEFAULT 0, total_latency_ms BIGINT DEFAULT 0, total_cost DOUBLE PRECISION DEFAULT 0, last_used_at TEXT, last_selected_at TEXT, last_fail_at TEXT, consecutive_fail_count BIGINT DEFAULT 0, cooldown_level BIGINT DEFAULT 0, cooldown_until TEXT)`,
		"route_group_sources":   `CREATE TABLE IF NOT EXISTS "route_group_sources" (id BIGSERIAL PRIMARY KEY, group_route_id BIGINT, source_route_id BIGINT)`,
		"proxy_logs":            `CREATE TABLE IF NOT EXISTS "proxy_logs" (id BIGSERIAL PRIMARY KEY, route_id BIGINT, channel_id BIGINT, account_id BIGINT, downstream_api_key_id BIGINT, model_requested TEXT, model_actual TEXT, status TEXT, http_status BIGINT, is_stream BOOLEAN DEFAULT FALSE, first_byte_latency_ms BIGINT, latency_ms BIGINT, prompt_tokens BIGINT, completion_tokens BIGINT, total_tokens BIGINT, estimated_cost DOUBLE PRECISION, billing_details JSONB, client_family TEXT, client_app_id TEXT, client_app_name TEXT, client_confidence TEXT, error_message TEXT, retry_count BIGINT DEFAULT 0, created_at TEXT)`,
		"proxy_video_tasks":     `CREATE TABLE IF NOT EXISTS "proxy_video_tasks" (id BIGSERIAL PRIMARY KEY, public_id TEXT, upstream_video_id TEXT, site_url TEXT, token_value TEXT, requested_model TEXT, actual_model TEXT, channel_id BIGINT, account_id BIGINT, status_snapshot JSONB, upstream_response_meta JSONB, last_upstream_status BIGINT, last_polled_at TEXT, created_at TEXT, updated_at TEXT)`,
		"proxy_files":           `CREATE TABLE IF NOT EXISTS "proxy_files" (id BIGSERIAL PRIMARY KEY, public_id TEXT, owner_type TEXT, owner_id TEXT, filename TEXT, mime_type TEXT, purpose TEXT, byte_size BIGINT, sha256 TEXT, content_base64 TEXT, created_at TEXT, updated_at TEXT, deleted_at TEXT)`,
		"downstream_api_keys":   `CREATE TABLE IF NOT EXISTS "downstream_api_keys" (id BIGSERIAL PRIMARY KEY, name TEXT, key TEXT, description TEXT, group_name TEXT, tags TEXT, enabled BOOLEAN DEFAULT TRUE, expires_at TEXT, max_cost DOUBLE PRECISION, used_cost DOUBLE PRECISION DEFAULT 0, max_requests BIGINT, used_requests BIGINT DEFAULT 0, supported_models JSONB, allowed_route_ids JSONB, site_weight_multipliers JSONB, excluded_site_ids JSONB, excluded_credential_refs JSONB, last_used_at TEXT, created_at TEXT, updated_at TEXT)`,
		"events":                `CREATE TABLE IF NOT EXISTS "events" (id BIGSERIAL PRIMARY KEY, type TEXT, title TEXT, message TEXT, level TEXT DEFAULT 'info', read BOOLEAN DEFAULT FALSE, related_id BIGINT, related_type TEXT, created_at TEXT)`,
		"settings":              `CREATE TABLE IF NOT EXISTS "settings" (key TEXT PRIMARY KEY, value TEXT)`,
	}

	for table, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create table %s: %w", table, err)
		}
		_ = table // used in error message
	}
	return nil
}

// ---- Checksum verification ----

func verifyChecksums(srcDB, tgtDB *sql.DB, snapshot map[string][]map[string]interface{}) error {
	tables := orderedTableNames()
	for _, table := range tables {
		srcCount := len(snapshot[table])

		var tgtCount int
		if err := tgtDB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, table)).Scan(&tgtCount); err != nil {
			return fmt.Errorf("count %s: %w", table, err)
		}

		if srcCount != tgtCount {
			return fmt.Errorf("%s: row count mismatch (source=%d, target=%d)", table, srcCount, tgtCount)
		}

		// Compute hash of source and target for this table
		srcHash := hashRows(snapshot[table])
		tgtHash, err := hashPGTable(tgtDB, table)
		if err != nil {
			return fmt.Errorf("hash %s: %w", table, err)
		}

		if !bytes.Equal(srcHash, tgtHash) {
			return fmt.Errorf("%s: checksum mismatch (source=%x, target=%x)", table, srcHash, tgtHash)
		}
	}
	return nil
}

func hashRows(rows []map[string]interface{}) []byte {
	h := sha256.New()
	// Sort keys for deterministic serialization
	for _, row := range rows {
		keys := make([]string, 0, len(row))
		for k := range row {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h.Write([]byte(k))
			h.Write([]byte(fmt.Sprintf("%v", row[k])))
		}
	}
	return h.Sum(nil)
}

func hashPGTable(db *sql.DB, table string) ([]byte, error) {
	rows, err := db.Query(fmt.Sprintf(`SELECT * FROM "%s" ORDER BY id`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	h := sha256.New()
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		for i, col := range cols {
			h.Write([]byte(col))
			h.Write([]byte(fmt.Sprintf("%v", values[i])))
		}
	}
	return h.Sum(nil), rows.Err()
}

// ---- Summary ----

func buildSummary(snapshot map[string][]map[string]interface{}, toURL string, overwrite bool) *MigrationSummary {
	s := &MigrationSummary{
		Dialect:    "postgres",
		Connection: maskPassword(toURL),
		Overwrite:  overwrite,
		Version:    "live-db-snapshot",
		Timestamp:  time.Now().UnixMilli(),
		Rows:       make(map[string]int),
	}
	for _, table := range orderedTableNames() {
		s.Rows[table] = len(snapshot[table])
	}
	return s
}

func printSummary(s *MigrationSummary) {
	fmt.Fprintf(os.Stderr, "\nMigration Summary:\n")
	fmt.Fprintf(os.Stderr, "  dialect:    %s\n", s.Dialect)
	fmt.Fprintf(os.Stderr, "  connection: %s\n", s.Connection)
	fmt.Fprintf(os.Stderr, "  overwrite:  %v\n", s.Overwrite)
	fmt.Fprintf(os.Stderr, "  version:    %s\n", s.Version)
	fmt.Fprintf(os.Stderr, "  timestamp:  %d\n", s.Timestamp)
	fmt.Fprintf(os.Stderr, "  rows:\n")
	for _, table := range orderedTableNames() {
		fmt.Fprintf(os.Stderr, "    %-28s %d\n", table+":", s.Rows[table])
	}
}

// ---- Main ----

func main() {
	flag.Parse()

	if *flagFrom == "" || *flagTo == "" {
		fmt.Fprintf(os.Stderr, "Usage: metapi-migrate --from <sqlite_path> --to <postgres_url> [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// --overwrite defaults to true, but --dry-run implies no writes
	overwrite := *flagOverwrite

	_ = context.Background() // reserved for future context propagation

	summary, err := runMigration(*flagFrom, *flagTo, overwrite, *flagDryRun, *flagProgress, *flagVerify, *flagBatchSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	printSummary(summary)
}
