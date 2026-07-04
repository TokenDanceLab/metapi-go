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
	geminiCliAuthURL                   = "https://accounts.google.com/o/oauth2/v2/auth"
	geminiCliTokenURL                  = "https://oauth2.googleapis.com/token"
	geminiCliUserinfoURL               = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
	geminiCliProjectsURL               = "https://cloudresourcemanager.googleapis.com/v1/projects"
	geminiCliServiceUsageURL           = "https://serviceusage.googleapis.com/v1"
	geminiCliLoopbackPort              = 8085
	geminiCliLoopbackPath              = "/oauth2callback"
	geminiCliLoopbackRedirectURI       = "http://localhost:8085/oauth2callback"
	geminiCliUpstreamBaseURL           = "https://cloudcode-pa.googleapis.com"
	geminiCliGoogleAPIClient           = "google-genai-sdk/1.41.0 gl-node/v22.19.0"
	geminiCliUserAgent                 = "GeminiCLI/0.31.0/unknown (win32; x64)"
	geminiCliRequiredService           = "cloudaicompanion.googleapis.com"
	geminiCliInternalAPIVersion        = "v1internal"
	geminiCliAutoOnboardPollIntervalMs = 2000
	geminiCliAutoOnboardMaxAttempts    = 15
	geminiCliOnboardPollIntervalMs     = 5000
	geminiCliOnboardMaxAttempts        = 6
)

var geminiCliScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

func init() {
	RegisterProvider(&OAuthProviderDefinition{
		Metadata: ProviderMetadata{
			Provider:                    ProviderGeminiCli,
			Label:                       "Gemini CLI",
			Platform:                    "gemini-cli",
			Enabled:                     true,
			LoginType:                   "oauth",
			RequiresProjectId:           true,
			SupportsDirectAccountRouting: true,
			SupportsCloudValidation:     true,
			SupportsNativeProxy:         true,
		},
		Site: ProviderSiteConfig{
			Name:     "Google Gemini CLI OAuth",
			URL:      geminiCliUpstreamBaseURL,
			Platform: "gemini-cli",
		},
		Loopback: LoopbackConfig{
			Host:        "127.0.0.1",
			Port:        geminiCliLoopbackPort,
			Path:        geminiCliLoopbackPath,
			RedirectURI: geminiCliLoopbackRedirectURI,
		},
		BuildAuthorizationURL:   buildGeminiCliAuthorizationURL,
		ExchangeAuthorizationCode: exchangeGeminiCliAuthorizationCode,
		RefreshAccessToken:      refreshGeminiCliAccessToken,
		BuildProxyHeaders:       buildGeminiCliProxyHeaders,
	})
}

func requireGeminiCliOAuthConfig() (clientID, clientSecret string) {
	clientID = strings.TrimSpace(config.Get().GeminiCliClientId)
	if clientID == "" {
		panic("GEMINI_CLI_CLIENT_ID is not configured")
	}
	clientSecret = strings.TrimSpace(config.Get().GeminiCliClientSecret)
	if clientSecret == "" {
		panic("GEMINI_CLI_CLIENT_SECRET is not configured")
	}
	return
}

// ---- Auth URL (no PKCE) ----

func buildGeminiCliAuthorizationURL(ctx context.Context, input BuildAuthURLInput) (string, error) {
	clientID, _ := requireGeminiCliOAuthConfig()
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", input.RedirectURI)
	params.Set("response_type", "code")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("scope", strings.Join(geminiCliScopes, " "))
	params.Set("state", input.State)
	return geminiCliAuthURL + "?" + params.Encode(), nil
}

// ---- Token Exchange ----

type geminiOAuthTokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    interface{} `json:"expires_in"`
	Scope        string `json:"scope"`
	Expiry       string `json:"expiry"`
}

