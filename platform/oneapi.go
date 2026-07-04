package platform

import (
	"context"
	"fmt"
	"strings"
)

// OneApiAdapter handles OneAPI platforms (detect: /api/status without system_name).
// Serves as the base for OneHubAdapter and DoneHubAdapter.
type OneApiAdapter struct {
	*BaseAdapter
}

func init() {
	Register(&OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")})
}

// Detect probes GET /api/status and checks that success===true and system_name is absent.
func (o *OneApiAdapter) Detect(ctx context.Context, url string) (bool, error) {
	resp, err := fetchJSON(ctx, url+"/api/status", "GET", nil, nil, nil)
	if err != nil {
		return false, nil
	}
	success, _ := getBool(resp, "success")
	if !success {
		return false, nil
	}
	data, ok := getMap(resp, "data")
	if !ok {
		return false, nil
	}
	_, hasSystemName := data["system_name"]
	return !hasSystemName, nil
}

// Checkin: POST /api/user/checkin (Bearer auth).
func (o *OneApiAdapter) Checkin(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error) {
	headers := authBearerHeaders(accessToken)
	resp, err := fetchJSON(ctx, baseURL+"/api/user/checkin", "POST", nil, headers, proxy)
	if err != nil {
		return &CheckinResult{Success: false, Message: err.Error()}, nil
	}

	success, _ := getBool(resp, "success")
	if success {
		msg, _ := getString(resp, "message")
		if msg == "" {
			msg = "Check-in successful"
		}
		reward := ""
		if data, ok := getMap(resp, "data"); ok {
			if r, ok := data["reward"]; ok {
				reward = fmt.Sprintf("%v", r)
			}
		}
		return &CheckinResult{Success: true, Message: msg, Reward: reward}, nil
	}

	msg, _ := getString(resp, "message")
	if msg == "" {
		msg = "Check-in failed"
	}
	return &CheckinResult{Success: false, Message: msg}, nil
}

// GetBalance: quota=total, balance=quota-used, divisor=500000.
func (o *OneApiAdapter) GetBalance(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error) {
	headers := authBearerHeaders(accessToken)
	resp, err := fetchJSON(ctx, baseURL+"/api/user/self", "GET", nil, headers, proxy)
	if err != nil {
		return &BalanceInfo{}, nil
	}

	data, ok := getMap(resp, "data")
	if !ok {
		data = resp
	}

	quota, _ := getFloat(data, "quota")
	used, _ := getFloat(data, "used_quota")
	balance := (quota - used) / 500000
	quotaUSD := quota / 500000
	usedUSD := used / 500000

	var todayIncome *float64
	if v, ok := getFloat(data, "today_income"); ok {
		ti := v / 500000
		todayIncome = &ti
	}
	var todayQuotaConsumption *float64
	if v, ok := getFloat(data, "today_quota_consumption"); ok {
		tq := v / 500000
		todayQuotaConsumption = &tq
	}

	return &BalanceInfo{
		Balance:              balance,
		Used:                 usedUSD,
		Quota:                quotaUSD,
		TodayIncome:          todayIncome,
		TodayQuotaConsumption: todayQuotaConsumption,
	}, nil
}

// GetModels: GET /v1/models (Bearer auth).
func (o *OneApiAdapter) GetModels(ctx context.Context, baseURL string, apiToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	headers := authBearerHeaders(apiToken)
	resp, err := fetchJSON(ctx, baseURL+"/v1/models", "GET", nil, headers, proxy)
	if err != nil {
		return []string{}, nil
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		return []string{}, nil
	}

	models := make([]string, 0, len(data))
	for _, item := range data {
		if m, ok := item.(map[string]interface{}); ok {
			if id, ok := m["id"].(string); ok && strings.TrimSpace(id) != "" {
				models = append(models, strings.TrimSpace(id))
			}
		}
	}
	return models, nil
}

// GetAPITokens: GET /api/token/?p=0&size=100 (Bearer auth).
func (o *OneApiAdapter) GetAPITokens(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]ApiTokenInfo, error) {
	headers := authBearerHeaders(accessToken)
	resp, err := fetchJSON(ctx, baseURL+"/api/token/?p=0&size=100", "GET", nil, headers, proxy)
	if err != nil {
		return []ApiTokenInfo{}, nil
	}

	items := parseTokenItemsFromMap(resp)
	if len(items) == 0 {
		return []ApiTokenInfo{}, nil
	}
	return normalizeTokenItems(items), nil
}

// GetAPIToken returns the first enabled token.
func (o *OneApiAdapter) GetAPIToken(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*string, error) {
	tokens, err := o.GetAPITokens(ctx, baseURL, accessToken, platformUserId, proxy)
	if err != nil {
		return nil, nil
	}
	return findFirstEnabledToken(tokens), nil
}

// GetUserGroups: GET /api/user_group_map, fallback /api/user/self/groups.
func (o *OneApiAdapter) GetUserGroups(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	headers := authBearerHeaders(accessToken)
	var terminalError string

	// Try /api/user_group_map first (OneApi order)
	groups, err := o.tryGetGroups(ctx, baseURL+"/api/user_group_map", headers, proxy)
	if err != nil {
		terminalError = err.Error()
	}
	if len(groups) > 0 {
		return dedupeStrings(groups), nil
	}

	// Fallback: /api/user/self/groups
	groups, err = o.tryGetGroups(ctx, baseURL+"/api/user/self/groups", headers, proxy)
	if err != nil {
		if terminalError == "" {
			terminalError = err.Error()
		}
	}
	if len(groups) > 0 {
		return dedupeStrings(groups), nil
	}

	if terminalError != "" {
		return nil, fmt.Errorf("%s", terminalError)
	}

	return []string{"default"}, nil
}

