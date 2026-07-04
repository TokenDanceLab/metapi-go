package platform

import (
	"context"
	"encoding/json"
	"strings"
)

// DoneHubAdapter extends OneHubAdapter with checkin unsupported, remaining-quota balance, and /api/notice.
type DoneHubAdapter struct {
	*OneHubAdapter
}

func init() {
	Register(&DoneHubAdapter{
		OneHubAdapter: &OneHubAdapter{
			OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("done-hub")},
		},
	})
}

// Detect: URL keyword match "donehub" or "done-hub" (title-first platform).
func (d *DoneHubAdapter) Detect(ctx context.Context, url string) (bool, error) {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "donehub") || strings.Contains(lower, "done-hub"), nil
}

// Checkin: always unsupported for DoneHub.
func (d *DoneHubAdapter) Checkin(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error) {
	return &CheckinResult{Success: false, Message: "checkin endpoint not found"}, nil
}

// GetBalance: quota=remaining, total=quota+used, divisor=500000.
func (d *DoneHubAdapter) GetBalance(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error) {
	headers := authBearerHeaders(accessToken)
	resp, err := fetchJSON(ctx, baseURL+"/api/user/self", "GET", nil, headers, proxy)
	if err != nil {
		return &BalanceInfo{}, nil
	}

	data, ok := getMap(resp, "data")
	if !ok {
		data = resp
	}

	quotaRemaining, _ := getFloat(data, "quota")
	used, _ := getFloat(data, "used_quota")

	quotaUSD := quotaRemaining / 500000
	usedUSD := used / 500000
	totalUSD := quotaUSD + usedUSD

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
		Balance:              quotaUSD,
		Used:                 usedUSD,
		Quota:                totalUSD,
		TodayIncome:          todayIncome,
		TodayQuotaConsumption: todayQuotaConsumption,
	}, nil
}

// GetSiteAnnouncements: GET /api/notice.
func (d *DoneHubAdapter) GetSiteAnnouncements(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]SiteAnnouncement, error) {
	resp, err := fetchJSON(ctx, baseURL+"/api/notice", "GET", nil, nil, proxy)
	if err != nil {
		return []SiteAnnouncement{}, nil
	}

	content := ""
	if dataStr, ok := getString(resp, "data"); ok {
		content = strings.TrimSpace(dataStr)
	}
	if content == "" {
		// Try payload as string directly
		if raw, err := json.Marshal(resp); err == nil {
			content = strings.TrimSpace(string(raw))
		}
	}
	if content == "" {
		return []SiteAnnouncement{}, nil
	}

	return []SiteAnnouncement{{
		SourceKey:  buildNoticeSourceKey(content),
		Title:      "Site notice",
		Content:    content,
		Level:      "info",
		SourceURL:  "/api/notice",
		RawPayload: nil,
	}}, nil
}
