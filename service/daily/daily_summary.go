package daily

import (
	"fmt"
	"math"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/service/checkin"
	notifypkg "github.com/tokendancelab/metapi-go/service/notify"
	"github.com/tokendancelab/metapi-go/store"
)

// DailySummaryMetrics holds daily summary statistics.
type DailySummaryMetrics struct {
	LocalDay          string
	GeneratedAtLocal  string
	TimeZone          string
	TotalAccounts     int
	ActiveAccounts    int
	LowBalanceAccounts int
	CheckinTotal      int
	CheckinSuccess    int
	CheckinSkipped    int
	CheckinFailed     int
	ProxyTotal        int
	ProxySuccess      int
	ProxyFailed       int
	ProxyTotalTokens  int64
	TodaySpend        float64
	TodayReward       float64
}

// CollectDailySummaryMetrics aggregates daily metrics from the database.
// Mirrors TS collectDailySummaryMetrics().
func CollectDailySummaryMetrics(cfg *config.Config, db *sqlx.DB, now time.Time) *DailySummaryMetrics {
	dayRange := service.GetLocalDayRangeUTC(now)

	// Accounts on active sites
	var accountRows []struct {
		store.Account
	}
	err := db.Select(&accountRows, `
		SELECT a.* FROM accounts a
		INNER JOIN sites s ON a.site_id = s.id
		WHERE s.status = 'active'
	`)
	if err != nil {
		return nil
	}

	activeAccounts := 0
	lowBalanceAccounts := 0
	for _, a := range accountRows {
		if a.Status == "active" {
			activeAccounts++
		}
		if a.Balance < 1 {
			lowBalanceAccounts++
		}
	}

	// Today's checkin logs
	var checkinRows []struct {
		store.CheckinLog
		AccountID int64   `db:"account_id"`
		ExtraConfig *string `db:"extra_config"`
	}
	err = db.Select(&checkinRows, `
		SELECT cl.*, a.extra_config FROM checkin_logs cl
		INNER JOIN accounts a ON cl.account_id = a.id
		INNER JOIN sites s ON a.site_id = s.id
		WHERE cl.created_at >= ? AND cl.created_at < ? AND s.status = 'active'
	`, dayRange.StartUTC, dayRange.EndUTC)
	if err != nil {
		checkinRows = nil
	}

	checkinSuccess := 0
	checkinSkipped := 0
	checkinFailed := 0
	rewardByAccount := make(map[int64]float64)
	successCountByAccount := make(map[int64]int)
	parsedRewardCountByAccount := make(map[int64]int)

	for _, row := range checkinRows {
		switch row.Status {
		case "success":
			checkinSuccess++
		case "skipped":
			checkinSkipped++
		case "failed":
			checkinFailed++
		default:
			checkinSuccess++
		}
		if row.Status == "success" {
			successCountByAccount[row.AccountID]++
			rewardVal := checkin.ParseCheckinRewardAmount(row.Reward)
			if rewardVal <= 0 && row.Message != nil {
				rewardVal = checkin.ParseCheckinRewardAmount(*row.Message)
			}
			if rewardVal > 0 {
				rewardByAccount[row.AccountID] += rewardVal
				parsedRewardCountByAccount[row.AccountID]++
			}
		}
	}

	// Today's proxy logs
	var proxyTotal, proxySuccess, proxyFailed int
	var proxyTotalTokens int64
	var todaySpend float64

	// Query proxy_logs for today's metrics
	type proxyLogAgg struct {
		Count        *int64   `db:"count"`
		SuccessCount *int64   `db:"success_count"`
		FailedCount  *int64   `db:"failed_count"`
		TotalTokens  *int64   `db:"total_tokens"`
		TotalCost    *float64 `db:"total_cost"`
	}
	var agg proxyLogAgg
	err = db.Get(&agg, `
		SELECT
			COUNT(*) AS count,
			COALESCE(SUM(CASE WHEN status = 'success' OR status IS NULL THEN 1 ELSE 0 END), 0) AS success_count,
			COALESCE(SUM(CASE WHEN status != 'success' AND status IS NOT NULL THEN 1 ELSE 0 END), 0) AS failed_count,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			COALESCE(SUM(estimated_cost), 0.0) AS total_cost
		FROM proxy_logs
		WHERE created_at >= ? AND created_at < ?
	`, dayRange.StartUTC, dayRange.EndUTC)
	if err == nil && agg.Count != nil {
		proxyTotal = int(*agg.Count)
		if agg.SuccessCount != nil {
			proxySuccess = int(*agg.SuccessCount)
		}
		if agg.FailedCount != nil {
			proxyFailed = int(*agg.FailedCount)
		}
		if agg.TotalTokens != nil {
			proxyTotalTokens = *agg.TotalTokens
		}
		if agg.TotalCost != nil {
			todaySpend = *agg.TotalCost
		}
	}

	// Calculate today reward using todayIncome fallback
	var todayReward float64
	for _, a := range accountRows {
		todayReward += service.EstimateRewardWithTodayIncomeFallback(service.EstimateRewardInput{
			Day:               dayRange.LocalDay,
			SuccessCount:      successCountByAccount[a.ID],
			ParsedRewardCount: parsedRewardCountByAccount[a.ID],
			RewardSum:         rewardByAccount[a.ID],
			ExtraConfig:       a.ExtraConfig,
		})
	}

	return &DailySummaryMetrics{
		LocalDay:           dayRange.LocalDay,
		GeneratedAtLocal:   service.FormatLocalDateTime(now),
		TimeZone:           service.GetResolvedTimeZone(),
		TotalAccounts:      len(accountRows),
		ActiveAccounts:     activeAccounts,
		LowBalanceAccounts: lowBalanceAccounts,
		CheckinTotal:       len(checkinRows),
		CheckinSuccess:     checkinSuccess,
		CheckinSkipped:     checkinSkipped,
		CheckinFailed:      checkinFailed,
		ProxyTotal:         proxyTotal,
		ProxySuccess:       proxySuccess,
		ProxyFailed:        proxyFailed,
		ProxyTotalTokens:   proxyTotalTokens,
		TodaySpend:         Round6(todaySpend),
		TodayReward:        Round6(todayReward),
	}
}

