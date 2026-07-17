package alert

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service"
	notifypkg "github.com/tokendancelab/metapi-go/service/notify"
)

// TokenExpiredParams holds parameters for reportTokenExpired.
type TokenExpiredParams struct {
	AccountID int64
	Username  *string
	SiteName  *string
	Detail    string
}

// ReportTokenExpired reports a token expiration event and marks the account expired.
// Callers MUST gate with ShouldMarkAccountExpired (ClassExpired only) — not the
// broader IsTokenExpiredError / auto-relogin heuristic — so 429/5xx/network/billing
// never force-mark keys (#568 / #298).
// Mirrors TS reportTokenExpired() with tighter call-site guards.
func ReportTokenExpired(cfg *config.Config, db *sqlx.DB, params TokenExpiredParams) {
	accountLabel := orID(params.Username, params.AccountID)
	siteLabel := "unknown-site"
	if params.SiteName != nil {
		siteLabel = *params.SiteName
	}

	detailText := ""
	if params.Detail != "" {
		detailText = AppendSessionTokenRebindHint(params.Detail)
	}
	detail := ""
	if detailText != "" {
		detail = fmt.Sprintf(" (%s)", detailText)
	}

	createdAt := service.FormatUtcSqlDateTime(time.Now())

	// Write events
	_ = createdAt
	service.CreateEvent(db, "token", "Token 已失效",
		fmt.Sprintf("%s @ %s 的 Token 无效或已过期%s", accountLabel, siteLabel, detail),
		"error", params.AccountID, "account")

	// Update account status
	db.Exec("UPDATE accounts SET status = 'expired', updated_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339), params.AccountID)

	// Set runtime health
	healthReason := "访问令牌失效"
	if detailText != "" {
		healthReason = "访问令牌失效：" + detailText
	}
	service.SetAccountRuntimeHealth(db, params.AccountID, service.RuntimeHealthEntry{
		State: service.HealthUnhealthy, Reason: healthReason, Source: service.HealthSourceAuth,
	})

	// Send notification
	notifypkg.SendNotification(cfg, "Token 已失效",
		fmt.Sprintf("%s @ %s 的 Token 无效或已过期%s", accountLabel, siteLabel, detail),
		"error", nil)
}

// ProxyAllFailedParams holds parameters for reportProxyAllFailed.
type ProxyAllFailedParams struct {
	Model  string
	Reason string
}

// ReportProxyAllFailed reports a proxy all-failed event.
// Mirrors TS reportProxyAllFailed().
func ReportProxyAllFailed(cfg *config.Config, db *sqlx.DB, params ProxyAllFailedParams) {
	createdAt := service.FormatUtcSqlDateTime(time.Now())

	service.CreateEvent(db, "proxy", "代理全部失败",
		fmt.Sprintf("模型=%s, 原因=%s", params.Model, params.Reason),
		"error", 0, "route")

	notifypkg.SendNotification(cfg, "代理全部失败",
		fmt.Sprintf("模型=%s, 原因=%s", params.Model, params.Reason),
		"error", nil)

	_ = createdAt // already used above
}

func orID(username *string, id int64) string {
	if username != nil && *username != "" {
		return *username
	}
	return fmt.Sprintf("ID:%d", id)
}