func parseGeminiExpiresAt(payload *geminiOAuthTokenPayload) int64 {
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

func postGeminiToken(form url.Values, proxyURL *string) (*TokenSet, error) {
	req, err := http.NewRequest("POST", geminiCliTokenURL, strings.NewReader(form.Encode()))
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

	var payload geminiOAuthTokenPayload
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("gemini token exchange returned invalid payload")
	}

	accessToken := strings.TrimSpace(payload.AccessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("gemini token exchange response missing access token")
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
		TokenExpiresAt: parseGeminiExpiresAt(&payload),
		ProviderData:   providerData,
	}, nil
}

func exchangeGeminiCliAuthorizationCode(ctx context.Context, input ExchangeCodeInput) (*TokenSet, error) {
	clientID, clientSecret := requireGeminiCliOAuthConfig()
	form := url.Values{}
	form.Set("code", input.Code)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("redirect_uri", input.RedirectURI)
	form.Set("grant_type", "authorization_code")

	token, err := postGeminiToken(form, input.ProxyURL)
	if err != nil {
		return nil, err
	}

	resolvedProjectID, err := ensureGeminiProjectAndOnboard(token.AccessToken, input.ProjectID, input.ProxyURL)
	if err != nil {
		return nil, err
	}

	if err := checkCloudAIAPIEnabled(token.AccessToken, resolvedProjectID, input.ProxyURL); err != nil {
		return nil, err
	}

	email, _ := fetchGeminiUserEmail(token.AccessToken, input.ProxyURL)

	return &TokenSet{
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
		TokenExpiresAt: token.TokenExpiresAt,
		Email:          email,
		AccountKey:     email,
		AccountID:      email,
		ProjectID:      resolvedProjectID,
		ProviderData:   token.ProviderData,
	}, nil
}

// ---- Token Refresh ----

func refreshGeminiCliAccessToken(ctx context.Context, input RefreshTokenInput) (*TokenSet, error) {
	clientID, clientSecret := requireGeminiCliOAuthConfig()
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", input.RefreshToken)

	token, err := postGeminiToken(form, input.ProxyURL)
	if err != nil {
		return nil, err
	}

	var nextProjectID string
	if input.OAuth != nil && input.OAuth.ProjectID != "" {
		nextProjectID = input.OAuth.ProjectID
	} else {
		var err error
		nextProjectID, err = ensureGeminiProjectAndOnboard(token.AccessToken, "", input.ProxyURL)
		if err != nil {
			return nil, err
		}
	}

	if err := checkCloudAIAPIEnabled(token.AccessToken, nextProjectID, input.ProxyURL); err != nil {
		return nil, err
	}

	email, _ := fetchGeminiUserEmail(token.AccessToken, input.ProxyURL)

	return &TokenSet{
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
		TokenExpiresAt: token.TokenExpiresAt,
		Email:          email,
		AccountKey:     email,
		AccountID:      email,
		ProjectID:      nextProjectID,
		ProviderData:   token.ProviderData,
	}, nil
}

// ---- Proxy Headers ----

func buildGeminiCliProxyHeaders(ctx context.Context, input ProxyHeaderInput) map[string]string {
	return map[string]string{
		"User-Agent":      geminiCliUserAgent,
		"X-Goog-Api-Client": geminiCliGoogleAPIClient,
	}
}

// ---- Onboarding ----

type geminiLoadCodeAssistPayload struct {
	CloudaicompanionProject interface{} `json:"cloudaicompanionProject"`
	AllowedTiers            interface{} `json:"allowedTiers"`
}

type geminiOnboardUserPayload struct {
	Done     interface{} `json:"done"`
	Response interface{} `json:"response"`
}

type geminiInternalMetadata struct {
	IDEType    string `json:"ideType"`
	Platform   string `json:"platform"`
	PluginType string `json:"pluginType"`
}

func buildGeminiCliMetadata() geminiInternalMetadata {
	return geminiInternalMetadata{
		IDEType:    "IDE_UNSPECIFIED",
		Platform:   "PLATFORM_UNSPECIFIED",
		PluginType: "GEMINI",
	}
}