func (o *OneApiAdapter) tryGetGroups(ctx context.Context, url string, headers map[string]string, proxy *ProxyConfig) ([]string, error) {
	resp, err := fetchJSON(ctx, url, "GET", nil, headers, proxy)
	if err != nil {
		return nil, err
	}

	if success, _ := getBool(resp, "success"); !success {
		msg := resolveGroupFetchErrorMessage(resp)
		return nil, fmt.Errorf("%s", msg)
	}

	return extractGroupKeys(resp), nil
}

// CreateAPIToken: POST /api/token/ (Bearer auth).
func (o *OneApiAdapter) CreateAPIToken(ctx context.Context, baseURL, accessToken string, platformUserId *int, options *CreateAPITokenOptions, proxy *ProxyConfig) (bool, error) {
	payload := buildDefaultTokenPayload(options)
	headers := authBearerHeaders(accessToken)
	resp, err := fetchJSON(ctx, baseURL+"/api/token/", "POST", payload, headers, proxy)
	if err != nil {
		return false, nil
	}
	success, _ := getBool(resp, "success")
	return success, nil
}

// DeleteAPIToken: list -> find key -> DELETE /api/token/{id} (with trailing-slash fallback).
func (o *OneApiAdapter) DeleteAPIToken(ctx context.Context, baseURL, accessToken, tokenKey string, platformUserId *int, proxy *ProxyConfig) error {
	targetKey := normalizeTokenKeyForCompare(tokenKey)
	if targetKey == "" {
		return nil
	}

	headers := authBearerHeaders(accessToken)

	// List tokens
	resp, err := fetchJSON(ctx, baseURL+"/api/token/?p=0&size=100", "GET", nil, headers, proxy)
	if err != nil {
		return nil
	}

	items := parseTokenItemsFromMap(resp)
	tokenID := pickTokenID(items, targetKey)
	if tokenID == nil {
		return nil // Already absent, safe
	}

	// Try DELETE without trailing slash
	delResp, err := fetchJSON(ctx, fmt.Sprintf("%s/api/token/%d", baseURL, *tokenID), "DELETE", nil, headers, proxy)
	if err == nil {
		if success, _ := getBool(delResp, "success"); success {
			return nil
		}
	}

	// Double-DELETE: trailing slash fallback (OneApi-specific)
	delResp2, err := fetchJSON(ctx, fmt.Sprintf("%s/api/token/%d/", baseURL, *tokenID), "DELETE", nil, headers, proxy)
	if err == nil {
		success, _ := getBool(delResp2, "success")
		if success {
			return nil
		}
		_ = success
	}
	return nil
}

func resolveGroupFetchErrorMessage(payload map[string]interface{}) string {
	msg, _ := getString(payload, "message")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "failed to fetch groups"
	}
	lower := strings.ToLower(msg)
	indicatesExpired := strings.Contains(lower, "expired") ||
		strings.Contains(lower, "invalid token") ||
		strings.Contains(lower, "access token") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "未登录") ||
		strings.Contains(lower, "登录") ||
		strings.Contains(lower, "过期")
	if indicatesExpired {
		return "账号会话可能已过期，请重新登录后再拉取分组"
	}
	return msg
}

func extractGroupKeys(payload map[string]interface{}) []string {
	source := payload
	if data, ok := getMap(payload, "data"); ok {
		source = data
	}

	if source == nil {
		return nil
	}

	excluded := map[string]bool{
		"success": true, "message": true, "code": true, "data": true, "error": true,
	}

	keys := make([]string, 0, len(source))
	for k := range source {
		if !excluded[strings.ToLower(k)] && strings.TrimSpace(k) != "" {
			keys = append(keys, strings.TrimSpace(k))
		}
	}
	return keys
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(items))
	for _, item := range items {
		t := strings.TrimSpace(item)
		if t != "" && !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}

func buildDefaultTokenPayload(options *CreateAPITokenOptions) map[string]interface{} {
	name := "metapi"
	if options != nil && strings.TrimSpace(options.Name) != "" {
		name = strings.TrimSpace(options.Name)
	}

	unlimitedQuota := true
	if options != nil {
		unlimitedQuota = options.UnlimitedQuota
	}

	remainQuota := 0.0
	if options != nil && options.RemainQuota > 0 {
		remainQuota = options.RemainQuota
	}

	expiredTime := int64(-1)
	if options != nil && options.ExpiredTime != 0 {
		expiredTime = options.ExpiredTime
	}

	allowIPs := ""
	modelLimits := ""
	group := ""
	modelLimitsEnabled := false
	if options != nil {
		allowIPs = options.AllowIPs
		modelLimits = options.ModelLimits
		group = options.Group
		modelLimitsEnabled = options.ModelLimitsEnabled
	}

	return map[string]interface{}{
		"name":                name,
		"unlimited_quota":     unlimitedQuota,
		"expired_time":        expiredTime,
		"remain_quota":        remainQuota,
		"allow_ips":           allowIPs,
		"model_limits_enabled": modelLimitsEnabled,
		"model_limits":        modelLimits,
		"group":               group,
	}
}
