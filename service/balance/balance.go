package balance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/service/adapter"
	"github.com/tokendancelab/metapi-go/service/alert"
	"github.com/tokendancelab/metapi-go/store"
)

// IsSiteDisabled checks if a site is disabled.
func IsSiteDisabled(status string) bool {
	normalized := strings.TrimSpace(status)
	if normalized == "" {
		normalized = "active"
	}
	return strings.EqualFold(normalized, "disabled")
}

func isAccountDisabled(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "disabled")
}

// sub2apiRefreshSingleflight provides deduplication for concurrent Sub2Api token refreshes.
var (
	sub2apiRefreshMu       sync.Mutex
	sub2apiRefreshInFlight = map[int64]*sub2apiRefreshPromise{}
)

type sub2apiRefreshPromise struct {
	done   chan struct{}
	once   sync.Once
	result sub2apiRefreshResult
}

type sub2apiRefreshResult struct {
	accessToken string
	err         error
}

func newSub2APIRefreshPromise() *sub2apiRefreshPromise {
	return &sub2apiRefreshPromise{done: make(chan struct{})}
}

func (p *sub2apiRefreshPromise) wait() sub2apiRefreshResult {
	<-p.done
	return p.result
}

func (p *sub2apiRefreshPromise) resolve(result sub2apiRefreshResult) {
	p.once.Do(func() {
		p.result = result
		close(p.done)
	})
}

// refreshSub2ApiManagedSessionSingleflight refreshes a Sub2Api managed session token.
// Uses singleflight pattern: concurrent callers for the same accountID wait on the first call.
func refreshSub2ApiManagedSessionSingleflight(cfg *config.Config, db *sqlx.DB, accountID int64, account *store.Account, site *store.Site, managedAuth map[string]any) (string, error) {
	sub2apiRefreshMu.Lock()
	if p, ok := sub2apiRefreshInFlight[accountID]; ok {
		sub2apiRefreshMu.Unlock()
		result := p.wait()
		return result.accessToken, result.err
	}
	p := newSub2APIRefreshPromise()
	sub2apiRefreshInFlight[accountID] = p
	sub2apiRefreshMu.Unlock()

	defer func() {
		sub2apiRefreshMu.Lock()
		delete(sub2apiRefreshInFlight, accountID)
		sub2apiRefreshMu.Unlock()
	}()

	token, err := tryAutoReloginBalance(cfg, db, account, site)
	p.resolve(sub2apiRefreshResult{accessToken: token, err: err})
	return token, err
}

// shouldAttemptAutoReloginBalance checks if auto-relogin should be attempted for balance refresh.
// Has extra patterns beyond the checkin version: unauthorized, forbidden, not login, not logged.
func shouldAttemptAutoReloginBalance(message string) bool {
	if message == "" {
		return false
	}
	if alert.IsTokenExpiredError(0, message) {
		return true
	}
	text := strings.ToLower(message)
	return strings.Contains(text, "access token") ||
		strings.Contains(text, "new-api-user") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "forbidden") ||
		strings.Contains(text, "not login") ||
		strings.Contains(text, "not logged")
}

// income log fallback config
const incomeLogResponseBodyLimit = 1 << 20

var supportedIncomeLogPlatforms = map[string]bool{
	"new-api":   true,
	"anyrouter": true,
	"one-api":   true,
	"veloera":   true,
}

func supportsTodayIncomeLogFallback(platform string) bool {
	return supportedIncomeLogPlatforms[strings.ToLower(platform)]
}

func resolveQuotaConversionFactor(platform string) float64 {
	if strings.ToLower(platform) == "veloera" {
		return 1_000_000
	}
	return 500_000
}

