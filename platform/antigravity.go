package platform

import (
	"context"
	"strings"
)

// AntigravityAdapter handles Google Antigravity platforms (OAuth-driven).
type AntigravityAdapter struct {
	*BaseAdapter
}

func init() {
	Register(&AntigravityAdapter{BaseAdapter: NewBaseAdapter("antigravity")})
}

// Detect matches URL keyword: antigravity.
func (a *AntigravityAdapter) Detect(ctx context.Context, url string) (bool, error) {
	return strings.Contains(strings.ToLower(url), "antigravity"), nil
}

// Login: not supported.
func (a *AntigravityAdapter) Login(ctx context.Context, url, username, password string, platformUserId *int, proxy *ProxyConfig) (*LoginResult, error) {
	return &LoginResult{Success: false, Message: "login endpoint not supported"}, nil
}

// GetUserInfo returns nil.
func (a *AntigravityAdapter) GetUserInfo(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*UserInfo, error) {
	return nil, nil
}

// Checkin: not supported.
func (a *AntigravityAdapter) Checkin(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error) {
	return &CheckinResult{Success: false, Message: "checkin endpoint not supported"}, nil
}

// GetBalance returns zero balance.
func (a *AntigravityAdapter) GetBalance(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error) {
	return &BalanceInfo{Balance: 0, Used: 0, Quota: 0}, nil
}

// GetModels fetches from /v1internal:fetchAvailableModels with Antigravity-specific headers.
func (a *AntigravityAdapter) GetModels(ctx context.Context, baseURL string, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")

	headers := map[string]string{
		"Authorization":   "Bearer " + accessToken,
		"Accept":          "application/json",
		"User-Agent":      "Antigravity/1.0",
		"X-Goog-Api-Client": "antigravity-client",
		"Client-Metadata": "antigravity",
	}

	resp, err := fetchJSON(ctx, normalized+"/v1internal:fetchAvailableModels", "POST", map[string]interface{}{}, headers, proxy)
	if err != nil {
		return []string{}, nil
	}

	return extractAntigravityModelNames(resp), nil
}

func extractAntigravityModelNames(payload map[string]interface{}) []string {
	modelsObj, ok := payload["models"]
	if !ok {
		return []string{}
	}

	// Object form: {"models": {"model-name": {...}, ...}}
	if m, ok := modelsObj.(map[string]interface{}); ok {
		names := make([]string, 0, len(m))
		for name := range m {
			if t := strings.TrimSpace(name); t != "" {
				names = append(names, t)
			}
		}
		return names
	}

	// Array form: {"models": [{"id": "...", "name": "..."}, ...]}
	if arr, ok := modelsObj.([]interface{}); ok {
		names := make([]string, 0, len(arr))
		for _, item := range arr {
			switch v := item.(type) {
			case string:
				if t := strings.TrimSpace(v); t != "" {
					names = append(names, t)
				}
			case map[string]interface{}:
				if id, ok := v["id"].(string); ok && strings.TrimSpace(id) != "" {
					names = append(names, strings.TrimSpace(id))
				} else if name, ok := v["name"].(string); ok && strings.TrimSpace(name) != "" {
					names = append(names, strings.TrimSpace(name))
				}
			}
		}
		return names
	}

	return []string{}
}
