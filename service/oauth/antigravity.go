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
)

const (
	antigravityAuthURL             = "https://accounts.google.com/o/oauth2/v2/auth"
	antigravityTokenURL            = "https://oauth2.googleapis.com/token"
	antigravityUserinfoURL         = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
	antigravityClientID            = "ANTIGRAVITY_CLIENT_ID_PLACEHOLDER"
	antigravityClientSecret        = "ANTIGRAVITY_CLIENT_SECRET_PLACEHOLDER"
	antigravityLoopbackPort        = 51121
	antigravityLoopbackPath        = "/oauth-callback"
	antigravityLoopbackRedirectURI = "http://localhost:51121/oauth-callback"
	antigravityUpstreamBaseURL     = "https://cloudcode-pa.googleapis.com"
	antigravityGoogleAPIClient     = "google-cloud-sdk vscode_cloudshelleditor/0.1"
	antigravityUserAgent           = "google-api-nodejs-client/9.15.1"
	antigravityModelsUserAgent     = "antigravity/1.19.6 darwin/arm64"
	antigravityClientMetadata      = `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`
	antigravityInternalAPIVersion  = "v1internal"
	antigravityOnboardPollIntervalMs = 2000
	antigravityOnboardMaxAttempts  = 5
)

var antigravityScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

func init() {
	RegisterProvider(&OAuthProviderDefinition{
		Metadata: ProviderMetadata{
			Provider:                    ProviderAntigravity,
			Label:                       "Antigravity",
			Platform:                    "antigravity",
			Enabled:                     true,
			LoginType:                   "oauth",
			RequiresProjectId:           false,
			SupportsDirectAccountRouting: true,
			SupportsCloudValidation:     true,
			SupportsNativeProxy:         true,
		},
		Site: ProviderSiteConfig{
			Name:     "Google Antigravity OAuth",
			URL:      antigravityUpstreamBaseURL,
			Platform: "antigravity",
		},
		Loopback: LoopbackConfig{
			Host:        "127.0.0.1",
			Port:        antigravityLoopbackPort,
			Path:        antigravityLoopbackPath,
			RedirectURI: antigravityLoopbackRedirectURI,
		},
		BuildAuthorizationURL:   buildAntigravityAuthorizationURL,
		ExchangeAuthorizationCode: exchangeAntigravityAuthorizationCode,
		RefreshAccessToken:      refreshAntigravityAccessToken,
		BuildProxyHeaders:       buildAntigravityProxyHeaders,
	})
}

// ---- Auth URL (no PKCE) ----

func buildAntigravityAuthorizationURL(ctx context.Context, input BuildAuthURLInput) (string, error) {
	params := url.Values{}
	params.Set("client_id", antigravityClientID)
	params.Set("redirect_uri", input.RedirectURI)
	params.Set("response_type", "code")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("scope", strings.Join(antigravityScopes, " "))
	params.Set("state", input.State)
	return antigravityAuthURL + "?" + params.Encode(), nil
}

// ---- Token Exchange ----

type antigravityOAuthTokenPayload struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	TokenType    string      `json:"token_type"`
	ExpiresIn    interface{} `json:"expires_in"`
	Scope        string      `json:"scope"`
	Expiry       string      `json:"expiry"`
}

type antigravityLoadCodeAssistPayload struct {
	CloudaicompanionProject interface{} `json:"cloudaicompanionProject"`
	AllowedTiers            interface{} `json:"allowedTiers"`
}

type antigravityOnboardUserPayload struct {
	Done     interface{} `json:"done"`
	Response interface{} `json:"response"`
}

func parseAntigravityExpiresAt(payload *antigravityOAuthTokenPayload) int64 {
	switch v := payload.ExpiresIn.(type) {
	case float64:
		if v > 0 {
			return time.Now().UnixMilli() + int64(v)*1000
		}
	case string:
		parsed, err := parseInt64Safe(strings.TrimSpace(v))
		if err == nil && parsed > 0 {
			return time.Now().UnixMilli() + parsed*1000
		}
	}
	if payload.Expiry != "" {
		if parsed, err := time.Parse(time.RFC3339, payload.Expiry); err == nil {
			return parsed.UnixMilli()
		}
	}
	return 0
}

func postAntigravityToken(form url.Values, proxyURL *string) (*TokenSet, error) {
	req, err := http.NewRequest("POST", antigravityTokenURL, strings.NewReader(form.Encode()))
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

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", string(respBody))
	}

	var payload antigravityOAuthTokenPayload
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("antigravity token exchange returned invalid payload")
	}

	accessToken := strings.TrimSpace(payload.AccessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("antigravity token exchange response missing access token")
	}

	providerData := make(map[string]interface{})
	if tt := strings.TrimSpace(payload.TokenType); tt != "" {
		providerData["tokenType"] = tt
	}
	if sc := strings.TrimSpace(payload.Scope); sc != "" {
		providerData["scope"] = sc
	}

	return &TokenSet{
		AccessToken:    accessToken,
		RefreshToken:   strings.TrimSpace(payload.RefreshToken),
		TokenExpiresAt: parseAntigravityExpiresAt(&payload),
		ProviderData:   providerData,
	}, nil
}

