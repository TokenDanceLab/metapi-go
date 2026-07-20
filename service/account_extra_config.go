package service

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
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

// NormalizeManagedRefreshToken accepts a non-empty trimmed string refresh token.
// Returns ok=false for nil/empty/invalid values so callers preserve existing auth.
func NormalizeManagedRefreshToken(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	s, ok := value.(string)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

// NormalizeManagedTokenExpiresAt accepts a positive epoch-seconds value
// (number or numeric string). Returns ok=false when missing/invalid.
func NormalizeManagedTokenExpiresAt(value any) (int64, bool) {
	if value == nil {
		return 0, false
	}
	switch v := value.(type) {
	case int64:
		if v > 0 {
			return v, true
		}
	case int:
		if v > 0 {
			return int64(v), true
		}
	case int32:
		if v > 0 {
			return int64(v), true
		}
	case float64:
		if v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			return int64(v), true
		}
	case float32:
		if v > 0 && !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) {
			return int64(v), true
		}
	case json.Number:
		i, err := v.Int64()
		if err == nil && i > 0 {
			return i, true
		}
		f, err := v.Float64()
		if err == nil && f > 0 && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return int64(f), true
		}
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		if i, err := strconv.ParseInt(trimmed, 10, 64); err == nil && i > 0 {
			return i, true
		}
		if f, err := strconv.ParseFloat(trimmed, 64); err == nil && f > 0 && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return int64(f), true
		}
	}
	return 0, false
}

// MergeSub2ApiAuth merges managed auth fields onto an existing sub2apiAuth map.
// Valid new values overwrite; invalid/missing values preserve existing keys.
// Unrelated keys on existing are always preserved. Returns nil when empty.
func MergeSub2ApiAuth(existing map[string]any, refreshToken any, tokenExpiresAt any) map[string]any {
	out := map[string]any{}
	if existing != nil {
		for k, v := range existing {
			out[k] = v
		}
	}
	if rt, ok := NormalizeManagedRefreshToken(refreshToken); ok {
		out["refreshToken"] = rt
	}
	if exp, ok := NormalizeManagedTokenExpiresAt(tokenExpiresAt); ok {
		out["tokenExpiresAt"] = exp
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildMergedSub2ApiAuth builds the next sub2apiAuth object for a sub2api account.
// Sources (later wins for the same field):
//  1. existing extraConfig.sub2apiAuth
//  2. optional nested extraConfigPatch["sub2apiAuth"]
//  3. top-level refreshToken / tokenExpiresAt pointers
//
// Returns nil when there are no valid fields to write (caller should leave extraConfig alone).
func BuildMergedSub2ApiAuth(existingExtraConfig *string, refreshToken *string, tokenExpiresAt *int64, extraConfigPatch map[string]any) map[string]any {
	var patchRT any
	var patchExp any
	if extraConfigPatch != nil {
		if auth, ok := extraConfigPatch["sub2apiAuth"].(map[string]any); ok && auth != nil {
			if v, ok := auth["refreshToken"]; ok {
				patchRT = v
			}
			if v, ok := auth["tokenExpiresAt"]; ok {
				patchExp = v
			}
		}
	}

	hasIncoming := false
	var rt any = patchRT
	var exp any = patchExp
	if refreshToken != nil {
		rt = *refreshToken
		hasIncoming = true
	} else if patchRT != nil {
		hasIncoming = true
	}
	if tokenExpiresAt != nil {
		exp = *tokenExpiresAt
		hasIncoming = true
	} else if patchExp != nil {
		hasIncoming = true
	}
	if !hasIncoming {
		return nil
	}

	// Only produce a write when at least one incoming field normalizes successfully,
	// otherwise leave existing managed auth untouched.
	_, rtOK := NormalizeManagedRefreshToken(rt)
	_, expOK := NormalizeManagedTokenExpiresAt(exp)
	if !rtOK && !expOK {
		return nil
	}

	existing := GetSub2ApiAuthFromExtraConfig(existingExtraConfig)
	return MergeSub2ApiAuth(existing, rt, exp)
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
	return strings.EqualFold(strings.TrimSpace(platform), "sub2api")
}

// IsManagedSub2ApiTokenDue checks if a managed Sub2Api token needs refresh.
// A token is "due" if it expires within sub2apiRefreshLeadSeconds of now.
func IsManagedSub2ApiTokenDue(tokenExpiresAt any) bool {
	if tokenExpiresAt == nil {
		return false
	}
	exp, ok := NormalizeManagedTokenExpiresAt(tokenExpiresAt)
	if !ok || exp <= 0 {
		return false
	}
	now := time.Now().Unix()
	// Use a 5-minute lead window: refresh if token expires within 300s.
	const sub2apiRefreshLeadSeconds int64 = 300
	return exp-now <= sub2apiRefreshLeadSeconds
}
