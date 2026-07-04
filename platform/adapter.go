// Package platform implements detection and operation adapters for 14 upstream API platforms.
//
// Inheritance chain (via Go struct embedding):
//
//	BaseAdapter (base.go)
//	├── StandardAdapter (standard.go)
//	│   ├── OpenAiAdapter       (openai.go)
//	│   ├── ClaudeAdapter       (claude.go)
//	│   ├── GeminiAdapter       (gemini.go)
//	│   │   └── GeminiCliAdapter (gemini_cli.go)
//	│   └── CliProxyApiAdapter  (cliproxyapi.go)
//	├── CodexAdapter            (codex.go)
//	├── AntigravityAdapter      (antigravity.go)
//	├── OneApiAdapter           (oneapi.go)
//	│   └── OneHubAdapter       (onehub.go)
//	│       └── DoneHubAdapter  (donehub.go)
//	├── VeloeraAdapter          (veloera.go)
//	├── Sub2ApiAdapter          (sub2api.go)
//	└── NewApiAdapter           (newapi.go)
//	    └── AnyRouterAdapter    (anyrouter.go)
package platform

import (
	"context"
	"encoding/json"
)

// CredentialMode indicates the credential type preference.
type CredentialMode int

const (
	CredentialAuto    CredentialMode = iota // auto-detect (session first, then apikey)
	CredentialSession                       // only session token
	CredentialAPIKey                        // only API key
)

// ProxyConfig carries optional proxy settings for a single request.
type ProxyConfig struct {
	ProxyURL        string
	CustomHeaders   map[string]string
	UseSystemProxy  bool
	InsecureSkipTLS bool
}

// CheckinResult is the outcome of a daily checkin.
type CheckinResult struct {
	Success bool
	Message string
	Reward  string // optional
}

// SubscriptionPlanSummary describes a single subscription plan.
type SubscriptionPlanSummary struct {
	ID              *int     `json:"id,omitempty"`
	GroupID         *int     `json:"groupId,omitempty"`
	GroupName       string   `json:"groupName,omitempty"`
	Status          string   `json:"status,omitempty"`
	ExpiresAt       string   `json:"expiresAt,omitempty"`
	DailyUsedUsd    *float64 `json:"dailyUsedUsd,omitempty"`
	DailyLimitUsd   *float64 `json:"dailyLimitUsd,omitempty"`
	WeeklyUsedUsd   *float64 `json:"weeklyUsedUsd,omitempty"`
	WeeklyLimitUsd  *float64 `json:"weeklyLimitUsd,omitempty"`
	MonthlyUsedUsd  *float64 `json:"monthlyUsedUsd,omitempty"`
	MonthlyLimitUsd *float64 `json:"monthlyLimitUsd,omitempty"`
}

// SubscriptionSummary aggregates subscription data.
type SubscriptionSummary struct {
	ActiveCount   int
	TotalUsedUsd  float64
	Subscriptions []SubscriptionPlanSummary
}

// BalanceInfo holds balance and usage information in USD.
type BalanceInfo struct {
	Balance               float64              `json:"balance"`
	Used                  float64              `json:"used"`
	Quota                 float64              `json:"quota"`
	TodayIncome           *float64             `json:"todayIncome,omitempty"`
	TodayQuotaConsumption *float64             `json:"todayQuotaConsumption,omitempty"`
	SubscriptionSummary   *SubscriptionSummary `json:"subscriptionSummary,omitempty"`
}

// LoginResult is the outcome of a login attempt.
type LoginResult struct {
	Success     bool
	AccessToken string
	Username    string
	Message     string
}

// UserInfo holds the authenticated user's profile.
type UserInfo struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	Role        *int   `json:"role,omitempty"`
}

// TokenVerifyResult reports the result of token verification.
type TokenVerifyResult struct {
	TokenType string     // "session", "apikey", "unknown"
	UserInfo  *UserInfo
	Balance   *BalanceInfo
	APIToken  string // first discovered API key (optional)
	Models    []string
}

// ApiTokenInfo describes a single API key/token.
type ApiTokenInfo struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	Enabled    bool   `json:"enabled"`
	TokenGroup string `json:"tokenGroup,omitempty"`
}

// SiteAnnouncement is a notice or announcement from the upstream platform.
type SiteAnnouncement struct {
	SourceKey         string          `json:"sourceKey"`
	Title             string          `json:"title"`
	Content           string          `json:"content"`
	Level             string          `json:"level"` // "info", "warning", "error"
	SourceURL         string          `json:"sourceUrl,omitempty"`
	StartsAt          string          `json:"startsAt,omitempty"`
	EndsAt            string          `json:"endsAt,omitempty"`
	UpstreamCreatedAt string          `json:"upstreamCreatedAt,omitempty"`
	UpstreamUpdatedAt string          `json:"upstreamUpdatedAt,omitempty"`
	RawPayload        json.RawMessage `json:"rawPayload,omitempty"`
}

// CreateAPITokenOptions holds parameters for creating a new API key.
type CreateAPITokenOptions struct {
	Name              string
	Group             string
	UnlimitedQuota    bool    // default true
	RemainQuota       float64 // default 0
	ExpiredTime       int64   // Unix timestamp, -1 = never expire
	AllowIPs          string
	ModelLimitsEnabled bool
	ModelLimits       string
}

// PlatformAdapter is the interface every platform adapter must implement.
type PlatformAdapter interface {
	PlatformName() string

	// Detect returns true if the given URL can be handled by this adapter.
	Detect(ctx context.Context, url string) (bool, error)

	// Session management.
	// platformUserId is for NewApi-fork platforms' cookie-based auth fallback.
	Login(ctx context.Context, url, username, password string, platformUserId *int, proxy *ProxyConfig) (*LoginResult, error)
	GetUserInfo(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*UserInfo, error)
	VerifyToken(ctx context.Context, url, token string, platformUserId *int, proxy *ProxyConfig) (*TokenVerifyResult, error)

	// Daily operations.
	Checkin(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error)
	GetBalance(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error)

	// Model discovery: returns model ID strings.
	GetModels(ctx context.Context, url, token string, platformUserId *int, proxy *ProxyConfig) ([]string, error)

	// Token management (NewAPI-style platforms).
	GetAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*string, error)
	GetAPITokens(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]ApiTokenInfo, error)
	CreateAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, options *CreateAPITokenOptions, proxy *ProxyConfig) (bool, error)
	DeleteAPIToken(ctx context.Context, url, accessToken string, tokenKey string, platformUserId *int, proxy *ProxyConfig) error

	// Announcements and groups.
	GetSiteAnnouncements(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]SiteAnnouncement, error)
	GetUserGroups(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error)
}