// fetchTodayIncomeFromLogs fetches today's income from /api/log/self endpoints.
func fetchTodayIncomeFromLogs(baseURL, accessToken, platformName string, platformUserID int64, proxyConfig *platform.ProxyConfig) (float64, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	accessToken = strings.TrimSpace(accessToken)
	if baseURL == "" || accessToken == "" {
		return 0, fmt.Errorf("empty baseURL or accessToken")
	}

	start, end := service.GetTodayUnixSecondsRange(time.Now())
	conversionFactor := resolveQuotaConversionFactor(platformName)
	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
		"Content-Type":  "application/json",
	}
	if platformUserID > 0 {
		headers["New-Api-User"] = strconv.FormatInt(platformUserID, 10)
	}

	logTypes := []int{1, 4}
	const pageSize = 100
	const maxPages = 6
	hasAnyResponse := false
	totalIncome := 0.0

	for _, logType := range logTypes {
		for page := 1; page <= maxPages; page++ {
			query := fmt.Sprintf("p=%d&page_size=%d&type=%d&token_name=&model_name=&start_timestamp=%d&end_timestamp=%d&group=",
				page, pageSize, logType, start, end)
			reqURL := baseURL + "/api/log/self?" + query

			req, err := http.NewRequest(http.MethodGet, reqURL, nil)
			if err != nil {
				break
			}
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			resp, err := platform.DoWithProxy(context.Background(), req, proxyConfig)
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil {
					resp.Body.Close()
				}
				break
			}

			var payload map[string]any
			if err := decodeIncomeLogPayload(resp.Body, &payload); err != nil {
				resp.Body.Close()
				break
			}
			resp.Body.Close()

			if payload == nil {
				break
			}
			hasAnyResponse = true

			items := extractLogItems(payload)
			for _, item := range items {
				quotaRaw := parsePositiveNumberAny(item["quota"])
				if quotaRaw > 0 {
					totalIncome += quotaRaw / conversionFactor
					continue
				}
				if content, ok := item["content"].(string); ok {
					totalIncome += parseIncomeFromContent(content)
				}
			}

			total := extractLogTotal(payload)
			if len(items) == 0 {
				break
			}
			if total != nil && page*pageSize >= *total {
				break
			}
		}
	}

	if !hasAnyResponse {
		return 0, fmt.Errorf("no log responses")
	}
	return math.Round(totalIncome*1_000_000) / 1_000_000, nil
}

func decodeIncomeLogPayload(body io.Reader, payload *map[string]any) error {
	limited := &io.LimitedReader{R: body, N: incomeLogResponseBodyLimit + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(data)) > incomeLogResponseBodyLimit {
		return fmt.Errorf("income log response exceeds %d bytes", incomeLogResponseBodyLimit)
	}
	return json.Unmarshal(data, payload)
}

func extractLogItems(payload map[string]any) []map[string]any {
	if data, ok := payload["data"].(map[string]any); ok {
		if items, ok := data["items"].([]any); ok {
			result := make([]map[string]any, 0, len(items))
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					result = append(result, m)
				}
			}
			return result
		}
	}
	if items, ok := payload["items"].([]any); ok {
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}
	if data, ok := payload["data"].([]any); ok {
		result := make([]map[string]any, 0, len(data))
		for _, item := range data {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}
	return nil
}

func extractLogTotal(payload map[string]any) *int {
	candidates := []any{}
	if data, ok := payload["data"].(map[string]any); ok {
		candidates = append(candidates, data["total"])
	}
	candidates = append(candidates, payload["total"])

	for _, v := range candidates {
		switch val := v.(type) {
		case float64:
			if val >= 0 {
				t := int(val)
				return &t
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n >= 0 {
				return &n
			}
		}
	}
	return nil
}

func parsePositiveNumberAny(v any) float64 {
	switch val := v.(type) {
	case float64:
		if val > 0 && !math.IsNaN(val) && !math.IsInf(val, 0) {
			return val
		}
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err == nil && n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) {
			return n
		}
	}
	return 0
}

