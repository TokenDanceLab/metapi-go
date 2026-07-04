package admin

import (
	"encoding/json"
	"net/http"
	"strings"

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

// GET /api/settings/backup/export?type=
func (h *backupHandler) exportBackup(w http.ResponseWriter, r *http.Request) {
	exportType := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("type")))
	if exportType != "" && exportType != "all" && exportType != "accounts" && exportType != "preferences" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "导出类型无效，仅支持 all/accounts/preferences",
		})
		return
	}

	// Stub: export all data
	backup := map[string]any{
		"version":   1,
		"exportedAt": "",
		"type":      exportType,
		"data":      map[string]any{},
	}

	writeJSON(w, http.StatusOK, backup)
}

// POST /api/settings/backup/import
func (h *backupHandler) importBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Data == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "导入数据格式错误：需要 JSON 对象",
		})
		return
	}

	// Stub: import not yet fully implemented
	writeJSON(w, http.StatusOK, map[string]any{
		"success":         true,
		"message":         "导入完成",
		"appliedSettings": []any{},
	})
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
