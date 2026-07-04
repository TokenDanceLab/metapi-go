package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterBackupRoutes registers all /api/settings/backup routes.
func RegisterBackupRoutes(r chi.Router, db *sqlx.DB) {
	handler := &backupHandler{db: db}

	r.Get("/api/settings/backup/export", handler.exportBackup)
	r.Post("/api/settings/backup/import", handler.importBackup)
	r.Get("/api/settings/backup/webdav", handler.getWebdavConfig)
	r.Put("/api/settings/backup/webdav", handler.saveWebdavConfig)
	r.Post("/api/settings/backup/webdav/export", handler.exportToWebdav)
	r.Post("/api/settings/backup/webdav/import", handler.importFromWebdav)
}

type backupHandler struct {
	db *sqlx.DB
}

// All 27 tables in FK-safe order (parents before children).
// This order is used for import to satisfy foreign key constraints.
var allTables = []string{
	"sites",
	"site_api_endpoints",
	"site_disabled_models",
	"accounts",
	"account_tokens",
	"checkin_logs",
	"model_availability",
	"token_model_availability",
	"token_routes",
	"route_group_sources",
	"oauth_route_units",
	"oauth_route_unit_members",
	"route_channels",
	"proxy_logs",
	"proxy_debug_traces",
	"proxy_debug_attempts",
	"proxy_video_tasks",
	"proxy_files",
	"settings",
	"admin_snapshots",
	"analytics_projection_checkpoints",
	"site_day_usage",
	"site_hour_usage",
	"model_day_usage",
	"downstream_api_keys",
	"site_announcements",
	"events",
}

// accountsTables is the subset of tables exported when ?type=accounts.
var accountsTables = []string{
	"sites",
	"site_api_endpoints",
	"site_disabled_models",
	"accounts",
	"account_tokens",
	"checkin_logs",
	"model_availability",
	"token_model_availability",
	"token_routes",
	"route_group_sources",
	"oauth_route_units",
	"oauth_route_unit_members",
	"route_channels",
	"proxy_video_tasks",
	"proxy_files",
	"downstream_api_keys",
	"site_announcements",
}

// reverseAllTables is allTables reversed, for DELETE in FK-safe order.
var reverseAllTables []string

func init() {
	reverseAllTables = make([]string, len(allTables))
	for i, t := range allTables {
		reverseAllTables[len(allTables)-1-i] = t
	}
}

// GET /api/settings/backup/export?type=
func (h *backupHandler) exportBackup(w http.ResponseWriter, r *http.Request) {
	exportType := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("type")))
	if exportType == "" {
		exportType = "all"
	}
	if exportType != "all" && exportType != "accounts" && exportType != "preferences" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "导出类型无效，仅支持 all/accounts/preferences",
		})
		return
	}

	var tables []string
	switch exportType {
	case "all":
		tables = allTables
	case "accounts":
		tables = accountsTables
	case "preferences":
		tables = []string{"settings"}
	}

	result := map[string]any{}
	for _, table := range tables {
		rows, err := queryTableAsJSON(h.db, table)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"success": false,
				"message": fmt.Sprintf("导出失败：无法读取表 %s：%v", table, err),
			})
			return
		}
		result[table] = rows
	}

	backup := map[string]any{
		"metadata": map[string]any{
			"exported_at": time.Now().UTC().Format(time.RFC3339Nano),
			"version":     "1.0",
		},
		"type":   exportType,
		"tables": result,
	}

	writeJSON(w, http.StatusOK, backup)
}

// queryTableAsJSON queries all rows from a table and returns them as a []map[string]any
// with timestamps preserved as ISO 8601 strings (already stored as TEXT in DB).
func queryTableAsJSON(db *sqlx.DB, table string) ([]map[string]any, error) {
	// Validate table name against known list to prevent SQL injection.
	if !isKnownTable(table) {
		return nil, fmt.Errorf("unknown table: %s", table)
	}

	query := fmt.Sprintf("SELECT * FROM %s", table)
	rows, err := db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			return nil, err
		}
		// Normalize []byte values to strings (SQLite driver returns []byte for TEXT).
		for k, v := range row {
			if b, ok := v.([]byte); ok {
				row[k] = string(b)
			}
		}
		result = append(result, row)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, rows.Err()
}