func parseIncomeFromContent(content string) float64 {
	// Extract the first number from content text.
	// Matches TS behavior: scans for digit patterns in log content.
	normalized := strings.ReplaceAll(content, ",", "")
	re := regexp.MustCompile(`\d+(?:\.\d+)?`)
	match := re.FindString(normalized)
	if match == "" {
		return 0
	}
	val, err := strconv.ParseFloat(match, 64)
	if err != nil || val <= 0 || math.IsNaN(val) || math.IsInf(val, 0) {
		return 0
	}
	return val
}

// tryAutoReloginBalance attempts re-login for balance refresh.
func tryAutoReloginBalance(cfg *config.Config, db *sqlx.DB, account *store.Account, site *store.Site) (string, error) {
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

	proxyConfig := service.BuildPlatformProxyConfig(cfg, account, site)

	result, err := adp.Login(site.URL, relogin.Username, password, proxyConfig)
	if err != nil || !result.Success || result.AccessToken == "" {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("login failed: %s", result.Message)
	}

	newStatus := account.Status
	if account.Status == "expired" {
		newStatus = "active"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		db.Rebind("UPDATE accounts SET access_token = ?, status = ?, updated_at = ? WHERE id = ?"),
		result.AccessToken, newStatus, now, account.ID,
	)
	if err != nil {
		return "", err
	}
	return result.AccessToken, nil
}

// BalanceResult is the result of a balance refresh.
type BalanceResult struct {
	Balance     float64
	Used        float64
	Quota       float64
	Skipped     bool
	Reason      string
	BalanceInfo *adapter.BalanceInfo
}

