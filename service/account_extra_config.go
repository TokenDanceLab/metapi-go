package service

import (
	"strconv"
	"strings"
)

// AutoReloginConfig holds credentials for auto-relogin.
type AutoReloginConfig struct {
	Username       string
	PasswordCipher string
}

// GetAutoReloginConfig reads autoRelogin from extraConfig.
// Returns nil if not configured.
func GetAutoReloginConfig(extraConfig *string) *AutoReloginConfig {
	cfg := ParseExtraConfig(extraConfig)
	if cfg == nil {
		return nil
	}
	relogin, ok := cfg["autoRelogin"].(map[string]any)
	if !ok {
		return nil
	}
	username, _ := relogin["username"].(string)
	passwordCipher, _ := relogin["passwordCipher"].(string)
	if username == "" || passwordCipher == "" {
		return nil
	}
	return &AutoReloginConfig{
		Username:       username,
		PasswordCipher: passwordCipher,
	}
}

// GetPlatformUserIdFromExtraConfig reads platformUserId from extraConfig.
func GetPlatformUserIdFromExtraConfig(extraConfig *string) (int64, bool) {
	cfg := ParseExtraConfig(extraConfig)
	if cfg == nil {
		return 0, false
	}
	if id, ok := cfg["platformUserId"].(float64); ok {
		return int64(id), true
	}
	return 0, false
}

// GuessPlatformUserIdFromUsername attempts to parse the username as a numeric user ID.
func GuessPlatformUserIdFromUsername(username *string) int64 {
	if username == nil {
		return 0
	}
	trimmed := strings.TrimSpace(*username)
	if trimmed == "" {
		return 0
	}
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// GetSub2ApiAuthFromExtraConfig reads sub2apiAuth from extraConfig.
func GetSub2ApiAuthFromExtraConfig(extraConfig *string) map[string]any {
	cfg := ParseExtraConfig(extraConfig)
	if cfg == nil {
		return nil
	}
	auth, ok := cfg["sub2apiAuth"].(map[string]any)
	if !ok {
		return nil
	}
	return auth
}

// BuildStoredSub2ApiSubscriptionSummary builds a storable subscription summary.
func BuildStoredSub2ApiSubscriptionSummary(summary any) map[string]any {
	return map[string]any{
		"planName":       "",
		"expiresAt":      "",
		"totalQuota":     0,
		"usedQuota":      0,
		"remainingQuota": 0,
		"daysRemaining":  0,
	}
}

// IsSub2ApiPlatform checks if a platform is Sub2Api.
func IsSub2ApiPlatform(platform string) bool {
	return strings.ToLower(platform) == "sub2api"
}

// IsManagedSub2ApiTokenDue checks if a managed Sub2Api token needs refresh.
func IsManagedSub2ApiTokenDue(tokenExpiresAt any) bool {
	if tokenExpiresAt == nil {
		return false
	}
	// A token is "due" for refresh if it expires within some window.
	// For now, always consider it due if present — real logic will be in P4.
	return true
}
