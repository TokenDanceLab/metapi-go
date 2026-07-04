package checkin

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/service/adapter"
	"github.com/tokendancelab/metapi-go/service/alert"
	"github.com/tokendancelab/metapi-go/service/balance"
	notifypkg "github.com/tokendancelab/metapi-go/service/notify"
	"github.com/tokendancelab/metapi-go/store"
)

// CheckinExecutionStatus is the status of a checkin execution.
type CheckinExecutionStatus string

const (
	CheckinSuccess CheckinExecutionStatus = "success"
	CheckinFailed  CheckinExecutionStatus = "failed"
	CheckinSkipped CheckinExecutionStatus = "skipped"
)

// CheckinOptions configures checkin behavior.
type CheckinOptions struct {
	SkipEvent    bool
	ScheduleMode string // "cron" or "interval"
}

// CheckinResult is the result of a single account checkin.
type CheckinResult struct {
	Success bool
	Status  CheckinExecutionStatus
	Skipped bool
	Reason  string
	Message string
	Reward  string
}

// CheckinAllResult is a result entry for CheckinAll.
type CheckinAllResult struct {
	AccountID int64
	Username  string
	Site      string
	Result    CheckinResult
}

// IsSiteDisabled checks if a site status represents "disabled".
func IsSiteDisabled(status string) bool {
	normalized := strings.TrimSpace(status)
	if normalized == "" {
		normalized = "active"
	}
	return normalized == "disabled"
}

// isAlreadyCheckedInMessage detects "already checked in" patterns in 12 languages/forms.
func isAlreadyCheckedInMessage(message string) bool {
	text := strings.TrimSpace(message)
	if text == "" {
		return false
	}
	normalized := strings.ToLower(text)
	return strings.Contains(normalized, "already checked in") ||
		strings.Contains(normalized, "already signed") ||
		strings.Contains(normalized, "already sign in") ||
		strings.Contains(text, "今日已签到") ||
		strings.Contains(text, "今天已签到") ||
		strings.Contains(text, "今天已经签到") ||
		strings.Contains(text, "今日已经签到") ||
		strings.Contains(text, "已经签到") ||
		strings.Contains(text, "已签到") ||
		strings.Contains(text, "重复签到") ||
		strings.Contains(text, "签到达")
}

// isUnsupportedCheckinMessage detects unsupported checkin endpoints.
func isUnsupportedCheckinMessage(message string) bool {
	if message == "" {
		return false
	}
	text := strings.ToLower(message)
	return strings.Contains(text, "invalid url (post /api/user/checkin)") ||
		(strings.Contains(text, "http 404") && strings.Contains(text, "/api/user/checkin")) ||
		strings.Contains(text, "checkin endpoint not found") ||
		strings.Contains(text, "check-in is not supported") ||
		strings.Contains(text, "checkin is not supported") ||
		strings.Contains(text, "does not support checkin") ||
		strings.Contains(text, "not support checkin")
}

// isManualVerificationRequiredMessage detects Turnstile verification messages.
func isManualVerificationRequiredMessage(message string) bool {
	if message == "" {
		return false
	}
	text := strings.ToLower(message)
	return strings.Contains(text, "turnstile token 为空") ||
		(strings.Contains(text, "turnstile") && (strings.Contains(text, "token") || strings.Contains(text, "校验") || strings.Contains(text, "验证")))
}

// shouldAttemptAutoRelogin checks if auto-relogin should be attempted for checkin.
func shouldAttemptAutoRelogin(message string) bool {
	if message == "" {
		return false
	}
	if alert.IsTokenExpiredError(0, message) {
		return true
	}
	text := strings.ToLower(message)
	return strings.Contains(text, "new-api-user") || strings.Contains(text, "access token")
}

// tryAutoRelogin attempts to re-login and get a new access token.
func tryAutoRelogin(cfg *config.Config, db *sqlx.DB, account *store.Account, site *store.Site) (string, error) {
	adp := adapter.GetAdapter(site.Platform)
	if adp == nil {
		return "", fmt.Errorf("no adapter for platform %s", site.Platform)
	}

	relogin := service.GetAutoReloginConfig(account.ExtraConfig)
	if relogin == nil {
		return "", fmt.Errorf("no auto-relogin config")
	}

	password := service.DecryptPassword(cfg, relogin.PasswordCipher)
	if password == "" {
		return "", fmt.Errorf("failed to decrypt password")
	}

	proxyURL := service.GetProxyURLFromExtraConfig(account.ExtraConfig)

	result, err := adp.Login(site.URL, relogin.Username, password, proxyURL)
	if err != nil || !result.Success || result.AccessToken == "" {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("login failed: %s", result.Message)
	}

	// Update DB
	newStatus := account.Status
	if account.Status == "expired" {
		newStatus = "active"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		"UPDATE accounts SET access_token = ?, status = ?, updated_at = ? WHERE id = ?",
		result.AccessToken, newStatus, now, account.ID,
	)
	if err != nil {
		return "", err
	}

	return result.AccessToken, nil
}

