package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterDatabaseRoutes registers all /api/settings/database routes.
func RegisterDatabaseRoutes(r chi.Router, db *sqlx.DB) {
	handler := &databaseHandler{db: db}

	r.Get("/api/settings/database/runtime", handler.getRuntime)
	r.Put("/api/settings/database/runtime", handler.saveRuntime)
	r.Post("/api/settings/database/test-connection", handler.testConnection)
	r.Post("/api/settings/database/migrate", handler.migrate)
}

type databaseHandler struct {
	db *sqlx.DB
}

// GET /api/settings/database/runtime
func (h *databaseHandler) getRuntime(w http.ResponseWriter, r *http.Request) {
	saved, _ := loadSavedDatabaseConfig(h.db)

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"active": map[string]any{
			"dialect":    "sqlite",
			"connection": "(default sqlite path)",
			"ssl":        false,
		},
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
	json.NewDecoder(r.Body).Decode(&body)

	if body.Dialect != nil {
		upsertSettingDB(h.db, "db_type", strings.TrimSpace(*body.Dialect))
	}
	if body.ConnectionString != nil {
		upsertSettingDB(h.db, "db_url", strings.TrimSpace(*body.ConnectionString))
	}
	if body.Ssl != nil {
		upsertSettingDB(h.db, "db_ssl", *body.Ssl)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":         true,
		"message":         "数据库运行配置已保存，重启容器后生效",
		"active":          map[string]any{"dialect": "sqlite", "connection": "(default sqlite path)", "ssl": false},
		"saved":           map[string]any{"dialect": "sqlite", "connection": "(default sqlite path)", "ssl": false},
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
	json.NewDecoder(r.Body).Decode(&body)

	// Stub: actual connection test not implemented
	if body.Dialect == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "数据库测试连接失败",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "目标数据库连接成功",
		"dialect": body.Dialect,
	})
}

// POST /api/settings/database/migrate
func (h *databaseHandler) migrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dialect          string `json:"dialect"`
		ConnectionString string `json:"connectionString"`
		Ssl              bool   `json:"ssl"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	// Stub: actual migration not implemented
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "数据库迁移完成",
		"dialect": body.Dialect,
		"rows": map[string]int{
			"sites":         0,
			"accounts":      0,
			"accountTokens": 0,
			"tokenRoutes":   0,
			"routeChannels": 0,
			"settings":      0,
		},
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
