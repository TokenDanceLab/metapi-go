package admin

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service/notify"
)

// RegisterNotifyRoutes registers the /api/settings/notify/test route.
func RegisterNotifyRoutes(r chi.Router) {
	r.Post("/api/settings/notify/test", testNotify)
}

// POST /api/settings/notify/test
// Dispatches a real connectivity test through all configured notification channels.
// Returns a clear 400 failure when no channel is configured or all sends fail.
func testNotify(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()

	result, err := notify.SendNotification(
		cfg,
		"测试通知",
		"您好，这是一条来自系统设置的连通性测试通知，您的通知相关配置目前工作正常！",
		"info",
		&notify.SendNotificationOptions{
			BypassThrottle: true,
			RequireChannel: true,
			ThrowOnFailure: true,
		},
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	message := fmt.Sprintf("测试通知已发送（成功 %d/%d）", result.Succeeded, result.Attempted)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": message,
	})
}
