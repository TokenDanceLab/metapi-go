package platform

import (
	"context"
	"strings"
)

// CodexAdapter handles chatgpt.com/backend-api/codex (OAuth-driven, all methods return empty/unsupported).
type CodexAdapter struct {
	*BaseAdapter
}

func init() {
	Register(&CodexAdapter{BaseAdapter: NewBaseAdapter("codex")})
}

// Detect matches URL keyword: chatgpt.com/backend-api/codex.
func (c *CodexAdapter) Detect(ctx context.Context, url string) (bool, error) {
	normalized := strings.TrimRight(strings.ToLower(strings.TrimSpace(url)), "/")
	return strings.Contains(normalized, "chatgpt.com/backend-api/codex"), nil
}

// Login: OAuth only, not supported.
func (c *CodexAdapter) Login(ctx context.Context, url, username, password string, platformUserId *int, proxy *ProxyConfig) (*LoginResult, error) {
	return &LoginResult{Success: false, Message: "codex oauth login is managed via OAuth flow"}, nil
}

// GetUserInfo returns nil (OAuth-managed).
func (c *CodexAdapter) GetUserInfo(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*UserInfo, error) {
	return nil, nil
}

// Checkin: not supported.
func (c *CodexAdapter) Checkin(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error) {
	return &CheckinResult{Success: false, Message: "codex oauth connections do not support checkin"}, nil
}

// GetBalance returns zero balance.
func (c *CodexAdapter) GetBalance(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error) {
	return &BalanceInfo{Balance: 0, Used: 0, Quota: 0}, nil
}

// GetModels returns empty list.
func (c *CodexAdapter) GetModels(ctx context.Context, url, token string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	return []string{}, nil
}
