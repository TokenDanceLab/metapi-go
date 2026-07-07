// Package adapter defines the platform adapter interface and registry.
// P4 will register real adapters via RegisterAdapter. P5 services
// call GetAdapter to perform checkin/login/getBalance operations.
package adapter

import (
	"context"

	"github.com/tokendancelab/metapi-go/platform"
)

// CheckinResult represents the result of a checkin operation.
type CheckinResult struct {
	Success bool
	Message string
	Reward  string // may be a number or string; parsed later by reward parser
}

// LoginResult represents the result of a login/auto-relogin operation.
type LoginResult struct {
	Success     bool
	AccessToken string
	Message     string
}

// BalanceInfo represents account balance information.
type BalanceInfo struct {
	Balance             float64
	Used                float64
	Quota               float64
	TodayIncome         *float64
	SubscriptionSummary *Sub2ApiSubscriptionSummary
}

// Sub2ApiSubscriptionSummary is a summary of a Sub2Api subscription.
type Sub2ApiSubscriptionSummary struct {
	PlanName       string  `json:"planName"`
	ExpiresAt      string  `json:"expiresAt"`
	TotalQuota     float64 `json:"totalQuota"`
	UsedQuota      float64 `json:"usedQuota"`
	RemainingQuota float64 `json:"remainingQuota"`
	DaysRemaining  int64   `json:"daysRemaining"`
}

// Adapter is the interface that platform adapters must implement.
type Adapter interface {
	// Checkin performs a daily checkin on the platform.
	Checkin(baseURL, accessToken string, platformUserID int64, proxyConfig *platform.ProxyConfig) (*CheckinResult, error)
	// Login performs a login on the platform, used for auto-relogin.
	Login(baseURL, username, password string, proxyConfig *platform.ProxyConfig) (*LoginResult, error)
	// GetBalance retrieves account balance information.
	GetBalance(baseURL, accessToken string, platformUserID int64, proxyConfig *platform.ProxyConfig) (*BalanceInfo, error)
}

var registry = map[string]Adapter{}

// RegisterAdapter registers a platform adapter. Call this during init() in P4 adapter packages.
func RegisterAdapter(platform string, a Adapter) {
	registry[platform] = a
}

// GetAdapter returns the adapter for the given platform, or nil if not found.
func GetAdapter(platform string) Adapter {
	if a := registry[platform]; a != nil {
		return a
	}
	if a := platformadapter(platform); a != nil {
		return a
	}
	return nil
}

type platformBridge struct {
	adapter platform.PlatformAdapter
}

func platformadapter(name string) Adapter {
	a := platform.GetAdapter(name)
	if a == nil {
		return nil
	}
	return &platformBridge{adapter: a}
}

func (p *platformBridge) Checkin(baseURL, accessToken string, platformUserID int64, proxyConfig *platform.ProxyConfig) (*CheckinResult, error) {
	result, err := p.adapter.Checkin(context.Background(), baseURL, accessToken, platformUserIDPtr(platformUserID), proxyConfig)
	if result == nil {
		return nil, err
	}
	return &CheckinResult{
		Success: result.Success,
		Message: result.Message,
		Reward:  result.Reward,
	}, err
}

func (p *platformBridge) Login(baseURL, username, password string, proxyConfig *platform.ProxyConfig) (*LoginResult, error) {
	result, err := p.adapter.Login(context.Background(), baseURL, username, password, nil, proxyConfig)
	if result == nil {
		return nil, err
	}
	return &LoginResult{
		Success:     result.Success,
		AccessToken: result.AccessToken,
		Message:     result.Message,
	}, err
}

func (p *platformBridge) GetBalance(baseURL, accessToken string, platformUserID int64, proxyConfig *platform.ProxyConfig) (*BalanceInfo, error) {
	result, err := p.adapter.GetBalance(context.Background(), baseURL, accessToken, platformUserIDPtr(platformUserID), proxyConfig)
	if result == nil {
		return nil, err
	}
	return &BalanceInfo{
		Balance:     result.Balance,
		Used:        result.Used,
		Quota:       result.Quota,
		TodayIncome: result.TodayIncome,
	}, err
}

func platformUserIDPtr(id int64) *int {
	if id <= 0 {
		return nil
	}
	value := int(id)
	return &value
}
