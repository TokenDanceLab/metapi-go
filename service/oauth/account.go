package oauth

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// ---- OAuth Info types ----

// OauthModelDiscoveryStatus is the model discovery status for an OAuth account.
type OauthModelDiscoveryStatus string

const (
	OauthModelDiscoveryHealthy  OauthModelDiscoveryStatus = "healthy"
	OauthModelDiscoveryAbnormal OauthModelDiscoveryStatus = "abnormal"
)

// OauthInfo holds the full OAuth information for an account.
type OauthInfo struct {
	Provider             string                 `json:"provider"`
	AccountID            string                 `json:"accountId,omitempty"`
	AccountKey           string                 `json:"accountKey,omitempty"`
	Email                string                 `json:"email,omitempty"`
	PlanType             string                 `json:"planType,omitempty"`
	ProjectID            string                 `json:"projectId,omitempty"`
	TokenExpiresAt       int64                  `json:"tokenExpiresAt,omitempty"`
	RefreshToken         string                 `json:"refreshToken,omitempty"`
	IDToken              string                 `json:"idToken,omitempty"`
	ProviderData         map[string]interface{} `json:"providerData,omitempty"`
	Quota                *OauthQuotaSnapshot     `json:"quota,omitempty"`
	ModelDiscoveryStatus OauthModelDiscoveryStatus `json:"modelDiscoveryStatus,omitempty"`
	LastModelSyncAt      string                 `json:"lastModelSyncAt,omitempty"`
	LastModelSyncError   string                 `json:"lastModelSyncError,omitempty"`
	LastDiscoveredModels []string               `json:"lastDiscoveredModels,omitempty"`
}

// StoredOauthState is OauthInfo minus identity fields (provider, accountId, accountKey, projectId).
type StoredOauthState struct {
	Email                string                 `json:"email,omitempty"`
	PlanType             string                 `json:"planType,omitempty"`
	TokenExpiresAt       int64                  `json:"tokenExpiresAt,omitempty"`
	RefreshToken         string                 `json:"refreshToken,omitempty"`
	IDToken              string                 `json:"idToken,omitempty"`
	ProviderData         map[string]interface{} `json:"providerData,omitempty"`
	Quota                *OauthQuotaSnapshot     `json:"quota,omitempty"`
	ModelDiscoveryStatus OauthModelDiscoveryStatus `json:"modelDiscoveryStatus,omitempty"`
	LastModelSyncAt      string                 `json:"lastModelSyncAt,omitempty"`
	LastModelSyncError   string                 `json:"lastModelSyncError,omitempty"`
	LastDiscoveredModels []string               `json:"lastDiscoveredModels,omitempty"`
}

// ---- Parsing ----

type parsedExtraConfig struct {
	OAuth *parsedOauthInfo `json:"oauth"`
}

type parsedOauthInfo struct {
	Provider             string      `json:"provider"`
	AccountID            interface{} `json:"accountId"`
	AccountKey           interface{} `json:"accountKey"`
	Email                interface{} `json:"email"`
	PlanType             interface{} `json:"planType"`
	ProjectID            interface{} `json:"projectId"`
	TokenExpiresAt       interface{} `json:"tokenExpiresAt"`
	RefreshToken         interface{} `json:"refreshToken"`
	IDToken              interface{} `json:"idToken"`
	ProviderData         interface{} `json:"providerData"`
	Quota                interface{} `json:"quota"`
	ModelDiscoveryStatus interface{} `json:"modelDiscoveryStatus"`
	LastModelSyncAt      interface{} `json:"lastModelSyncAt"`
	LastModelSyncError   interface{} `json:"lastModelSyncError"`
	LastDiscoveredModels interface{} `json:"lastDiscoveredModels"`
}

func parseRawExtraConfig(extraConfig *string) *parsedExtraConfig {
	if extraConfig == nil || strings.TrimSpace(*extraConfig) == "" {
		return nil
	}
	var result parsedExtraConfig
	if err := json.Unmarshal([]byte(*extraConfig), &result); err != nil {
		return nil
	}
	return &result
}

