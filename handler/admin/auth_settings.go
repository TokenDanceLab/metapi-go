package admin

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
)

// RegisterAuthSettingsRoutes registers all /api/settings/auth routes.
func RegisterAuthSettingsRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	handler := &authSettingsHandler{db: db, cfg: cfg}

	r.Get("/api/settings/auth/info", handler.getInfo)
	r.Post("/api/settings/auth/change", handler.changeToken)
}

type authSettingsHandler struct {
	db  *sqlx.DB
	cfg *config.Config
}

// GET /api/settings/auth/info
func (h *authSettingsHandler) getInfo(w http.ResponseWriter, r *http.Request) {
	token := h.cfg.AuthToken
	var masked string
	if len(token) > 8 {
		masked = token[:4] + "****" + token[len(token)-4:]
	} else {
		masked = "****"
	}
	writeJSON(w, http.StatusOK, map[string]string{"masked": masked})
}

// POST /api/settings/auth/change
func (h *authSettingsHandler) changeToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OldToken string `json:"oldToken"`
		NewToken string `json:"newToken"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "请填写所有字段",
		})
		return
	}

	body.OldToken = strings.TrimSpace(body.OldToken)
	body.NewToken = strings.TrimSpace(body.NewToken)

	if body.OldToken == "" || body.NewToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "请填写所有字段",
		})
		return
	}

	if len(body.NewToken) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "新 Token 至少 6 个字符",
		})
		return
	}

	// Constant-time compare (matches AdminAuth middleware). Reject unequal
	// lengths after a dummy compare so length mismatches do not short-circuit
	// before crypto/subtle.ConstantTimeCompare.
	if !constantTimeTokenEqual(body.OldToken, h.cfg.AuthToken) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"success": false,
			"message": "旧 Token 验证失败",
		})
		return
	}

	// Persist to settings table
	now := timeNowUTC()
	var existingCount int
	h.db.Get(&existingCount, "SELECT COUNT(*) FROM settings WHERE key = 'auth_token'")
	if existingCount > 0 {
		h.db.Exec("UPDATE settings SET value = ? WHERE key = 'auth_token'", jsonQuote(body.NewToken))
	} else {
		h.db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "auth_token", jsonQuote(body.NewToken))
	}

	// Update runtime config
	h.cfg.AuthToken = body.NewToken

	// Defense-in-depth: expire HttpOnly meta_monitor_auth so browsers drop the
	// dead cookie after AuthToken rotation (HMAC already invalidates server-side).
	// Only on success — failed change must not clear a still-valid session cookie.
	clearMonitorAuthCookies(w, r)

	// Log the change event
	h.db.Exec(`INSERT INTO events (type, title, message, level, related_type, created_at, read)
		VALUES ('token', '管理员登录令牌已更新', '管理员登录 Token 已被修改，请使用新 Token 登录。', 'warning', 'settings', ?, 0)`, now)

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Token 已更新",
	})
}

// constantTimeTokenEqual compares a and b as UTF-8 bytes in constant time when
// lengths match. Mismatched lengths return false after a dummy compare so the
// early-reject path still exercises ConstantTimeCompare.
func constantTimeTokenEqual(a, b string) bool {
	ab, bb := []byte(a), []byte(b)
	if len(ab) != len(bb) {
		_ = subtle.ConstantTimeCompare(ab, ab)
		return false
	}
	return subtle.ConstantTimeCompare(ab, bb) == 1
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func timeNowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