// CheckinAccount performs a checkin for a single account.
// Mirrors TS checkinAccount().
func CheckinAccount(cfg *config.Config, db *sqlx.DB, accountID int64, options *CheckinOptions) CheckinResult {
	if options == nil {
		options = &CheckinOptions{}
	}

	// 1. Load account + site
	aws, err := service.GetAccountWithSiteByID(db, accountID)
	if err != nil {
		return CheckinResult{Success: false, Message: "account not found", Status: CheckinFailed}
	}
	account := &aws.Account
	site := &aws.Site

	// 2. Site disabled check
	if IsSiteDisabled(site.Status) {
		createdAt := service.FormatUtcSqlDateTime(time.Now())
		service.SetAccountRuntimeHealth(db, account.ID, service.RuntimeHealthEntry{
			State: service.HealthDisabled, Reason: "站点已禁用", Source: service.HealthSourceCheckin,
		})

		db.Exec("INSERT INTO checkin_logs (account_id, status, message, created_at) VALUES (?, ?, ?, ?)",
			accountID, "skipped", "site disabled", createdAt)

		if !options.SkipEvent {
			msg := fmt.Sprintf("%s @ %s: site disabled", orUsername(account.Username, accountID), site.Name)
			service.CreateEvent(db, "checkin", "checkin skipped", msg, "info", accountID, "account")
		}

		return CheckinResult{
			Success: true, Status: CheckinSkipped, Skipped: true,
			Reason: "site_disabled", Message: "site disabled",
		}
	}

	// 3. Get platform adapter
	adp := adapter.GetAdapter(site.Platform)
	if adp == nil {
		return CheckinResult{
			Success: false, Status: CheckinFailed,
			Message: fmt.Sprintf("unsupported platform: %s", site.Platform),
		}
	}

	// 4. Resolve platformUserId and proxy
	_, hasStored := service.GetPlatformUserIdFromExtraConfig(account.ExtraConfig)
	var guessedPlatformUserID int64
	if !hasStored {
		guessedPlatformUserID = service.GuessPlatformUserIdFromUsername(account.Username)
	}
	platformUserID := service.ResolvePlatformUserID(account.ExtraConfig, account.Username)
	proxyURL := service.GetProxyURLFromExtraConfig(account.ExtraConfig)

	// 5. First checkin attempt
	activeAccessToken := account.AccessToken
	result, err := adp.Checkin(site.URL, activeAccessToken, platformUserID, proxyURL)
	if err != nil {
		result = &adapter.CheckinResult{Success: false, Message: err.Error()}
	}

	// 6. Auto-relogin on failure
	if !result.Success && shouldAttemptAutoRelogin(result.Message) {
		refreshedToken, reloginErr := tryAutoRelogin(cfg, db, account, site)
		if reloginErr == nil && refreshedToken != "" {
			activeAccessToken = refreshedToken
			result, err = adp.Checkin(site.URL, activeAccessToken, platformUserID, proxyURL)
			if err != nil {
				result = &adapter.CheckinResult{Success: false, Message: err.Error()}
			}
		}
	}

	// 7. Classify result
	isCloudflare := alert.IsCloudflareChallenge(result.Message)
	alreadyCheckedIn := isAlreadyCheckedInMessage(result.Message)
	unsupportedCheckin := isUnsupportedCheckinMessage(result.Message)
	manualVerificationRequired := isManualVerificationRequiredMessage(result.Message)
	manualVerificationMessage := "站点开启了 Turnstile 校验，需要人工签到"
	logMessage := result.Message
	if manualVerificationRequired {
		logMessage = manualVerificationMessage
	}
	effectiveSuccess := result.Success || alreadyCheckedIn || unsupportedCheckin || manualVerificationRequired
	shouldRefreshBalance := result.Success || alreadyCheckedIn
	directCheckinSuccess := result.Success && !alreadyCheckedIn && !unsupportedCheckin
	shouldAdvanceLastCheckinAt := directCheckinSuccess || (alreadyCheckedIn && options.ScheduleMode != "interval")

	normalizedStatus := CheckinSuccess
	if !effectiveSuccess {
		normalizedStatus = CheckinFailed
	} else if unsupportedCheckin || manualVerificationRequired {
		normalizedStatus = CheckinSkipped
	}

	logReward := result.Reward
	var refreshedBalanceInfo *adapter.BalanceInfo

	// 8. Post-success processing
	if effectiveSuccess {
		healthState := service.HealthHealthy
		healthReason := ""
		if unsupportedCheckin {
			healthState = service.HealthDegraded
			healthReason = "站点不支持签到接口"
		} else if manualVerificationRequired {
			healthState = service.HealthDegraded
			healthReason = manualVerificationMessage
		} else if alreadyCheckedIn {
			healthState = service.HealthHealthy
			healthReason = "今日已签到"
		} else {
			healthState = service.HealthHealthy
			healthReason = "签到成功"
			if result.Message != "" {
				healthReason = result.Message
			}
		}
		service.SetAccountRuntimeHealth(db, account.ID, service.RuntimeHealthEntry{
			State: healthState, Reason: healthReason, Source: service.HealthSourceCheckin,
		})

		// Update account fields
		var setClauses []string
		var args []any

		if shouldAdvanceLastCheckinAt {
			now := time.Now().UTC().Format(time.RFC3339)
			setClauses = append(setClauses, "last_checkin_at = ?")
			args = append(args, now)
		}

		if !hasStored && guessedPlatformUserID != 0 {
			newConfig := service.MergeExtraConfig(account.ExtraConfig, map[string]any{
				"platformUserId": float64(guessedPlatformUserID),
			})
			if newConfig != nil {
				setClauses = append(setClauses, "extra_config = ?")
				args = append(args, *newConfig)
			}
		}

		if account.Status == "expired" {
			setClauses = append(setClauses, "status = ?", "updated_at = ?")
			args = append(args, "active", time.Now().UTC().Format(time.RFC3339))
		}

		if len(setClauses) > 0 {
			query := "UPDATE accounts SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
			args = append(args, accountID)
			db.Exec(query, args...)
		}

		// Refresh balance if needed
		if shouldRefreshBalance {
			balanceResult, balErr := balance.RefreshBalance(cfg, db, accountID)
			if balErr == nil && balanceResult != nil {
				refreshedBalanceInfo = balanceResult.BalanceInfo
			}
		}

		// Parse reward
		parsedReward := ParseCheckinRewardAmount(logReward)
		if parsedReward <= 0 {
			parsedReward = ParseCheckinRewardAmount(result.Message)
			if parsedReward > 0 {
				logReward = fmt.Sprintf("%v", parsedReward)
			}
		}
		if directCheckinSuccess && parsedReward <= 0 {
			if refreshedBalanceInfo != nil {
				inferredReward := InferRewardFromBalanceDelta(account.Balance, refreshedBalanceInfo.Balance)
				if inferredReward > 0 {
					parsedReward = inferredReward
					logReward = fmt.Sprintf("%v", inferredReward)
				}
			}
		}
	}

	// 9. Write checkin_logs
	createdAt := service.FormatUtcSqlDateTime(time.Now())
	db.Exec("INSERT INTO checkin_logs (account_id, status, message, reward, created_at) VALUES (?, ?, ?, ?, ?)",
		accountID, string(normalizedStatus), logMessage, logReward, createdAt)

	// 10. Write events
	if !options.SkipEvent {
		eventTitle := "checkin success"
		eventLevel := "info"
		if !effectiveSuccess {
			if isCloudflare {
				eventTitle = "checkin failed (cloudflare challenge)"
			} else {
				eventTitle = "checkin failed"
			}
			eventLevel = "error"
		} else if normalizedStatus == CheckinSkipped {
			eventTitle = "checkin skipped"
		}
		eventMsg := fmt.Sprintf("%s @ %s: %s", orUsername(account.Username, accountID), site.Name, logMessage)
		service.CreateEvent(db, "checkin", eventTitle, eventMsg, eventLevel, accountID, "account")
	}

	// 11. Post-failure processing
	if !effectiveSuccess {
		service.SetAccountRuntimeHealth(db, account.ID, service.RuntimeHealthEntry{
			State: service.HealthUnhealthy, Reason: result.Message,
			Source: service.HealthSourceCheckin,
		})

		if alert.IsTokenExpiredError(0, result.Message) {
			alert.ReportTokenExpired(cfg, db, alert.TokenExpiredParams{
				AccountID: account.ID, Username: account.Username,
				SiteName: &site.Name, Detail: result.Message,
			})
		}

		if isCloudflare {
			notifypkg.SendNotification(cfg,
				"Cloudflare challenge",
				fmt.Sprintf("%s @ %s: %s", orUsername(account.Username, accountID), site.Name, result.Message),
				"warning", nil,
			)
		}

		if !unsupportedCheckin && !manualVerificationRequired {
			notifypkg.SendNotification(cfg,
				"checkin failed",
				fmt.Sprintf("%s @ %s: %s", orUsername(account.Username, accountID), site.Name, result.Message),
				"error", nil,
			)
		}
	}

	return CheckinResult{
		Success: effectiveSuccess,
		Status:  normalizedStatus,
		Skipped: normalizedStatus == CheckinSkipped,
		Message: logMessage,
		Reward:  logReward,
	}
}

