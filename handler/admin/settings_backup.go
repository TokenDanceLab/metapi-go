package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	backupsvc "github.com/tokendancelab/metapi-go/service/backup"
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

const (
	backupWebdavConfigSettingKey    = "backup_webdav_config_v1"
	backupWebdavStateSettingKey     = "backup_webdav_state_v1"
	backupWebdavDefaultAutoSyncCron = "0 */6 * * *"
	backupWebdavFetchTimeout        = 15 * time.Second
)

var backupWebdavImportMaxBytes int64 = 64 << 20

var allowPrivateWebdavTargets bool

var (
	backupImportMaxRowsPerTable  = 50_000
	backupImportMaxColumnsPerRow = 128
	backupImportMaxCellBytes     = 4 << 20
)

type webdavBackupConfig struct {
	Enabled         bool   `json:"enabled"`
	FileURL         string `json:"fileUrl"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	ExportType      string `json:"exportType"`
	AutoSyncEnabled bool   `json:"autoSyncEnabled"`
	AutoSyncCron    string `json:"autoSyncCron"`
}

var allTables = backupsvc.AllTables

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
	backup, err := buildBackupPayload(h.db, exportType)
	if err != nil {
		status := backupExportErrorStatus(err)
		writeJSON(w, status, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, backup)
}

var errInvalidBackupExportType = backupsvc.ErrInvalidExportType

func backupExportErrorStatus(err error) int {
	if errors.Is(err, errInvalidBackupExportType) {
		return http.StatusBadRequest
	}
	var limitErr backupsvc.ExportLimitError
	if errors.As(err, &limitErr) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusInternalServerError
}

func buildBackupPayload(db *sqlx.DB, exportType string) (map[string]any, error) {
	return backupsvc.BuildPayload(db, exportType)
}

// isKnownTable checks if a table name is in the known list.
func isKnownTable(name string) bool {
	return backupsvc.IsKnownTable(name)
}

// POST /api/settings/backup/import
func (h *backupHandler) importBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := decodeBackupImportRequest(r, &body); err != nil || body.Tables == nil {
		status := http.StatusBadRequest
		message := "导入数据格式错误：需要 JSON 对象且包含 tables 字段"
		var tooLarge webdavImportTooLargeError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
			message = err.Error()
		}
		writeJSON(w, status, map[string]any{
			"success": false,
			"message": message,
		})
		return
	}

	imported, err := importBackupTables(h.db, body.Tables)
	if err != nil {
		writeJSON(w, backupImportErrorStatus(err), map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"message":  "导入完成",
		"imported": imported,
	})
}

func decodeBackupImportRequest(r *http.Request, dst any) error {
	raw, err := readLimitedWebdavBody(r.Body, backupWebdavImportMaxBytes)
	if err != nil {
		return err
	}
	return decodeBackupPayload(raw, dst)
}

func importBackupTables(db *sqlx.DB, tables map[string]json.RawMessage) (map[string]int64, error) {
	if tables == nil {
		return nil, fmt.Errorf("导入数据格式错误：需要 JSON 对象且包含 tables 字段")
	}
	if err := validateBackupImportTableKeys(tables); err != nil {
		return nil, err
	}

	tx, err := db.Beginx()
	if err != nil {
		return nil, fmt.Errorf("begin import tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	imported, err := importBackupTablesWithConn(tx, tables)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit import tx: %w", err)
	}
	committed = true
	return imported, nil
}

func validateBackupImportTableKeys(tables map[string]json.RawMessage) error {
	for table := range tables {
		if !isKnownTable(table) {
			return backupImportClientError{message: fmt.Sprintf("导入失败：未知表 %s", table)}
		}
	}
	return nil
}

type backupImportConn interface {
	DriverName() string
	Queryx(query string, args ...any) (*sqlx.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

func importBackupTablesWithConn(conn backupImportConn, tables map[string]json.RawMessage) (map[string]int64, error) {
	imported := map[string]int64{}
	for _, table := range allTables {
		raw, ok := tables[table]
		if !ok {
			continue
		}

		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, backupImportClientError{message: fmt.Sprintf("导入失败：表 %s 数据格式错误：%v", table, err)}
		}
		if backupImportMaxRowsPerTable > 0 && len(rows) > backupImportMaxRowsPerTable {
			return nil, backupImportClientError{
				message: fmt.Sprintf("导入失败：表 %s 行数超过上限 %d", table, backupImportMaxRowsPerTable),
			}
		}
		if len(rows) == 0 {
			continue
		}

		count, err := importTableRowsWithConn(conn, table, rows)
		if err != nil {
			return nil, fmt.Errorf("导入失败：表 %s：%w", table, err)
		}
		imported[table] = count
	}
	return imported, nil
}

type backupImportClientError struct {
	message string
}

func (e backupImportClientError) Error() string {
	return e.message
}

func backupImportErrorStatus(err error) int {
	var clientErr backupImportClientError
	if errors.As(err, &clientErr) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// importTableRows inserts rows into a table, skipping conflicts on primary key.
// Direct calls wrap one table in its own transaction; backup imports use a shared transaction.
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
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	imported, err := importTableRowsWithConn(tx, table, rows)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	committed = true
	return imported, nil
}

func importTableRowsWithConn(conn backupImportConn, table string, rows []map[string]any) (int64, error) {
	if !isKnownTable(table) {
		return 0, fmt.Errorf("unknown table: %s", table)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	knownColumns, err := tableColumns(conn, table)
	if err != nil {
		return 0, err
	}

	var imported int64
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		if backupImportMaxColumnsPerRow > 0 && len(row) > backupImportMaxColumnsPerRow {
			return 0, backupImportClientError{
				message: fmt.Sprintf("row has %d columns, exceeds limit %d", len(row), backupImportMaxColumnsPerRow),
			}
		}
		if shouldSkipBackupImportRow(table, row) {
			continue
		}

		columns := make([]string, 0, len(row))
		values := make([]any, 0, len(row))

		for col, val := range row {
			if !knownColumns[col] {
				return 0, fmt.Errorf("unknown column %q for table %s", col, table)
			}
			if err := validateBackupImportCellValue(col, val); err != nil {
				return 0, err
			}
			columns = append(columns, col)
			values = append(values, val)
		}

		// Build dialect-aware INSERT with correct placeholders.
		var query string
		driverName := conn.DriverName()
		switch driverName {
		case "pgx", "postgres":
			placeholders := make([]string, 0, len(columns))
			for i := range columns {
				placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
			}
			query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
				quoteIdentifier(table),
				strings.Join(quoteIdentifiers(columns), ", "),
				strings.Join(placeholders, ", "),
			)
		default: // sqlite, sqlite3
			placeholders := make([]string, 0, len(columns))
			for i := 0; i < len(columns); i++ {
				placeholders = append(placeholders, "?")
			}
			query = fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) VALUES (%s)",
				quoteIdentifier(table),
				strings.Join(quoteIdentifiers(columns), ", "),
				strings.Join(placeholders, ", "),
			)
		}

		result, err := conn.Exec(query, values...)
		if err != nil {
			return 0, fmt.Errorf("insert row: %w", err)
		}
		n, _ := result.RowsAffected()
		imported += n
	}

	return imported, nil
}

func validateBackupImportCellValue(column string, value any) error {
	switch v := value.(type) {
	case nil, bool, float64:
		return nil
	case string:
		if backupImportMaxCellBytes > 0 && len(v) > backupImportMaxCellBytes {
			return backupImportClientError{
				message: fmt.Sprintf("column %q value exceeds limit %d bytes", column, backupImportMaxCellBytes),
			}
		}
		return nil
	default:
		return backupImportClientError{message: fmt.Sprintf("column %q must be a scalar JSON value", column)}
	}
}

var runtimeLocalSettingKeys = map[string]bool{
	"auth_token":             true,
	"backup_webdav_state_v1": true,
	"db_ssl":                 true,
	"db_type":                true,
	"db_url":                 true,
}

func shouldSkipBackupImportRow(table string, row map[string]any) bool {
	if table != "settings" {
		return false
	}
	key, ok := row["key"].(string)
	if !ok {
		return false
	}
	return runtimeLocalSettingKeys[key]
}

func tableColumns(conn backupImportConn, table string) (map[string]bool, error) {
	if !isKnownTable(table) {
		return nil, fmt.Errorf("unknown table: %s", table)
	}

	query := fmt.Sprintf("SELECT * FROM %s LIMIT 0", quoteIdentifier(table))
	rows, err := conn.Queryx(query)
	if err != nil {
		return nil, fmt.Errorf("read table columns: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read table columns: %w", err)
	}

	allowed := make(map[string]bool, len(cols))
	for _, col := range cols {
		allowed[col] = true
	}
	return allowed, nil
}

func quoteIdentifiers(columns []string) []string {
	quoted := make([]string, 0, len(columns))
	for _, col := range columns {
		quoted = append(quoted, quoteIdentifier(col))
	}
	return quoted
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// GET /api/settings/backup/webdav
func (h *backupHandler) getWebdavConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadWebdavBackupConfig(h.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "读取 WebDAV 配置失败",
		})
		return
	}

	writeWebdavConfigResponse(w, http.StatusOK, true, cfg, loadWebdavBackupState(h.db))
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	cfg, err := loadWebdavBackupConfig(h.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "读取 WebDAV 配置失败",
		})
		return
	}

	if body.Enabled != nil {
		cfg.Enabled = *body.Enabled
	}
	if body.FileUrl != nil {
		cfg.FileURL = strings.TrimSpace(*body.FileUrl)
	}
	if body.Username != nil {
		cfg.Username = *body.Username
	}
	if body.ClearPassword != nil && *body.ClearPassword {
		cfg.Password = ""
	} else if body.Password != nil {
		cfg.Password = *body.Password
	}
	if body.ExportType != nil {
		cfg.ExportType = strings.TrimSpace(strings.ToLower(*body.ExportType))
	}
	if body.AutoSyncEnabled != nil {
		cfg.AutoSyncEnabled = *body.AutoSyncEnabled
	}
	if body.AutoSyncCron != nil {
		cfg.AutoSyncCron = strings.TrimSpace(*body.AutoSyncCron)
	}
	normalizeWebdavBackupConfig(&cfg)

	if err := validateWebdavBackupConfig(cfg, false); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := saveWebdavBackupConfig(h.db, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "保存 WebDAV 配置失败",
		})
		return
	}

	writeWebdavConfigResponse(w, http.StatusOK, true, cfg, loadWebdavBackupState(h.db))
}

// POST /api/settings/backup/webdav/export
func (h *backupHandler) exportToWebdav(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadWebdavBackupConfig(h.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "读取 WebDAV 配置失败",
		})
		return
	}
	if err := validateWebdavBackupConfig(cfg, true); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var body struct {
		Type string `json:"type"`
	}
	if err := decodeOptionalJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}
	exportType := cfg.ExportType
	if strings.TrimSpace(body.Type) != "" {
		exportType = body.Type
	}

	backup, err := buildBackupPayload(h.db, exportType)
	if err != nil {
		status := backupExportErrorStatus(err)
		writeJSON(w, status, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	data, err := json.Marshal(backup)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "序列化备份数据失败",
		})
		return
	}

	client := newWebdavHTTPClient()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPut, cfg.FileURL, bytes.NewReader(data))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "WebDAV 文件 URL 无效",
		})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Username != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		state := updateWebdavBackupState(h.db, err)
		writeWebdavFailureResponse(w, http.StatusBadGateway, cfg, state, "WebDAV 导出请求失败")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		errMsg := sanitizeWebdavError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))), cfg)
		state := updateWebdavBackupState(h.db, errors.New(errMsg))
		writeWebdavFailureResponse(w, http.StatusBadGateway, cfg, state, errMsg)
		return
	}

	state := updateWebdavBackupState(h.db, nil)
	respPayload := webdavConfigResponsePayload(true, cfg, state)
	respPayload["message"] = "WebDAV 导出成功"
	respPayload["fileUrl"] = cfg.FileURL
	writeJSON(w, http.StatusOK, respPayload)
}

func loadWebdavBackupConfig(db *sqlx.DB) (webdavBackupConfig, error) {
	cfg := defaultWebdavBackupConfig()

	var raw string
	err := db.Get(&raw, db.Rebind("SELECT value FROM settings WHERE key = ?"), backupWebdavConfigSettingKey)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultWebdavBackupConfig(), nil
	}
	normalizeWebdavBackupConfig(&cfg)
	return cfg, nil
}

func saveWebdavBackupConfig(db *sqlx.DB, cfg webdavBackupConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return saveBackupSettingString(db, backupWebdavConfigSettingKey, string(data))
}

func saveBackupSettingString(db *sqlx.DB, key, value string) error {
	query := db.Rebind(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	_, err := db.Exec(query, key, value)
	return err
}

func defaultWebdavBackupConfig() webdavBackupConfig {
	return webdavBackupConfig{
		ExportType:   "all",
		AutoSyncCron: backupWebdavDefaultAutoSyncCron,
	}
}

func normalizeWebdavBackupConfig(cfg *webdavBackupConfig) {
	cfg.FileURL = strings.TrimSpace(cfg.FileURL)
	cfg.ExportType = strings.TrimSpace(strings.ToLower(cfg.ExportType))
	if cfg.ExportType == "" {
		cfg.ExportType = "all"
	}
	cfg.AutoSyncCron = strings.TrimSpace(cfg.AutoSyncCron)
	if cfg.AutoSyncCron == "" {
		cfg.AutoSyncCron = backupWebdavDefaultAutoSyncCron
	}
	if !cfg.Enabled {
		cfg.AutoSyncEnabled = false
	}
}

func validateWebdavBackupConfig(cfg webdavBackupConfig, requireEnabled bool) error {
	if cfg.ExportType != "all" && cfg.ExportType != "accounts" && cfg.ExportType != "preferences" {
		return errInvalidBackupExportType
	}
	if requireEnabled && !cfg.Enabled {
		return fmt.Errorf("WebDAV 未启用")
	}
	if (cfg.Enabled || requireEnabled) && !isValidWebdavFileURL(cfg.FileURL) {
		return fmt.Errorf("WebDAV 文件 URL 无效")
	}
	if cfg.AutoSyncEnabled && !cfg.Enabled {
		return fmt.Errorf("自动同步需要先启用 WebDAV")
	}
	return nil
}

func isValidWebdavFileURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Host == "" || parsed.User != nil {
		return false
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return isAllowedWebdavTargetHost(parsed.Hostname())
}

func decodeOptionalJSONRequest(r *http.Request, dst any) error {
	raw, err := readAdminJSONBody(r.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func writeWebdavConfigResponse(w http.ResponseWriter, status int, success bool, cfg webdavBackupConfig, state map[string]any) {
	writeJSON(w, status, webdavConfigResponsePayload(success, cfg, state))
}

func webdavConfigResponsePayload(success bool, cfg webdavBackupConfig, state map[string]any) map[string]any {
	config := maskedWebdavBackupConfig(cfg)
	payload := map[string]any{
		"success": success,
		"config":  config,
		"state":   state,
	}
	for k, v := range config {
		payload[k] = v
	}
	return payload
}

func maskedWebdavBackupConfig(cfg webdavBackupConfig) map[string]any {
	return map[string]any{
		"enabled":         cfg.Enabled,
		"fileUrl":         cfg.FileURL,
		"username":        cfg.Username,
		"password":        "",
		"hasPassword":     cfg.Password != "",
		"passwordMasked":  maskValue(cfg.Password),
		"exportType":      cfg.ExportType,
		"autoSyncEnabled": cfg.AutoSyncEnabled,
		"autoSyncCron":    cfg.AutoSyncCron,
	}
}

func loadWebdavBackupState(db *sqlx.DB) map[string]any {
	state := defaultWebdavBackupState()

	var raw string
	err := db.Get(&raw, db.Rebind("SELECT value FROM settings WHERE key = ?"), backupWebdavStateSettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return state
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return state
	}
	if v, ok := stored["lastSyncAt"]; ok {
		state["lastSyncAt"] = v
	}
	if v, ok := stored["lastAttemptAt"]; ok {
		state["lastAttemptAt"] = v
	}
	if v, ok := stored["lastError"]; ok {
		state["lastError"] = v
	}
	return state
}

func defaultWebdavBackupState() map[string]any {
	return map[string]any{
		"lastSyncAt":    nil,
		"lastAttemptAt": nil,
		"lastError":     nil,
	}
}

func updateWebdavBackupState(db *sqlx.DB, syncErr error) map[string]any {
	previous := loadWebdavBackupState(db)
	now := time.Now().UTC().Format(time.RFC3339)
	state := map[string]any{
		"lastSyncAt":    previous["lastSyncAt"],
		"lastAttemptAt": now,
		"lastError":     nil,
	}
	if syncErr != nil {
		state["lastError"] = syncErr.Error()
	} else {
		state["lastSyncAt"] = now
	}
	data, err := json.Marshal(state)
	if err == nil {
		_ = saveBackupSettingString(db, backupWebdavStateSettingKey, string(data))
	}
	return state
}

func writeWebdavFailureResponse(w http.ResponseWriter, status int, cfg webdavBackupConfig, state map[string]any, message string) {
	payload := webdavConfigResponsePayload(false, cfg, state)
	payload["message"] = sanitizeWebdavError(message, cfg)
	writeJSON(w, status, payload)
}

func sanitizeWebdavError(message string, cfg webdavBackupConfig) string {
	result := message
	if cfg.Password != "" {
		result = strings.ReplaceAll(result, cfg.Password, "****")
	}
	if cfg.Username != "" {
		result = strings.ReplaceAll(result, cfg.Username, "****")
	}
	return result
}

// POST /api/settings/backup/webdav/import
func (h *backupHandler) importFromWebdav(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadWebdavBackupConfig(h.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "读取 WebDAV 配置失败",
		})
		return
	}
	if err := validateWebdavBackupConfig(cfg, true); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	client := newWebdavHTTPClient()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, cfg.FileURL, nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "WebDAV 文件 URL 无效",
		})
		return
	}
	if cfg.Username != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		state := updateWebdavBackupState(h.db, errors.New(sanitizeWebdavError(err.Error(), cfg)))
		writeWebdavFailureResponse(w, http.StatusBadGateway, cfg, state, "WebDAV 导入请求失败")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		errMsg := sanitizeWebdavError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))), cfg)
		state := updateWebdavBackupState(h.db, errors.New(errMsg))
		writeWebdavFailureResponse(w, http.StatusBadGateway, cfg, state, errMsg)
		return
	}

	body, err := readLimitedWebdavBody(resp.Body, backupWebdavImportMaxBytes)
	if err != nil {
		errMsg := sanitizeWebdavError(err.Error(), cfg)
		state := updateWebdavBackupState(h.db, errors.New(errMsg))
		status := http.StatusBadRequest
		var tooLarge webdavImportTooLargeError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeWebdavFailureResponse(w, status, cfg, state, errMsg)
		return
	}

	var backup struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := decodeBackupPayload(body, &backup); err != nil || backup.Tables == nil {
		errMsg := "WebDAV 备份数据格式错误：需要 JSON 对象且包含 tables 字段"
		if err != nil {
			errMsg = fmt.Sprintf("%s：%v", errMsg, err)
		}
		state := updateWebdavBackupState(h.db, errors.New(errMsg))
		writeWebdavFailureResponse(w, http.StatusBadRequest, cfg, state, errMsg)
		return
	}

	imported, err := importBackupTables(h.db, backup.Tables)
	if err != nil {
		status := backupImportErrorStatus(err)
		state := updateWebdavBackupState(h.db, errors.New(sanitizeWebdavError(err.Error(), cfg)))
		writeWebdavFailureResponse(w, status, cfg, state, err.Error())
		return
	}

	state := updateWebdavBackupState(h.db, nil)
	payload := webdavConfigResponsePayload(true, cfg, state)
	payload["message"] = "WebDAV 导入完成"
	payload["imported"] = imported
	payload["appliedSettings"] = []any{}
	writeJSON(w, http.StatusOK, payload)
}

func newWebdavHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   backupWebdavFetchTimeout,
		Transport: newWebdavHTTPTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			if !isValidWebdavFileURL(req.URL.String()) {
				return fmt.Errorf("refusing WebDAV redirect to unsafe target")
			}
			if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("refusing WebDAV redirect from https to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

func newWebdavHTTPTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if !allowPrivateWebdavTargets {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				if err := rejectUnsafeWebdavDialHost(ctx, host); err != nil {
					return nil, err
				}
			}
			return dialer.DialContext(ctx, network, address)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: backupWebdavFetchTimeout,
		IdleConnTimeout:       30 * time.Second,
	}
}

func rejectUnsafeWebdavDialHost(ctx context.Context, host string) error {
	if !isAllowedWebdavTargetHost(host) {
		return fmt.Errorf("refusing WebDAV request to unsafe host %q", host)
	}
	if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses found for WebDAV host %q", host)
	}
	for _, ip := range ips {
		if isUnsafeWebdavAddr(ip) {
			return fmt.Errorf("refusing WebDAV request to unsafe resolved address %s", ip)
		}
	}
	return nil
}

func isAllowedWebdavTargetHost(host string) bool {
	if allowPrivateWebdavTargets {
		return true
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || strings.Contains(host, "%") {
		return false
	}
	lower := strings.TrimSuffix(strings.ToLower(host), ".")
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return !isUnsafeWebdavAddr(addr)
	}
	return true
}

func isUnsafeWebdavAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast()
}

func readLimitedWebdavBody(body io.Reader, maxBytes int64) ([]byte, error) {
	limited := &io.LimitedReader{R: body, N: maxBytes + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("读取 WebDAV 备份失败：%w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, webdavImportTooLargeError{maxBytes: maxBytes}
	}
	return data, nil
}

type webdavImportTooLargeError struct {
	maxBytes int64
}

func (e webdavImportTooLargeError) Error() string {
	return fmt.Sprintf("备份文件超过 %d 字节限制", e.maxBytes)
}

func decodeBackupPayload(raw []byte, dst any) error {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("backup payload must contain a single JSON value")
		}
		return err
	}
	return nil
}
