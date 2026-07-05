package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
)

const (
	codexAuthURL           = "https://auth.openai.com/oauth/authorize"
	codexTokenURL          = "https://auth.openai.com/oauth/token"
	codexLoopbackPort      = 1455
	codexLoopbackPath      = "/auth/callback"
	codexLoopbackRedirectURI = "http://localhost:1455/auth/callback"
	codexUpstreamBaseURL   = "https://chatgpt.com/backend-api/codex"
)

func init() {
	RegisterProvider(&OAuthProviderDefinition{
		Metadata: ProviderMetadata{
			Provider:                    ProviderCodex,
			Label:                       "Codex",
			Platform:                    "codex",
			Enabled:                     true,
			LoginType:                   "oauth",
			RequiresProjectId:           false,
			SupportsDirectAccountRouting: true,
			SupportsCloudValidation:     true,
			SupportsNativeProxy:         true,
		},
		Site: ProviderSiteConfig{
			Name:     "ChatGPT Codex OAuth",
			URL:      codexUpstreamBaseURL,
			Platform: "codex",
		},
		Loopback: LoopbackConfig{
			Host:        "127.0.0.1",
			Port:        codexLoopbackPort,
			Path:        codexLoopbackPath,
			RedirectURI: codexLoopbackRedirectURI,
		},
		BuildAuthorizationURL:  buildCodexAuthorizationURL,
		ExchangeAuthorizationCode: exchangeCodexAuthorizationCode,
		RefreshAccessToken:     refreshCodexAccessToken,
		BuildProxyHeaders:      buildCodexProxyHeaders,
	})
}

func requireCodexClientID() (string, error) {
	id := strings.TrimSpace(config.Get().CodexClientId)
	if id == "" {
		return "", fmt.Errorf("CODEX_CLIENT_ID is not configured")
	}
	return id, nil
}

// ---- Auth URL ----

func buildCodexAuthorizationURL(ctx context.Context, input BuildAuthURLInput) (string, error) {
	params := url.Values{}
	clientID, err := requireCodexClientID()
	if err != nil {
		return "", err
	}
	params.Set("client_id", clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", input.RedirectURI)
	params.Set("scope", "openid email profile offline_access")
	params.Set("state", input.State)
	params.Set("code_challenge", CreatePKCEChallenge(input.CodeVerifier))
	params.Set("code_challenge_method", "S256")
	params.Set("prompt", "login")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	return codexAuthURL + "?" + params.Encode(), nil
}

// ---- Token Exchange ----

type codexJWTClaims struct {
	Email string `json:"email"`
	Auth  struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
		ChatGPTPlanType  string `json:"chatgpt_plan_type"`
	} `json:"https://api.openai.com/auth"`
}

type codexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    interface{} `json:"expires_in"`
}

func parseJWTClaims(token string) (*codexJWTClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT")
	}
	decoded, err := base64Decode(parts[1])
	if err != nil {
		return nil, err
	}
	var claims codexJWTClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func parseExpiresIn(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int64(v), true
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil && parsed > 0 {
			return parsed, true
		}
	}
	return 0, false
}

func exchangeCodexToken(form url.Values, proxyURL *string) (*codexTokenResponse, error) {
	req, err := http.NewRequest("POST", codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := doHTTP(req, proxyURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", string(body))
	}

	var payload codexTokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("codex token exchange returned invalid payload")
	}

	accessToken := strings.TrimSpace(payload.AccessToken)
	refreshToken := strings.TrimSpace(payload.RefreshToken)
	idToken := strings.TrimSpace(payload.IDToken)
	expiresIn, hasExpiresIn := parseExpiresIn(payload.ExpiresIn)

	if accessToken == "" || refreshToken == "" || idToken == "" || !hasExpiresIn || expiresIn <= 0 {
		return nil, fmt.Errorf("codex token exchange response missing required fields")
	}
	return &payload, nil
}

