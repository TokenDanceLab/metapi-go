package platform

import (
	"context"
	"fmt"
	"strings"
)

// VeloeraAdapter handles Veloera platforms (1M divisor + Veloera-User header).
// Directly extends BaseAdapter, NOT NewApiAdapter.
type VeloeraAdapter struct {
	*BaseAdapter
}

func init() {
	Register(&VeloeraAdapter{BaseAdapter: NewBaseAdapter("veloera")})
}

// Detect probes GET /api/status and checks for "veloera" in system_name or version.
func (v *VeloeraAdapter) Detect(ctx context.Context, url string) (bool, error) {
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

	systemName, _ := getString(data, "system_name")
	systemName = strings.ToLower(systemName)
	version, _ := getString(data, "version")
	version = strings.ToLower(version)

	return strings.Contains(systemName, "veloera") || strings.Contains(version, "veloera"), nil
}

// veloeraHeaders sets Authorization + Veloera-User + New-API-User + User-id.
func veloeraHeaders(accessToken string, userID *int) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
	}
	if userID != nil {
		val := fmt.Sprintf("%d", *userID)
		headers["Veloera-User"] = val
		headers["New-API-User"] = val
		headers["User-id"] = val
	}
	return headers
}

// Checkin: POST /api/user/checkin (veloeraHeaders, requires platformUserId).
func (v *VeloeraAdapter) Checkin(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error) {
	headers := veloeraHeaders(accessToken, platformUserId)
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

// GetBalance: quota=total, balance=quota-used, divisor=1,000,000 (NOT 500,000!).
func (v *VeloeraAdapter) GetBalance(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error) {
	headers := veloeraHeaders(accessToken, platformUserId)
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
	balance := (quota - used) / 1000000
	quotaUSD := quota / 1000000
	usedUSD := used / 1000000

	var todayIncome *float64
	if v, ok := getFloat(data, "today_income"); ok {
		ti := v / 1000000
		todayIncome = &ti
	}
	var todayQuotaConsumption *float64
	if v, ok := getFloat(data, "today_quota_consumption"); ok {
		tq := v / 1000000
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

// GetModels: GET /v1/models (Bearer auth only, no Veloera headers).
func (v *VeloeraAdapter) GetModels(ctx context.Context, baseURL string, apiToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
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
