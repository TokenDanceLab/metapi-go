package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
)

const (
	claudeAuthURL              = "https://claude.ai/oauth/authorize"
	claudeTokenURL             = "https://api.anthropic.com/v1/oauth/token"
	claudeLoopbackPort         = 54545
	claudeLoopbackPath         = "/callback"
	claudeLoopbackRedirectURI  = "http://localhost:54545/callback"
	claudeUpstreamBaseURL      = "https://api.anthropic.com"
	claudeDefaultAnthropicVersion = "2023-06-01"
)

func init() {
	RegisterProvider(&OAuthProviderDefinition{
		Metadata: ProviderMetadata{
			Provider:                    ProviderClaude,
			Label:                       "Claude",
			Platform:                    "claude",
			Enabled:                     true,
			LoginType:                   "oauth",
			RequiresProjectId:           false,
			SupportsDirectAccountRouting: true,
			SupportsCloudValidation:     true,
			SupportsNativeProxy:         true,
		},
		Site: ProviderSiteConfig{
			Name:     "Anthropic Claude OAuth",
			URL:      claudeUpstreamBaseURL,
			Platform: "claude",
		},
		Loopback: LoopbackConfig{
			Host:        "127.0.0.1",
			Port:        claudeLoopbackPort,
			Path:        claudeLoopbackPath,
			RedirectURI: claudeLoopbackRedirectURI,
		},
		BuildAuthorizationURL:   buildClaudeAuthorizationURL,
		ExchangeAuthorizationCode: exchangeClaudeAuthorizationCode,
		RefreshAccessToken:      refreshClaudeAccessToken,
		BuildProxyHeaders:       buildClaudeProxyHeaders,
	})
}

func requireClaudeClientID() string {
	id := strings.TrimSpace(config.Get().ClaudeClientId)
	if id == "" {
		panic("CLAUDE_CLIENT_ID is not configured")
	}
	return id
}

// ---- Auth URL ----

func buildClaudeAuthorizationURL(ctx context.Context, input BuildAuthURLInput) (string, error) {
	params := url.Values{}
	params.Set("code", "true")
	params.Set("client_id", requireClaudeClientID())
	params.Set("response_type", "code")
	params.Set("redirect_uri", input.RedirectURI)
	params.Set("scope", "org:create_api_key user:profile user:inference")
	params.Set("code_challenge", CreatePKCEChallenge(input.CodeVerifier))
	params.Set("code_challenge_method", "S256")
	params.Set("state", input.State)
	return claudeAuthURL + "?" + params.Encode(), nil
}

// ---- Token Exchange ----

type claudeTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    interface{} `json:"expires_in"`
	Organization struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	} `json:"organization"`
	Account struct {
		UUID         string `json:"uuid"`
		EmailAddress string `json:"email_address"`
	} `json:"account"`
}

func parseClaudeExpiresAt(value interface{}) int64 {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return time.Now().UnixMilli() + int64(v)*1000
		}
	case string:
		parsed, err := parseInt64(strings.TrimSpace(v))
		if err == nil && parsed > 0 {
			return time.Now().UnixMilli() + parsed*1000
		}
	}
	return 0
}

func postClaudeToken(body map[string]interface{}, proxyURL *string) (*claudeTokenResponse, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", claudeTokenURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := doHTTP(req, proxyURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", string(respBody))
	}

	var payload claudeTokenResponse
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("claude token exchange returned invalid payload")
	}

	accessToken := strings.TrimSpace(payload.AccessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("claude token exchange response missing access token")
	}
	return &payload, nil
}

func exchangeClaudeAuthorizationCode(ctx context.Context, input ExchangeCodeInput) (*TokenSet, error) {
	payload, err := postClaudeToken(map[string]interface{}{
		"code":          input.Code,
		"state":         input.State,
		"grant_type":    "authorization_code",
		"client_id":     requireClaudeClientID(),
		"redirect_uri":  input.RedirectURI,
		"code_verifier": input.CodeVerifier,
	}, input.ProxyURL)
	if err != nil {
		return nil, err
	}

	accountID := strings.TrimSpace(payload.Account.UUID)
	email := strings.TrimSpace(payload.Account.EmailAddress)
	accountKey := accountID
	if accountKey == "" {
		accountKey = email
	}

	providerData := make(map[string]interface{})
	if orgID := strings.TrimSpace(payload.Organization.UUID); orgID != "" {
		providerData["organizationId"] = orgID
	}
	if orgName := strings.TrimSpace(payload.Organization.Name); orgName != "" {
		providerData["organizationName"] = orgName
	}

	return &TokenSet{
		AccessToken:    payload.AccessToken,
		RefreshToken:   payload.RefreshToken,
		TokenExpiresAt: parseClaudeExpiresAt(payload.ExpiresIn),
		Email:          email,
		AccountID:      accountID,
		AccountKey:     accountKey,
		ProviderData:   providerData,
	}, nil
}

// ---- Token Refresh ----

func refreshClaudeAccessToken(ctx context.Context, input RefreshTokenInput) (*TokenSet, error) {
	payload, err := postClaudeToken(map[string]interface{}{
		"client_id":     requireClaudeClientID(),
		"grant_type":    "refresh_token",
		"refresh_token": input.RefreshToken,
	}, input.ProxyURL)
	if err != nil {
		return nil, err
	}

	accountID := strings.TrimSpace(payload.Account.UUID)
	email := strings.TrimSpace(payload.Account.EmailAddress)
	accountKey := accountID
	if accountKey == "" {
		accountKey = email
	}

	providerData := make(map[string]interface{})
	if orgID := strings.TrimSpace(payload.Organization.UUID); orgID != "" {
		providerData["organizationId"] = orgID
	}
	if orgName := strings.TrimSpace(payload.Organization.Name); orgName != "" {
		providerData["organizationName"] = orgName
	}

	return &TokenSet{
		AccessToken:    payload.AccessToken,
		RefreshToken:   payload.RefreshToken,
		TokenExpiresAt: parseClaudeExpiresAt(payload.ExpiresIn),
		Email:          email,
		AccountID:      accountID,
		AccountKey:     accountKey,
		ProviderData:   providerData,
	}, nil
}

// ---- Proxy Headers ----

func buildClaudeProxyHeaders(ctx context.Context, input ProxyHeaderInput) map[string]string {
	return map[string]string{
		"anthropic-version": claudeDefaultAnthropicVersion,
	}
}

func parseInt64(s string) (int64, error) {
	n := int64(0)
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