func extractGeminiProjectID(value interface{}) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	if m, ok := value.(map[string]interface{}); ok {
		if id, ok := m["id"]; ok {
			if s, ok := id.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func extractGeminiDefaultTierID(payload *geminiLoadCodeAssistPayload) string {
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

func isGeminiFreeUserProject(requestedProjectID, tierID string) bool {
	return strings.HasPrefix(strings.TrimSpace(requestedProjectID), "gen-lang-client-") ||
		strings.EqualFold(strings.TrimSpace(tierID), "FREE") ||
		strings.EqualFold(strings.TrimSpace(tierID), "LEGACY")
}

func isSameGeminiProjectID(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func callGeminiCliInternalAPI(accessToken, method string, body map[string]interface{}, proxyURL *string) (map[string]interface{}, error) {
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/%s:%s", geminiCliUpstreamBaseURL, geminiCliInternalAPIVersion, method),
		bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", geminiCliUserAgent)
	req.Header.Set("X-Goog-Api-Client", geminiCliGoogleAPIClient)

	resp, err := doHTTP(req, proxyURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func fetchGcpProjects(accessToken string, proxyURL *string) ([]string, error) {
	req, err := http.NewRequest("GET", geminiCliProjectsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
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

	var payload struct {
		Projects []struct {
			ProjectID string `json:"projectId"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, err
	}

	var projects []string
	for _, p := range payload.Projects {
		if id := strings.TrimSpace(p.ProjectID); id != "" {
			projects = append(projects, id)
		}
	}
	return projects, nil
}

func fetchGeminiUserEmail(accessToken string, proxyURL *string) (string, error) {
	req, err := http.NewRequest("GET", geminiCliUserinfoURL, nil)
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

func checkCloudAIAPIEnabled(accessToken, projectID string, proxyURL *string) error {
	checkURL := fmt.Sprintf("%s/projects/%s/services/%s",
		geminiCliServiceUsageURL, url.PathEscape(projectID), url.PathEscape(geminiCliRequiredService))

	req, err := http.NewRequest("GET", checkURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", geminiCliUserAgent)
	req.Header.Set("X-Goog-Api-Client", geminiCliGoogleAPIClient)

	resp, err := doHTTP(req, proxyURL, nil)
	if err == nil {
		defer resp.Body.Close()
		var payload struct {
			State string `json:"state"`
		}
		respBody, _ := io.ReadAll(resp.Body)
		json.Unmarshal(respBody, &payload)
		if strings.EqualFold(strings.TrimSpace(payload.State), "ENABLED") {
			return nil
		}
	}

	// Enable the service
	enableURL := fmt.Sprintf("%s/projects/%s/services/%s:enable",
		geminiCliServiceUsageURL, url.PathEscape(projectID), url.PathEscape(geminiCliRequiredService))

	enableReq, err := http.NewRequest("POST", enableURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	enableReq.Header.Set("Authorization", "Bearer "+accessToken)
	enableReq.Header.Set("Accept", "application/json")
	enableReq.Header.Set("Content-Type", "application/json")
	enableReq.Header.Set("User-Agent", geminiCliUserAgent)
	enableReq.Header.Set("X-Goog-Api-Client", geminiCliGoogleAPIClient)

	enableResp, err := doHTTP(enableReq, proxyURL, nil)
	if err != nil {
		return err
	}
	defer enableResp.Body.Close()

	enableBody, _ := io.ReadAll(enableResp.Body)
	if enableResp.StatusCode == 200 {
		return nil
	}
	if enableResp.StatusCode == 400 && strings.Contains(strings.ToLower(string(enableBody)), "already enabled") {
		return nil
	}
	return fmt.Errorf("project activation required: HTTP %d", enableResp.StatusCode)
}

func performGeminiCliSetup(accessToken, requestedProjectID string, proxyURL *string) (string, error) {
	trimmedRequest := strings.TrimSpace(requestedProjectID)
	explicitProject := trimmedRequest != ""
	metadata := buildGeminiCliMetadata()

	loadBody := map[string]interface{}{"metadata": metadata}
	if explicitProject {
		loadBody["cloudaicompanionProject"] = trimmedRequest
	}

	loadResp, err := callGeminiCliInternalAPI(accessToken, "loadCodeAssist", loadBody, proxyURL)
	if err != nil {
		return "", err
	}

	loadPayload := &geminiLoadCodeAssistPayload{}
	if v, ok := loadResp["cloudaicompanionProject"]; ok {
		loadPayload.CloudaicompanionProject = v
	}
	if v, ok := loadResp["allowedTiers"]; ok {
		loadPayload.AllowedTiers = v
	}

	tierID := extractGeminiDefaultTierID(loadPayload)
	projectID := trimmedRequest
	if projectID == "" {
		projectID = extractGeminiProjectID(loadPayload.CloudaicompanionProject)
	}

	if projectID == "" {
		for attempt := 0; attempt < geminiCliAutoOnboardMaxAttempts; attempt++ {
			onboardResp, err := callGeminiCliInternalAPI(accessToken, "onboardUser", map[string]interface{}{
				"tierId":   tierID,
				"metadata": metadata,
			}, proxyURL)
			if err != nil {
				return "", err
			}
			if done, _ := onboardResp["done"].(bool); done {
				if resp, ok := onboardResp["response"].(map[string]interface{}); ok {
					projectID = extractGeminiProjectID(resp["cloudaicompanionProject"])
				}
				break
			}
			if attempt+1 < geminiCliAutoOnboardMaxAttempts {
				time.Sleep(time.Duration(geminiCliAutoOnboardPollIntervalMs) * time.Millisecond)
			}
		}
	}

	if projectID == "" {
		return "", fmt.Errorf("gemini cli: project selection required")
	}

	finalProjectID := projectID
	for attempt := 0; attempt < geminiCliOnboardMaxAttempts; attempt++ {
		onboardResp, err := callGeminiCliInternalAPI(accessToken, "onboardUser", map[string]interface{}{
			"tierId":                   tierID,
			"metadata":                 metadata,
			"cloudaicompanionProject": projectID,
		}, proxyURL)
		if err != nil {
			return "", err
		}
		if done, _ := onboardResp["done"].(bool); done {
			responseProjectID := ""
			if resp, ok := onboardResp["response"].(map[string]interface{}); ok {
				responseProjectID = extractGeminiProjectID(resp["cloudaicompanionProject"])
			}
			if responseProjectID != "" {
				if explicitProject && !isSameGeminiProjectID(responseProjectID, projectID) {
					if isGeminiFreeUserProject(projectID, tierID) {
						finalProjectID = responseProjectID
					} else {
						finalProjectID = projectID
					}
				} else {
					finalProjectID = responseProjectID
				}
			}
			if finalProjectID == "" {
				finalProjectID = projectID
			}
			return finalProjectID, nil
		}
		if attempt+1 < geminiCliOnboardMaxAttempts {
			time.Sleep(time.Duration(geminiCliOnboardPollIntervalMs) * time.Millisecond)
		}
	}

	if finalProjectID != "" {
		return finalProjectID, nil
	}
	return "", fmt.Errorf("gemini cli: onboarding timed out")
}

func ensureGeminiProjectAndOnboard(accessToken, requestedProjectID string, proxyURL *string) (string, error) {
	explicitProject := strings.TrimSpace(requestedProjectID)
	if explicitProject != "" {
		return performGeminiCliSetup(accessToken, explicitProject, proxyURL)
	}

	projects, err := fetchGcpProjects(accessToken, proxyURL)
	if err != nil {
		return "", err
	}
	if len(projects) == 0 {
		return "", fmt.Errorf("no Google Cloud projects available for this account")
	}
	return performGeminiCliSetup(accessToken, projects[0], proxyURL)
}

func parseInt64Safe(s string) (int64, error) {
	n := int64(0)
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