// RefreshBalance refreshes the balance for a single account.
// Mirrors TS refreshBalance().
func RefreshBalance(cfg *config.Config, db *sqlx.DB, accountID int64) (*BalanceResult, error) {
	aws, err := service.GetAccountWithSiteByID(db, accountID)
	if err != nil {
		return nil, nil
	}
	account := &aws.Account
	site := &aws.Site

	// Disabled checks
	if isAccountDisabled(account.Status) {
		service.SetAccountRuntimeHealth(db, account.ID, service.RuntimeHealthEntry{
			State: service.HealthDisabled, Reason: "账号已禁用", Source: service.HealthSourceBalance,
		})
		return &BalanceResult{
			Balance: account.Balance,
			Used:    account.BalanceUsed,
			Quota:   account.Quota,
			Skipped: true,
			Reason:  "account_disabled",
		}, nil
	}

	if IsSiteDisabled(site.Status) {
		service.SetAccountRuntimeHealth(db, account.ID, service.RuntimeHealthEntry{
			State: service.HealthDisabled, Reason: "站点已禁用", Source: service.HealthSourceBalance,
		})
		return &BalanceResult{
			Balance: account.Balance,
			Used:    account.BalanceUsed,
			Quota:   account.Quota,
			Skipped: true,
			Reason:  "site_disabled",
		}, nil
	}

	// Get adapter
	adp := adapter.GetAdapter(site.Platform)
	if adp == nil {
		return nil, fmt.Errorf("unsupported platform: %s", site.Platform)
	}

	// API key connection check — skip balance refresh for apikey accounts
	if service.IsAPIKeyConnection(account) {
		return &BalanceResult{
			Balance: account.Balance,
			Used:    account.BalanceUsed,
			Quota:   account.Quota,
			Skipped: true,
			Reason:  "proxy_only",
		}, nil
	}

	platformUserID := service.ResolvePlatformUserID(account.ExtraConfig, account.Username)
	proxyConfig := service.BuildPlatformProxyConfig(cfg, account, site)
	activeAccessToken := account.AccessToken
	activeExtraConfig := account.ExtraConfig

	// Pre-refresh: Sub2Api managed session
	if service.IsSub2ApiPlatform(site.Platform) {
		managedAuth := service.GetSub2ApiAuthFromExtraConfig(activeExtraConfig)
		if managedAuth != nil && managedAuth["refreshToken"] != nil {
			if service.IsManagedSub2ApiTokenDue(managedAuth["tokenExpiresAt"]) {
				refreshedToken, refErr := refreshSub2ApiManagedSessionSingleflight(cfg, db, account.ID, account, site, managedAuth)
				if refErr == nil && refreshedToken != "" {
					activeAccessToken = refreshedToken
					slog.Info("Sub2Api token refreshed via singleflight", "accountID", accountID)
				} else {
					slog.Warn("Sub2Api token refresh failed", "accountID", accountID, "error", refErr)
				}
			}
		}
	}

	// Read balance helper
	readBalance := func(token string) (*adapter.BalanceInfo, error) {
		return adp.GetBalance(site.URL, token, platformUserID, proxyConfig)
	}

	// Error handler
	handleBalanceError := func(err error) error {
		message := alert.AppendSessionTokenRebindHint(err.Error())
		service.SetAccountRuntimeHealth(db, account.ID, service.RuntimeHealthEntry{
			State: service.HealthUnhealthy, Reason: message, Source: service.HealthSourceBalance,
		})
		if alert.ShouldMarkAccountExpired(0, message) {
			alert.ReportTokenExpired(cfg, db, alert.TokenExpiredParams{
				AccountID: account.ID, Username: account.Username,
				SiteName: &site.Name, Detail: message,
			})
		}
		return fmt.Errorf("%s", message)
	}

	// First attempt
	balanceInfo, err := readBalance(activeAccessToken)
	if err != nil {
		errMsg := err.Error()

		// Sub2Api managed refresh retry
		isSub2Api := service.IsSub2ApiPlatform(site.Platform)
		managedAuth := service.GetSub2ApiAuthFromExtraConfig(activeExtraConfig)
		if isSub2Api && managedAuth != nil && managedAuth["refreshToken"] != nil && shouldAttemptAutoReloginBalance(errMsg) {
			refreshedToken, refErr := refreshSub2ApiManagedSessionSingleflight(cfg, db, account.ID, account, site, managedAuth)
			if refErr == nil && refreshedToken != "" {
				activeAccessToken = refreshedToken
				balanceInfo, err = readBalance(activeAccessToken)
				if err != nil {
					return nil, handleBalanceError(err)
				}
				// Successfully retried with refreshed token — skip auto-relogin below
				goto afterRetry
			}
		}

		if shouldAttemptAutoReloginBalance(errMsg) {
			refreshedToken, reloginErr := tryAutoReloginBalance(cfg, db, account, site)
			if reloginErr == nil && refreshedToken != "" {
				activeAccessToken = refreshedToken
				balanceInfo, err = readBalance(activeAccessToken)
				if err != nil {
					return nil, handleBalanceError(err)
				}
			} else {
				return nil, handleBalanceError(err)
			}
		} else {
			return nil, handleBalanceError(err)
		}
	}

afterRetry:
	if balanceInfo == nil {
		return nil, fmt.Errorf("failed to fetch balance")
	}

	// Today income fallback from logs
	if balanceInfo.TodayIncome == nil || math.IsNaN(*balanceInfo.TodayIncome) || math.IsInf(*balanceInfo.TodayIncome, 0) {
		if supportsTodayIncomeLogFallback(site.Platform) {
			fallbackIncome, fallbackErr := fetchTodayIncomeFromLogs(site.URL, activeAccessToken, site.Platform, platformUserID, proxyConfig)
			if fallbackErr == nil {
				balanceInfo.TodayIncome = &fallbackIncome
			}
		}
	}

	// Update todayIncome snapshot
	nextExtraConfig := activeExtraConfig
	if balanceInfo.TodayIncome != nil && !math.IsNaN(*balanceInfo.TodayIncome) && !math.IsInf(*balanceInfo.TodayIncome, 0) {
		nextExtraConfig = service.UpdateTodayIncomeSnapshot(nextExtraConfig, *balanceInfo.TodayIncome, time.Now())
	}
	if balanceInfo.SubscriptionSummary != nil && service.IsSub2ApiPlatform(site.Platform) {
		subSummary := service.BuildStoredSub2ApiSubscriptionSummary(balanceInfo.SubscriptionSummary)
		nextExtraConfig = service.MergeExtraConfig(nextExtraConfig, map[string]any{
			"sub2apiSubscription": subSummary,
		})
	}

	// Check if unsupported checkin degraded state should be preserved
	existingHealth := service.ExtractRuntimeHealth(nextExtraConfig)
	keepUnsupportedCheckinDegraded := service.IsUnsupportedCheckinRuntimeHealth(existingHealth)

	// Prepare updates
	updates := map[string]any{
		"balance":     balanceInfo.Balance,
		"balanceUsed": balanceInfo.Used,
		"quota":       balanceInfo.Quota,
		"status": func() string {
			if account.Status == "expired" {
				return "active"
			}
			return account.Status
		}(),
		"lastBalanceRefresh": time.Now().UTC().Format(time.RFC3339),
		"updatedAt":          time.Now().UTC().Format(time.RFC3339),
	}

	if nextExtraConfig != nil && account.ExtraConfig != nil && *nextExtraConfig != *account.ExtraConfig {
		updates["extraConfig"] = *nextExtraConfig
	}
	if nextExtraConfig != nil && account.ExtraConfig == nil {
		updates["extraConfig"] = *nextExtraConfig
	}

	// Update DB
	err = service.UpdateAccountFields(db, accountID, updates)
	if err != nil {
		return nil, err
	}

	// Set runtime health
	if keepUnsupportedCheckinDegraded {
		reason := "站点不支持签到接口"
		source := service.HealthSourceCheckin
		if existingHealth != nil && existingHealth.Reason != "" {
			reason = existingHealth.Reason
		}
		if existingHealth != nil && existingHealth.Source != "" {
			source = existingHealth.Source
		}
		service.SetAccountRuntimeHealth(db, account.ID, service.RuntimeHealthEntry{
			State: service.HealthDegraded, Reason: reason, Source: source,
		})
	} else {
		service.SetAccountRuntimeHealth(db, account.ID, service.RuntimeHealthEntry{
			State: service.HealthHealthy, Reason: "余额刷新成功", Source: service.HealthSourceBalance,
		})
	}

	return &BalanceResult{
		Balance:     balanceInfo.Balance,
		Used:        balanceInfo.Used,
		Quota:       balanceInfo.Quota,
		Skipped:     false,
		BalanceInfo: balanceInfo,
	}, nil
}