// GetOauthInfoFromExtraConfig reads OAuth info from extraConfig JSON.
func GetOauthInfoFromExtraConfig(extraConfig *string) *OauthInfo {
	parsed := parseRawExtraConfig(extraConfig)
	if parsed == nil || parsed.OAuth == nil {
		return nil
	}
	o := parsed.OAuth
	provider := strings.TrimSpace(o.Provider)
	if provider == "" {
		return nil
	}
	info := &OauthInfo{
		Provider:   provider,
		AccountID:  asNonEmptyString(o.AccountID),
		AccountKey: asNonEmptyString(o.AccountKey),
		ProjectID:  asNonEmptyString(o.ProjectID),
		Email:      asNonEmptyString(o.Email),
		PlanType:   asNonEmptyString(o.PlanType),
	}
	if info.AccountID == "" {
		info.AccountID = info.AccountKey
	}
	if info.AccountKey == "" {
		info.AccountKey = info.AccountID
	}
	if v := asPositiveInteger(o.TokenExpiresAt); v > 0 {
		info.TokenExpiresAt = v
	}
	info.RefreshToken = asNonEmptyString(o.RefreshToken)
	info.IDToken = asNonEmptyString(o.IDToken)
	if pd, ok := o.ProviderData.(map[string]interface{}); ok {
		info.ProviderData = pd
	}
	info.Quota = asQuotaSnapshotGo(o.Quota)
	if s := asNonEmptyString(o.ModelDiscoveryStatus); s != "" {
		lowered := strings.ToLower(s)
		if lowered == "healthy" {
			info.ModelDiscoveryStatus = OauthModelDiscoveryHealthy
		} else if lowered == "abnormal" {
			info.ModelDiscoveryStatus = OauthModelDiscoveryAbnormal
		}
	}
	info.LastModelSyncAt = asISODateTime(o.LastModelSyncAt)
	info.LastModelSyncError = asNonEmptyString(o.LastModelSyncError)
	if arr, ok := o.LastDiscoveredModels.([]interface{}); ok {
		for _, item := range arr {
			if s, ok := item.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					info.LastDiscoveredModels = append(info.LastDiscoveredModels, trimmed)
				}
			}
		}
	}
	return info
}

// GetOauthInfoFromAccount reads OAuth info from an account record.
// Prefers column fields over extraConfig.oauth fields.
func GetOauthInfoFromAccount(account *store.Account) *OauthInfo {
	if account == nil {
		return nil
	}
	storedIdentity := GetOauthInfoFromExtraConfig(account.ExtraConfig)
	provider := ""
	if account.OAuthProvider != nil {
		provider = strings.TrimSpace(*account.OAuthProvider)
	}
	if provider == "" && storedIdentity != nil {
		provider = storedIdentity.Provider
	}
	if provider == "" {
		return nil
	}

	var accountKey string
	if account.OAuthAccountKey != nil {
		accountKey = strings.TrimSpace(*account.OAuthAccountKey)
	}
	if accountKey == "" && storedIdentity != nil {
		accountKey = storedIdentity.AccountKey
	}
	if accountKey == "" && storedIdentity != nil {
		accountKey = storedIdentity.AccountID
	}

	accountID := storedIdentity.AccountID
	if accountID == "" {
		accountID = accountKey
	}

	var projectID string
	if account.OAuthProjectID != nil {
		projectID = strings.TrimSpace(*account.OAuthProjectID)
	}
	if projectID == "" && storedIdentity != nil {
		projectID = storedIdentity.ProjectID
	}

	info := &OauthInfo{
		Provider:   provider,
		AccountID:  accountID,
		AccountKey: accountKey,
		ProjectID:  projectID,
	}
	if storedIdentity != nil {
		info.Email = storedIdentity.Email
		info.PlanType = storedIdentity.PlanType
		info.TokenExpiresAt = storedIdentity.TokenExpiresAt
		info.RefreshToken = storedIdentity.RefreshToken
		info.IDToken = storedIdentity.IDToken
		info.ProviderData = storedIdentity.ProviderData
		info.Quota = storedIdentity.Quota
		info.ModelDiscoveryStatus = storedIdentity.ModelDiscoveryStatus
		info.LastModelSyncAt = storedIdentity.LastModelSyncAt
		info.LastModelSyncError = storedIdentity.LastModelSyncError
		info.LastDiscoveredModels = storedIdentity.LastDiscoveredModels
	}
	return info
}