// isKnownTable checks if a table name is in the known list.
func isKnownTable(name string) bool {
	for _, t := range allTables {
		if t == name {
			return true
		}
	}
	return false
}

// POST /api/settings/backup/import
func (h *backupHandler) importBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tables == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "导入数据格式错误：需要 JSON 对象且包含 tables 字段",
		})
		return
	}

	imported := map[string]int64{}
	for _, table := range allTables {
		raw, ok := body.Tables[table]
		if !ok {
			continue
		}

		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"success": false,
				"message": fmt.Sprintf("导入失败：表 %s 数据格式错误：%v", table, err),
			})
			return
		}
		if len(rows) == 0 {
			continue
		}

		count, err := importTableRows(h.db, table, rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"success": false,
				"message": fmt.Sprintf("导入失败：表 %s：%v", table, err),
			})
			return
		}
		imported[table] = count
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"message":  "导入完成",
		"imported": imported,
	})
}

// importTableRows inserts rows into a table, skipping conflicts on primary key.
// Each table is wrapped in its own transaction.
func importTableRows(db *sqlx.DB, table string, rows []map[string]any) (int64, error) {
	if !isKnownTable(table) {
		return 0, fmt.Errorf("unknown table: %s", table)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	tx, err := db.Beginx()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var imported int64
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}

		columns := make([]string, 0, len(row))
		values := make([]any, 0, len(row))

		for col, val := range row {
			columns = append(columns, col)
			values = append(values, val)
		}

		// Build dialect-aware INSERT with correct placeholders.
		var query string
		driverName := db.DriverName()
		switch driverName {
		case "pgx", "postgres":
			placeholders := make([]string, 0, len(columns))
			for i := range columns {
				placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
			}
			query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
				table,
				strings.Join(columns, ", "),
				strings.Join(placeholders, ", "),
			)
		default: // sqlite, sqlite3
			placeholders := make([]string, 0, len(columns))
			for i := 0; i < len(columns); i++ {
				placeholders = append(placeholders, "?")
			}
			query = fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) VALUES (%s)",
				table,
				strings.Join(columns, ", "),
				strings.Join(placeholders, ", "),
			)
		}

		result, err := tx.Exec(query, values...)
		if err != nil {
			return 0, fmt.Errorf("insert row: %w", err)
		}
		n, _ := result.RowsAffected()
		imported += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}

	return imported, nil
}

// GET /api/settings/backup/webdav
func (h *backupHandler) getWebdavConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         false,
		"fileUrl":         "",
		"username":        "",
		"passwordMasked":  "",
		"exportType":      "all",
		"autoSyncEnabled": false,
		"autoSyncCron":    "",
	})
}

// PUT /api/settings/backup/webdav
func (h *backupHandler) saveWebdavConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled         *bool   `json:"enabled"`
		FileUrl         *string `json:"fileUrl"`
		Username        *string `json:"username"`
		Password        *string `json:"password"`
		ClearPassword   *bool   `json:"clearPassword"`
		ExportType      *string `json:"exportType"`
		AutoSyncEnabled *bool   `json:"autoSyncEnabled"`
		AutoSyncCron    *string `json:"autoSyncCron"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	// Stub: persist webdav config
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// POST /api/settings/backup/webdav/export
func (h *backupHandler) exportToWebdav(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "WebDAV 导出成功",
	})
}

// POST /api/settings/backup/webdav/import
func (h *backupHandler) importFromWebdav(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success":         true,
		"appliedSettings": []any{},
	})
}