// BuildDailySummaryNotification builds the daily summary notification text.
// Mirrors TS buildDailySummaryNotification().
func BuildDailySummaryNotification(metrics *DailySummaryMetrics) (title, message string) {
	net := Round6(metrics.TodayReward - metrics.TodaySpend)
	title = fmt.Sprintf("每日总结 %s", metrics.LocalDay)
	message = fmt.Sprintf(
		"日期: %s\n生成时间: %s (%s)\n\n"+
			"账号概览: 总计 %d | 活跃 %d | 低余额(<$1) %d\n"+
			"签到统计: 总计 %d | 成功 %d | 跳过 %d | 失败 %d\n"+
			"代理统计: 总计 %d | 成功 %d | 失败 %d | Tokens %s\n"+
			"费用统计: 支出 $%s | 奖励 $%s | 净值 $%s",
		metrics.LocalDay, metrics.GeneratedAtLocal, metrics.TimeZone,
		metrics.TotalAccounts, metrics.ActiveAccounts, metrics.LowBalanceAccounts,
		metrics.CheckinTotal, metrics.CheckinSuccess, metrics.CheckinSkipped, metrics.CheckinFailed,
		metrics.ProxyTotal, metrics.ProxySuccess, metrics.ProxyFailed, formatTokens(metrics.ProxyTotalTokens),
		fmt.Sprintf("%.6f", metrics.TodaySpend),
		fmt.Sprintf("%.6f", metrics.TodayReward),
		fmt.Sprintf("%.6f", net),
	)
	return
}

// SendDailySummary collects metrics and sends the daily summary notification.
func SendDailySummary(cfg *config.Config, db *sqlx.DB) {
	now := time.Now()
	metrics := CollectDailySummaryMetrics(cfg, db, now)
	if metrics == nil {
		return
	}
	title, message := BuildDailySummaryNotification(metrics)
	notifypkg.SendNotification(cfg, title, message, "info", nil)
}

// Round6 rounds a value to 6 decimal places.
func Round6(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}

func formatTokens(n int64) string {
	if n == 0 {
		return "0"
	}
	result := ""
	s := fmt.Sprintf("%d", n)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}