// BuildOauthInfo builds OauthInfo from extraConfig with an optional patch.
// Returns an error when no OAuth provider can be determined — the caller should
// return a clean 4xx error to the client rather than panicking.
func BuildOauthInfo(extraConfig *string, patch *OauthInfo) (*OauthInfo, error) {
	provider := ""
	if patch != nil {
		provider = patch.Provider
	}
	if provider == "" {
		existing := GetOauthInfoFromExtraConfig(extraConfig)
		if existing != nil {
			provider = existing.Provider
		}
	}
	if provider == "" {
		return nil, fmt.Errorf("oauth provider is required")
	}
	current := GetOauthInfoFromExtraConfig(extraConfig)
	info := &OauthInfo{Provider: provider}
	if current != nil {
		*info = *current
	}
	if patch != nil {
		if patch.AccountID != "" {
			info.AccountID = patch.AccountID
		}
		if patch.AccountKey != "" {
			info.AccountKey = patch.AccountKey
		}
		if patch.Email != "" {
			info.Email = patch.Email
		}
		if patch.PlanType != "" {
			info.PlanType = patch.PlanType
		}
		if patch.ProjectID != "" {
			info.ProjectID = patch.ProjectID
		}
		if patch.TokenExpiresAt > 0 {
			info.TokenExpiresAt = patch.TokenExpiresAt
		}
		if patch.RefreshToken != "" {
			info.RefreshToken = patch.RefreshToken
		}
		if patch.IDToken != "" {
			info.IDToken = patch.IDToken
		}
		if patch.ProviderData != nil {
			info.ProviderData = patch.ProviderData
		}
		if patch.Quota != nil {
			info.Quota = patch.Quota
		}
		if patch.ModelDiscoveryStatus != "" {
			info.ModelDiscoveryStatus = patch.ModelDiscoveryStatus
		}
		if patch.LastModelSyncAt != "" {
			info.LastModelSyncAt = patch.LastModelSyncAt
		}
		if patch.LastModelSyncError != "" {
			info.LastModelSyncError = patch.LastModelSyncError
		}
		if patch.LastDiscoveredModels != nil {
			info.LastDiscoveredModels = patch.LastDiscoveredModels
		}
	}
	if info.AccountKey == "" && info.AccountID != "" {
		info.AccountKey = info.AccountID
	}
	if info.AccountID == "" && info.AccountKey != "" {
		info.AccountID = info.AccountKey
	}
	return info, nil
}

// BuildOauthInfoFromAccount builds OauthInfo from an account with optional patch.
// Returns an error when no OAuth provider can be determined.
func BuildOauthInfoFromAccount(account *store.Account, patch *OauthInfo) (*OauthInfo, error) {
	provider := ""
	if patch != nil {
		provider = patch.Provider
	}
	if provider == "" {
		existing := GetOauthInfoFromAccount(account)
		if existing != nil {
			provider = existing.Provider
		}
	}
	if provider == "" {
		return nil, fmt.Errorf("oauth provider is required")
	}
	current := GetOauthInfoFromAccount(account)
	info := &OauthInfo{Provider: provider}
	if current != nil {
		*info = *current
	}
	if patch != nil {
		if patch.AccountID != "" {
			info.AccountID = patch.AccountID
		}
		if patch.AccountKey != "" {
			info.AccountKey = patch.AccountKey
		}
		if patch.Email != "" {
			info.Email = patch.Email
		}
		if patch.PlanType != "" {
			info.PlanType = patch.PlanType
		}
		if patch.ProjectID != "" {
			info.ProjectID = patch.ProjectID
		}
		if patch.TokenExpiresAt > 0 {
			info.TokenExpiresAt = patch.TokenExpiresAt
		}
		if patch.RefreshToken != "" {
			info.RefreshToken = patch.RefreshToken
		}
		if patch.IDToken != "" {
			info.IDToken = patch.IDToken
		}
		if patch.ProviderData != nil {
			info.ProviderData = patch.ProviderData
		}
		if patch.Quota != nil {
			info.Quota = patch.Quota
		}
	}
	if info.AccountKey == "" && info.AccountID != "" {
		info.AccountKey = info.AccountID
	}
	if info.AccountID == "" && info.AccountKey != "" {
		info.AccountID = info.AccountKey
	}
	return info, nil
}

// BuildStoredOauthState strips identity fields from OauthInfo.
func BuildStoredOauthState(oauth *OauthInfo) *StoredOauthState {
	if oauth == nil {
		return nil
	}
	return &StoredOauthState{
		Email:                oauth.Email,
		PlanType:             oauth.PlanType,
		TokenExpiresAt:       oauth.TokenExpiresAt,
		RefreshToken:         oauth.RefreshToken,
		IDToken:              oauth.IDToken,
		ProviderData:         oauth.ProviderData,
		Quota:                oauth.Quota,
		ModelDiscoveryStatus: oauth.ModelDiscoveryStatus,
		LastModelSyncAt:      oauth.LastModelSyncAt,
		LastModelSyncError:   oauth.LastModelSyncError,
		LastDiscoveredModels: oauth.LastDiscoveredModels,
	}
}

// BuildStoredOauthStateFromAccount builds stored state from account with optional patch.
func BuildStoredOauthStateFromAccount(account *store.Account, patch *OauthInfo) (*StoredOauthState, error) {
	info, err := BuildOauthInfoFromAccount(account, patch)
	if err != nil {
		return nil, err
	}
	return BuildStoredOauthState(info), nil
}