func exchangeAntigravityAuthorizationCode(ctx context.Context, input ExchangeCodeInput) (*TokenSet, error) {
	form := url.Values{}
	form.Set("code", input.Code)
	form.Set("client_id", antigravityClientID)
	form.Set("client_secret", antigravityClientSecret)
	form.Set("redirect_uri", input.RedirectURI)
	form.Set("grant_type", "authorization_code")

	token, err := postAntigravityToken(form, input.ProxyURL)
	if err != nil {
		return nil, err
	}

	email, _ := fetchAntigravityUserEmail(token.AccessToken, input.ProxyURL)
	projectID, _ := fetchAntigravityProjectID(token.AccessToken, input.ProxyURL)

	return &TokenSet{
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
		TokenExpiresAt: token.TokenExpiresAt,
		Email:          email,
		AccountKey:     email,
		AccountID:      email,
		ProjectID:      projectID,
		ProviderData:   token.ProviderData,
	}, nil
}

// ---- Token Refresh ----

func refreshAntigravityAccessToken(ctx context.Context, input RefreshTokenInput) (*TokenSet, error) {
	form := url.Values{}
	form.Set("client_id", antigravityClientID)
	form.Set("client_secret", antigravityClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", input.RefreshToken)

	token, err := postAntigravityToken(form, input.ProxyURL)
	if err != nil {
		return nil, err
	}

	email, _ := fetchAntigravityUserEmail(token.AccessToken, input.ProxyURL)

	var projectID string
	if input.OAuth != nil && input.OAuth.ProjectID != "" {
		projectID = input.OAuth.ProjectID
	} else {
		projectID, _ = fetchAntigravityProjectID(token.AccessToken, input.ProxyURL)
	}

	return &TokenSet{
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
		TokenExpiresAt: token.TokenExpiresAt,
		Email:          email,
		AccountKey:     email,
		AccountID:      email,
		ProjectID:      projectID,
		ProviderData:   token.ProviderData,
	}, nil
}

// ---- Proxy Headers ----

func buildAntigravityProxyHeaders(ctx context.Context, input ProxyHeaderInput) map[string]string {
	return map[string]string{
		"User-Agent":      antigravityUserAgent,
		"X-Goog-Api-Client": antigravityGoogleAPIClient,
		"Client-Metadata": antigravityClientMetadata,
	}
}

// ---- Onboarding ----

func buildAntigravityMetadata() geminiInternalMetadata {
	return geminiInternalMetadata{
		IDEType:    "ANTIGRAVITY",
		Platform:   "PLATFORM_UNSPECIFIED",
		PluginType: "GEMINI",
	}
}

func extractAntigravityProjectID(value interface{}) string {
	return extractGeminiProjectID(value)
}

func extractAntigravityDefaultTierID(payload *antigravityLoadCodeAssistPayload) string {
	tiers, ok := payload.AllowedTiers.([]interface{})
	if !ok {
		return "legacy-tier"
	}
	for _, rawTier := range tiers {
		tier, ok := rawTier.(map[string]interface{})
		if !ok {
			continue
		}
		if isDefault, _ := tier["isDefault"].(bool); isDefault {
			if id, ok := tier["id"].(string); ok {
				trimmed := strings.TrimSpace(id)
				if trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return "legacy-tier"
}

func callAntigravityInternalAPI(accessToken, method string, body map[string]interface{}, proxyURL *string) (map[string]interface{}, error) {
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/%s:%s", antigravityUpstreamBaseURL, antigravityInternalAPIVersion, method),
		bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent)
	req.Header.Set("X-Goog-Api-Client", antigravityGoogleAPIClient)
	req.Header.Set("Client-Metadata", antigravityClientMetadata)

	resp, err := doHTTP(req, proxyURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, nil
	}

	var result map[string]interface{}
	respBody, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(respBody, &result)
	return result, nil
}

func fetchAntigravityUserEmail(accessToken string, proxyURL *string) (string, error) {
	req, err := http.NewRequest("GET", antigravityUserinfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := doHTTP(req, proxyURL, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", nil
	}

	var payload struct {
		Email string `json:"email"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(respBody, &payload)
	return strings.TrimSpace(payload.Email), nil
}

func fetchAntigravityProjectID(accessToken string, proxyURL *string) (string, error) {
	metadata := buildAntigravityMetadata()
	resp, err := callAntigravityInternalAPI(accessToken, "loadCodeAssist", map[string]interface{}{
		"metadata": metadata,
	}, proxyURL)
	if err != nil || resp == nil {
		return "", nil
	}

	payload := &antigravityLoadCodeAssistPayload{}
	if v, ok := resp["cloudaicompanionProject"]; ok {
		payload.CloudaicompanionProject = v
	}
	if v, ok := resp["allowedTiers"]; ok {
		payload.AllowedTiers = v
	}

	discoveredFromLoad := extractAntigravityProjectID(payload.CloudaicompanionProject)
	if discoveredFromLoad != "" {
		return discoveredFromLoad, nil
	}

	tierID := extractAntigravityDefaultTierID(payload)
	for attempt := 0; attempt < antigravityOnboardMaxAttempts; attempt++ {
		onboardResp, err := callAntigravityInternalAPI(accessToken, "onboardUser", map[string]interface{}{
			"tierId":   tierID,
			"metadata": metadata,
		}, proxyURL)
		if err != nil || onboardResp == nil {
			return "", nil
		}
		if done, _ := onboardResp["done"].(bool); done {
			if r, ok := onboardResp["response"].(map[string]interface{}); ok {
				return extractAntigravityProjectID(r["cloudaicompanionProject"]), nil
			}
			return "", nil
		}
		if attempt+1 < antigravityOnboardMaxAttempts {
			time.Sleep(time.Duration(antigravityOnboardPollIntervalMs) * time.Millisecond)
		}
	}

	return "", nil
}
