package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// RegisterDatabaseRoutes registers all /api/settings/database routes.
func RegisterDatabaseRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	handler := &databaseHandler{db: db, cfg: cfg}

	r.Get("/api/settings/database/runtime", handler.getRuntime)
	r.Put("/api/settings/database/runtime", handler.saveRuntime)
	r.Post("/api/settings/database/test-connection", handler.testConnection)
	r.Post("/api/settings/database/migrate", handler.migrate)
}

type databaseHandler struct {
	db  *sqlx.DB
	cfg *config.Config
}

const runtimeDatabaseConnectionTestTimeoutSec = "5"

func normalizeRuntimeDatabaseDialect(dialect string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "sqlite":
		return "sqlite", true
	case "postgres", "postgresql":
		return "postgres", true
	default:
		return "", false
	}
}

func testRuntimeDatabaseConnection(dialect string, connectionString string, ssl bool) (string, error) {
	trimmed := strings.TrimSpace(connectionString)
	if trimmed == "" {
		return "", fmt.Errorf("connection string is required")
	}

	dsn := trimmed
	if dialect == store.DialectSQLite {
		dsn = store.ResolveSQLitePath(trimmed, ".")
	} else if dialect == store.DialectPostgres {
		dsn = applyPostgresTestConnectTimeout(trimmed)
	}

	sslMode := ""
	if ssl {
		sslMode = "require"
	}
	db, err := store.OpenWithPostgresSSLMode(dialect, dsn, sslMode)
	if err != nil {
		return "", err
	}
	if err := db.Close(); err != nil {
		return "", err
	}
	return maskConnectionString(trimmed), nil
}

func applyPostgresTestConnectTimeout(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if strings.Contains(strings.ToLower(trimmed), "connect_timeout=") {
		return dsn
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		parsed, err := url.Parse(trimmed)
		if err == nil {
			q := parsed.Query()
			q.Set("connect_timeout", runtimeDatabaseConnectionTestTimeoutSec)
			parsed.RawQuery = q.Encode()
			return parsed.String()
		}
	}
	if trimmed == "" {
		return dsn
	}
	return strings.TrimRight(dsn, " ") + " connect_timeout=" + runtimeDatabaseConnectionTestTimeoutSec
}

func sanitizeConnectionError(err error, connectionString string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	raw := strings.TrimSpace(connectionString)
	if raw != "" {
		message = strings.ReplaceAll(message, raw, maskConnectionString(raw))
	}
	if at := strings.LastIndex(raw, "@"); at > 0 {
		for i := at - 1; i >= 0; i-- {
			if raw[i] == ':' {
				password := raw[i+1 : at]
				if password != "" {
					message = strings.ReplaceAll(message, password, "***")
				}
				break
			}
		}
	}
	return message
}

func activeRuntimeDatabaseConfig(cfg *config.Config) map[string]any {
	if cfg == nil {
		return map[string]any{
			"dialect":    store.DialectSQLite,
			"connection": "(default sqlite path)",
			"ssl":        false,
		}
	}

	dialect, ok := normalizeRuntimeDatabaseDialect(cfg.DbType)
	if !ok {
		dialect = store.DialectSQLite
	}

	connection := strings.TrimSpace(cfg.DbUrl)
	if dialect == store.DialectSQLite {
		if connection == "" {
			connection = "(default sqlite path)"
		}
	} else {
		connection = maskConnectionString(connection)
	}

	return map[string]any{
		"dialect":    dialect,
		"connection": connection,
		"ssl":        cfg.PostgresSSLMode() != "",
	}
}

// GET /api/settings/database/runtime
func (h *databaseHandler) getRuntime(w http.ResponseWriter, r *http.Request) {
	saved, _ := loadSavedDatabaseConfig(h.db)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":         true,
		"active":          activeRuntimeDatabaseConfig(h.cfg),
		"saved":           saved,
		"restartRequired": saved != nil,
	})
}

// PUT /api/settings/database/runtime
func (h *databaseHandler) saveRuntime(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dialect          *string `json:"dialect"`
		ConnectionString *string `json:"connectionString"`
		Ssl              *bool   `json:"ssl"`
		Overwrite        *bool   `json:"overwrite"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	if body.Dialect != nil {
		dialect, ok := normalizeRuntimeDatabaseDialect(*body.Dialect)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"success": false,
				"message": "数据库类型仅支持 sqlite 或 postgres",
			})
			return
		}
		upsertSettingDB(h.db, "db_type", dialect)
	}
	if body.ConnectionString != nil {
		upsertSettingDB(h.db, "db_url", strings.TrimSpace(*body.ConnectionString))
	}
	if body.Ssl != nil {
		upsertSettingDB(h.db, "db_ssl", *body.Ssl)
	}

	saved, _ := loadSavedDatabaseConfig(h.db)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":         true,
		"message":         "数据库运行配置已保存，重启容器后生效",
		"active":          activeRuntimeDatabaseConfig(h.cfg),
		"saved":           saved,
		"restartRequired": true,
	})
}

// POST /api/settings/database/test-connection
func (h *databaseHandler) testConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dialect          string `json:"dialect"`
		ConnectionString string `json:"connectionString"`
		Ssl              bool   `json:"ssl"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	dialect, ok := normalizeRuntimeDatabaseDialect(body.Dialect)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "数据库类型仅支持 sqlite 或 postgres",
		})
		return
	}

	maskedConnection, err := testRuntimeDatabaseConnection(dialect, body.ConnectionString, body.Ssl)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "数据库测试连接失败: " + sanitizeConnectionError(err, body.ConnectionString),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"message":    "目标数据库连接成功",
		"dialect":    dialect,
		"connection": maskedConnection,
	})
}

// POST /api/settings/database/migrate
func (h *databaseHandler) migrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dialect          string `json:"dialect"`
		ConnectionString string `json:"connectionString"`
		Ssl              bool   `json:"ssl"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	dialect, ok := normalizeRuntimeDatabaseDialect(body.Dialect)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "数据库类型仅支持 sqlite 或 postgres",
		})
		return
	}

	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"success": false,
		"message": "数据库迁移接口尚未接入当前 Go 运行时；请使用 metapi-migrate CLI 执行 SQLite 到 PostgreSQL 迁移",
		"dialect": dialect,
	})
}

func loadSavedDatabaseConfig(db *sqlx.DB) (map[string]any, error) {
	rows := queryRows(db, "SELECT key, value FROM settings WHERE key IN ('db_type', 'db_url', 'db_ssl')")
	config := map[string]any{}
	for _, row := range rows {
		key, _ := row["key"].(string)
		val, _ := row["value"].(string)
		if val != "" {
			var parsed any
			if err := json.Unmarshal([]byte(val), &parsed); err == nil {
				config[key] = parsed
			}
		}
	}

	dialect, _ := config["db_type"].(string)
	conn, _ := config["db_url"].(string)
	if dialect == "" || conn == "" {
		return nil, nil
	}

	return map[string]any{
		"dialect":    dialect,
		"connection": maskConnectionString(conn),
		"ssl":        config["db_ssl"],
	}, nil
}

func maskConnectionString(conn string) string {
	if conn == "" {
		return ""
	}
	// Mask password in connection string
	// Simple approach: redact everything after @ for display
	if idx := strings.LastIndex(conn, "@"); idx >= 0 {
		return "****@" + conn[idx+1:]
	}
	return "****"
}
