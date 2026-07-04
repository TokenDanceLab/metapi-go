package admin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RegisterNotifyRoutes registers the /api/settings/notify/test route.
func RegisterNotifyRoutes(r chi.Router) {
	r.Post("/api/settings/notify/test", testNotify)
}

// POST /api/settings/notify/test
func testNotify(w http.ResponseWriter, r *http.Request) {
	// Stub: notification test not yet implemented
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "测试通知已发送（成功 0/0）",
	})
}