// CheckinAll performs checkin for all eligible accounts.
// Mirrors TS checkinAll().
func CheckinAll(cfg *config.Config, db *sqlx.DB, accountIDs []int64, scheduleMode string) []CheckinAllResult {
	query := `SELECT a.id AS "accounts.id", a.site_id AS "accounts.site_id", a.username AS "accounts.username",
		a.access_token AS "accounts.access_token", a.balance AS "accounts.balance",
		a.balance_used AS "accounts.balance_used", a.quota AS "accounts.quota",
		a.status AS "accounts.status", a.checkin_enabled AS "accounts.checkin_enabled",
		a.last_checkin_at AS "accounts.last_checkin_at", a.extra_config AS "accounts.extra_config",
		s.id AS "sites.id", s.name AS "sites.name", s.url AS "sites.url",
		s.platform AS "sites.platform", s.status AS "sites.status"
		FROM accounts a INNER JOIN sites s ON a.site_id = s.id
		WHERE a.checkin_enabled = 1 AND a.status = 'active'`

	var rows []struct {
		Accounts struct {
			ID             int64   `db:"id"`
			SiteID         int64   `db:"site_id"`
			Username       *string `db:"username"`
			AccessToken    string  `db:"access_token"`
			Balance        float64 `db:"balance"`
			BalanceUsed    float64 `db:"balance_used"`
			Quota          float64 `db:"quota"`
			Status         string  `db:"status"`
			CheckinEnabled bool    `db:"checkin_enabled"`
			LastCheckinAt  *string `db:"last_checkin_at"`
			ExtraConfig    *string `db:"extra_config"`
		} `db:"accounts"`
		Sites struct {
			ID       int64  `db:"id"`
			Name     string `db:"name"`
			URL      string `db:"url"`
			Platform string `db:"platform"`
			Status   string `db:"status"`
		} `db:"sites"`
	}

	if err := db.Select(&rows, query); err != nil {
		slog.Error("CheckinAll: failed to query accounts", "error", err)
		return nil
	}

	// Filter by accountIDs if provided
	scopedIDs := make(map[int64]bool)
	if len(accountIDs) > 0 {
		for _, id := range accountIDs {
			scopedIDs[id] = true
		}
	}

	// Group by siteId
	grouped := make(map[int64][]int)
	for i, row := range rows {
		if len(scopedIDs) > 0 && !scopedIDs[row.Accounts.ID] {
			continue
		}
		siteID := row.Sites.ID
		grouped[siteID] = append(grouped[siteID], i)
	}

	var results []CheckinAllResult
	var mu sync.Mutex

	// Different sites: parallel. Same site: serial.
	var wg sync.WaitGroup
	for _, indices := range grouped {
		wg.Add(1)
		go func(indices []int) {
			defer wg.Done()
			for _, idx := range indices {
				row := rows[idx]
				result := CheckinAccount(cfg, db, row.Accounts.ID, &CheckinOptions{
					SkipEvent:    true,
					ScheduleMode: scheduleMode,
				})

				username := ""
				if row.Accounts.Username != nil {
					username = *row.Accounts.Username
				}

				mu.Lock()
				results = append(results, CheckinAllResult{
					AccountID: row.Accounts.ID,
					Username:  username,
					Site:      row.Sites.Name,
					Result:    result,
				})
				mu.Unlock()
			}
		}(indices)
	}
	wg.Wait()

	return results
}

func orUsername(username *string, id int64) string {
	if username != nil && strings.TrimSpace(*username) != "" {
		return *username
	}
	return fmt.Sprintf("ID:%d", id)
}