// RefreshAllBalances refreshes balances for all active accounts.
// Mirrors TS refreshAllBalances().
func RefreshAllBalances(cfg *config.Config, db *sqlx.DB) []struct {
	AccountID int64
	Balance   *float64
} {
	var accounts []store.Account
	if err := db.Select(&accounts, "SELECT * FROM accounts WHERE status = 'active'"); err != nil {
		slog.Error("RefreshAllBalances: failed to query accounts", "error", err)
		return nil
	}

	type resultEntry struct {
		AccountID int64
		Balance   *float64
	}

	results := make([]resultEntry, len(accounts))
	var wg sync.WaitGroup

	for i, account := range accounts {
		wg.Add(1)
		go func(idx int, acc store.Account) {
			defer wg.Done()
			balanceResult, err := RefreshBalance(cfg, db, acc.ID)
			if err != nil || balanceResult == nil {
				results[idx] = resultEntry{AccountID: acc.ID, Balance: nil}
			} else {
				b := balanceResult.Balance
				results[idx] = resultEntry{AccountID: acc.ID, Balance: &b}
			}
		}(i, account)
	}
	wg.Wait()

	// Convert to export type
	out := make([]struct {
		AccountID int64
		Balance   *float64
	}, len(results))
	for i, r := range results {
		out[i].AccountID = r.AccountID
		out[i].Balance = r.Balance
	}
	return out
}
