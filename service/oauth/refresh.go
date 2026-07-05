package oauth

import (
	"fmt"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// ---- Singleflight Token Refresh ----

var (
	refreshInFlight   = make(map[int64]*refreshPromise)
	refreshInFlightMu sync.Mutex
)

type refreshPromise struct {
	ch    chan refreshResult
	ready bool
}

type refreshResult struct {
	AccountID   int64
	AccessToken string
	AccountKey  string
	ExtraConfig *string
	Err         error
}

// RefreshAccessTokenSingleflight refreshes an OAuth access token with singleflight dedup.
func RefreshAccessTokenSingleflight(accountID int64) (*refreshResult, error) {
	refreshInFlightMu.Lock()
	if p, exists := refreshInFlight[accountID]; exists && p.ready {
		ch := p.ch
		refreshInFlightMu.Unlock()
		result := <-ch
		return &result, result.Err
	}

	ch := make(chan refreshResult, 1)
	p := &refreshPromise{ch: ch, ready: true}
	refreshInFlight[accountID] = p
	refreshInFlightMu.Unlock()

	// Guaranteed cleanup even on panic.
	defer func() {
		refreshInFlightMu.Lock()
		delete(refreshInFlight, accountID)
		refreshInFlightMu.Unlock()
	}()

	result := doRefreshAccessToken(accountID)
	ch <- result

	if result.Err != nil {
		return nil, result.Err
	}
	return &result, nil
}

func doRefreshAccessToken(accountID int64) refreshResult {
	db := store.GetDB()
	if db == nil {
		return refreshResult{AccountID: accountID, Err: fmt.Errorf("database not initialized")}
	}

	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", accountID); err != nil {
		return refreshResult{AccountID: accountID, Err: fmt.Errorf("oauth account not found")}
	}

	oauth := GetOauthInfoFromAccount(&account)
	if oauth == nil || oauth.RefreshToken == "" {
		return refreshResult{AccountID: accountID, Err: fmt.Errorf("oauth refresh token missing")}
	}

	def := GetProviderDefinition(oauth.Provider)
	if def == nil {
		return refreshResult{AccountID: accountID, Err: fmt.Errorf("unsupported oauth provider: %s", oauth.Provider)}
	}

	var oauthCtx *RefreshOAuthContext
	if oauth.ProjectID != "" || oauth.ProviderData != nil {
		oauthCtx = &RefreshOAuthContext{
			ProjectID:    oauth.ProjectID,
			ProviderData: oauth.ProviderData,
		}
	}

	proxyURL := resolveAccountProxyURL(account.SiteID, account.ExtraConfig)

	refreshed, err := def.RefreshAccessToken(nil, RefreshTokenInput{
		RefreshToken: oauth.RefreshToken,
		OAuth:        oauthCtx,
		ProxyURL:     proxyURL,
	})
	if err != nil {
		return refreshResult{AccountID: accountID, Err: err}
	}

	// Merge refreshed fields with existing.
	nextOauth, err := BuildOauthInfoFromAccount(&account, &OauthInfo{
		Provider:   oauth.Provider,
		AccountID:  coalesceStr(refreshed.AccountID, oauth.AccountID),
		AccountKey: coalesceStr(refreshed.AccountKey, oauth.AccountKey, refreshed.AccountID, oauth.AccountID),
		Email:      coalesceStr(refreshed.Email, oauth.Email),
		PlanType:   coalesceStr(refreshed.PlanType, oauth.PlanType),
		ProjectID:  coalesceStr(refreshed.ProjectID, oauth.ProjectID),
		RefreshToken: coalesceStr(refreshed.RefreshToken, oauth.RefreshToken),
		TokenExpiresAt: coalesceInt64(refreshed.TokenExpiresAt, oauth.TokenExpiresAt),
		IDToken:    coalesceStr(refreshed.IDToken, oauth.IDToken),
		ProviderData: mergeProviderData(oauth.ProviderData, refreshed.ProviderData),
	})
	if err != nil {
		return refreshResult{AccountID: accountID, Err: err}
	}

	oauthMap := map[string]interface{}{
		"email":          nextOauth.Email,
		"planType":       nextOauth.PlanType,
		"tokenExpiresAt": nextOauth.TokenExpiresAt,
		"refreshToken":   nextOauth.RefreshToken,
		"idToken":        nextOauth.IDToken,
	}
	if nextOauth.ProviderData != nil {
		oauthMap["providerData"] = nextOauth.ProviderData
	}

	extraPatch := map[string]interface{}{
		"credentialMode": "session",
		"oauth":          oauthMap,
	}

	extraConfig := MergeAccountExtraConfig(account.ExtraConfig, extraPatch)
	accountKey := nextOauth.AccountKey
	if accountKey == "" {
		accountKey = nextOauth.AccountID
	}
	projectID := nextOauth.ProjectID

	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.Exec(
		`UPDATE accounts SET access_token = ?, oauth_provider = ?, oauth_account_key = ?,
		 oauth_project_id = ?, extra_config = ?, status = 'active', updated_at = ?
		 WHERE id = ?`,
		refreshed.AccessToken, oauth.Provider, accountKey, projectID,
		extraConfig, now, accountID,
	)
	if err != nil {
		return refreshResult{AccountID: accountID, Err: fmt.Errorf("token refresh persistence failed: %w", err)}
	}

	return refreshResult{
		AccountID:   accountID,
		AccessToken: refreshed.AccessToken,
		AccountKey:  accountKey,
		ExtraConfig: extraConfig,
	}
}

func coalesceStr(values ...string) string {
	for _, v := range values {
		if stringsTrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func coalesceInt64(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func mergeProviderData(existing, refreshed map[string]interface{}) map[string]interface{} {
	if existing == nil && refreshed == nil {
		return nil
	}
	result := make(map[string]interface{})
	for k, v := range existing {
		result[k] = v
	}
	for k, v := range refreshed {
		result[k] = v
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func stringsTrimSpace(s string) string {
	start, end := 0, len(s)
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}
	return s[start:end]
}

func resolveAccountProxyURL(siteID int64, extraConfig *string) *string {
	proxyURL := GetProxyURLFromExtraConfig(extraConfig)
	if proxyURL != "" {
		return &proxyURL
	}
	return nil
}