func exchangeCodexAuthorizationCode(ctx context.Context, input ExchangeCodeInput) (*TokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", func() string { id, _ := requireCodexClientID(); return id }())
	form.Set("code", input.Code)
	form.Set("redirect_uri", input.RedirectURI)
	form.Set("code_verifier", input.CodeVerifier)

	payload, err := exchangeCodexToken(form, input.ProxyURL)
	if err != nil {
		return nil, err
	}
	if _, idErr := requireCodexClientID(); idErr != nil {
		return nil, idErr
	}

	claims, err := parseJWTClaims(payload.IDToken)
	if err != nil {
		return nil, fmt.Errorf("codex token exchange response invalid id_token")
	}

	accountID := strings.TrimSpace(claims.Auth.ChatGPTAccountID)
	if accountID == "" {
		return nil, fmt.Errorf("codex token exchange response missing chatgpt_account_id")
	}

	expiresIn, _ := parseExpiresIn(payload.ExpiresIn)
	return &TokenSet{
		AccessToken:    payload.AccessToken,
		RefreshToken:   payload.RefreshToken,
		TokenExpiresAt: time.Now().UnixMilli() + expiresIn*1000,
		Email:          strings.TrimSpace(claims.Email),
		AccountID:      accountID,
		AccountKey:     accountID,
		PlanType:       strings.TrimSpace(claims.Auth.ChatGPTPlanType),
		IDToken:        payload.IDToken,
	}, nil
}

// ---- Token Refresh ----

func refreshCodexAccessToken(ctx context.Context, input RefreshTokenInput) (*TokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", func() string { id, _ := requireCodexClientID(); return id }())
	form.Set("refresh_token", input.RefreshToken)
	form.Set("scope", "openid profile email")

	payload, err := exchangeCodexToken(form, input.ProxyURL)
	if err != nil {
		return nil, err
	}

	claims, err := parseJWTClaims(payload.IDToken)
	if err != nil {
		return nil, fmt.Errorf("codex token refresh response invalid id_token")
	}

	accountID := strings.TrimSpace(claims.Auth.ChatGPTAccountID)
	if accountID == "" {
		return nil, fmt.Errorf("codex token refresh response missing chatgpt_account_id")
	}

	expiresIn, _ := parseExpiresIn(payload.ExpiresIn)
	return &TokenSet{
		AccessToken:    payload.AccessToken,
		RefreshToken:   payload.RefreshToken,
		TokenExpiresAt: time.Now().UnixMilli() + expiresIn*1000,
		Email:          strings.TrimSpace(claims.Email),
		AccountID:      accountID,
		AccountKey:     accountID,
		PlanType:       strings.TrimSpace(claims.Auth.ChatGPTPlanType),
		IDToken:        payload.IDToken,
	}, nil
}

// ---- Proxy Headers ----

func buildCodexProxyHeaders(ctx context.Context, input ProxyHeaderInput) map[string]string {
	accountID := input.OAuth.AccountID
	if accountID == "" {
		accountID = input.OAuth.AccountKey
	}
	originator := getHeaderValue(input.DownstreamHeaders, "originator")
	if originator == "" {
		originator = "codex_cli_rs"
	}
	headers := map[string]string{
		"Originator": originator,
	}
	if accountID != "" {
		headers["Chatgpt-Account-Id"] = accountID
	}
	return headers
}

// ---- Utilities ----

func base64Decode(encoded string) ([]byte, error) {
	// Handle standard and URL-safe base64.
	decoded, err := base64DecodeRaw(encoded)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func base64DecodeRaw(encoded string) ([]byte, error) {
	// Add padding if needed.
	switch len(encoded) % 4 {
	case 2:
		encoded += "=="
	case 3:
		encoded += "="
	}
	return base64.URLEncoding.DecodeString(
		strings.NewReplacer("-", "+", "_", "/").Replace(encoded),
	)
}

func getHeaderValue(headers map[string]interface{}, key string) string {
	if headers == nil {
		return ""
	}
	lowerKey := strings.ToLower(key)
	for k, v := range headers {
		if strings.ToLower(k) == lowerKey {
			if s, ok := v.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					return trimmed
				}
			}
			if arr, ok := v.([]interface{}); ok {
				for _, item := range arr {
					if s, ok := item.(string); ok {
						trimmed := strings.TrimSpace(s)
						if trimmed != "" {
							return trimmed
						}
					}
				}
			}
		}
	}
	return ""
}

func doHTTP(req *http.Request, proxyURL *string, client *http.Client) (*http.Response, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if proxyURL != nil && *proxyURL != "" {
		proxy, err := url.Parse(*proxyURL)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxy),
			}
		}
	}
	return client.Do(req)
}
