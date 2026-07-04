// Package adapter defines the platform adapter interface and registry.
// P4 will register real adapters via RegisterAdapter. P5 services
// call GetAdapter to perform checkin/login/getBalance operations.
package adapter

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
	Balance    float64
	Used       float64
	Quota      float64
	TodayIncome *float64
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
	Checkin(baseURL, accessToken string, platformUserID int64, proxyURL string) (*CheckinResult, error)
	// Login performs a login on the platform, used for auto-relogin.
	Login(baseURL, username, password, proxyURL string) (*LoginResult, error)
	// GetBalance retrieves account balance information.
	GetBalance(baseURL, accessToken string, platformUserID int64, proxyURL string) (*BalanceInfo, error)
}

var registry = map[string]Adapter{}

// RegisterAdapter registers a platform adapter. Call this during init() in P4 adapter packages.
func RegisterAdapter(platform string, a Adapter) {
	registry[platform] = a
}

// GetAdapter returns the adapter for the given platform, or nil if not found.
func GetAdapter(platform string) Adapter {
	return registry[platform]
}