// BuildOauthIdentityBackfillPatch produces backfill patches for column fields.
func BuildOauthIdentityBackfillPatch(account *store.Account) map[string]*string {
	if account == nil {
		return nil
	}
	legacy := GetOauthInfoFromExtraConfig(account.ExtraConfig)
	if legacy == nil {
		return nil
	}
	patch := make(map[string]*string)
	if (account.OAuthProvider == nil || strings.TrimSpace(*account.OAuthProvider) == "") && legacy.Provider != "" {
		s := legacy.Provider
		patch["oauthProvider"] = &s
	}
	if (account.OAuthAccountKey == nil || strings.TrimSpace(*account.OAuthAccountKey) == "") && (legacy.AccountKey != "" || legacy.AccountID != "") {
		s := legacy.AccountKey
		if s == "" {
			s = legacy.AccountID
		}
		if s != "" {
			patch["oauthAccountKey"] = &s
		}
	}
	if (account.OAuthProjectID == nil || strings.TrimSpace(*account.OAuthProjectID) == "") && legacy.ProjectID != "" {
		s := legacy.ProjectID
		patch["oauthProjectId"] = &s
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

// ---- ExtraConfig helpers ----

// MergeAccountExtraConfig merges a patch into an existing extraConfig JSON string.
func MergeAccountExtraConfig(existing *string, patch map[string]interface{}) *string {
	base := make(map[string]interface{})
	if existing != nil && strings.TrimSpace(*existing) != "" {
		if err := json.Unmarshal([]byte(*existing), &base); err != nil {
			base = make(map[string]interface{})
		}
	}
	for k, v := range patch {
		if v == nil {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	if len(base) == 0 {
		return nil
	}
	b, err := json.Marshal(base)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

// GetProxyURLFromExtraConfig reads proxyUrl from extraConfig.
func GetProxyURLFromExtraConfig(extraConfig *string) string {
	parsed := parseRawExtraConfigGo(extraConfig)
	if parsed == nil {
		return ""
	}
	if v, ok := parsed["proxyUrl"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// GetUseSystemProxyFromExtraConfig reads useSystemProxy from extraConfig.
func GetUseSystemProxyFromExtraConfig(extraConfig *string) bool {
	parsed := parseRawExtraConfigGo(extraConfig)
	if parsed == nil {
		return false
	}
	if v, ok := parsed["useSystemProxy"].(bool); ok {
		return v
	}
	return false
}

func parseRawExtraConfigGo(extraConfig *string) map[string]interface{} {
	if extraConfig == nil || strings.TrimSpace(*extraConfig) == "" {
		return nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(*extraConfig), &result); err != nil {
		return nil
	}
	return result
}

// ---- Type helpers reused across the package ----

func asPositiveInteger(value interface{}) int64 {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int64(v)
		}
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0
		}
		var n int64
		if _, err := fmt.Sscanf(trimmed, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func asISODateTime(value interface{}) string {
	s := asNonEmptyString(value)
	if s == "" {
		return ""
	}
	// Validate it's a valid ISO date.
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return s
	}
	// Try other common formats.
	for _, layout := range []string{
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return ""
}

func asQuotaSnapshotGo(value interface{}) *OauthQuotaSnapshot {
	if value == nil {
		return nil
	}
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	status, _ := m["status"].(string)
	source, _ := m["source"].(string)
	if status == "" || source == "" {
		return nil
	}
	snapshot := &OauthQuotaSnapshot{
		Status: status,
		Source: source,
	}
	if v, ok := m["lastSyncAt"].(string); ok {
		snapshot.LastSyncAt = v
	}
	if v, ok := m["lastError"].(string); ok {
		snapshot.LastError = v
	}
	if v, ok := m["providerMessage"].(string); ok {
		snapshot.ProviderMessage = v
	}
	snapshot.Windows = &OauthQuotaWindows{}
	if windows, ok := m["windows"].(map[string]interface{}); ok {
		if fh, ok := windows["fiveHour"].(map[string]interface{}); ok {
			snapshot.Windows.FiveHour = normalizeQuotaWindowGo(fh)
		}
		if sd, ok := windows["sevenDay"].(map[string]interface{}); ok {
			snapshot.Windows.SevenDay = normalizeQuotaWindowGo(sd)
		}
	}
	return snapshot
}

func normalizeQuotaWindowGo(raw map[string]interface{}) *OauthQuotaWindowSnapshot {
	supported, _ := raw["supported"].(bool)
	w := &OauthQuotaWindowSnapshot{Supported: supported}
	if v, ok := raw["limit"].(float64); ok {
		w.Limit = &v
	}
	if v, ok := raw["used"].(float64); ok {
		w.Used = &v
	}
	if v, ok := raw["remaining"].(float64); ok {
		w.Remaining = &v
	}
	if v, ok := raw["resetAt"].(string); ok {
		w.ResetAt = v
	}
	if v, ok := raw["message"].(string); ok {
		w.Message = v
	}
	return w
}
