package platform

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

// StandardAdapter is the base for standard API providers (OpenAI, Claude, Gemini, CliProxyApi).
// It provides sane "not supported" defaults for login/checkin/getBalance/getUserInfo.
type StandardAdapter struct {
	*BaseAdapter
	LoginUnsupportedMessage   string
	CheckinUnsupportedMessage string
}

// NewStandardAdapter creates a StandardAdapter.
func NewStandardAdapter(name string) *StandardAdapter {
	return &StandardAdapter{
		BaseAdapter:               NewBaseAdapter(name),
		LoginUnsupportedMessage:   "login endpoint not supported",
		CheckinUnsupportedMessage: "checkin endpoint not supported",
	}
}

// Login returns unsupported.
func (s *StandardAdapter) Login(ctx context.Context, url, username, password string, platformUserId *int, proxy *ProxyConfig) (*LoginResult, error) {
	return &LoginResult{Success: false, Message: s.LoginUnsupportedMessage}, nil
}

// GetUserInfo returns nil.
func (s *StandardAdapter) GetUserInfo(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*UserInfo, error) {
	return nil, nil
}

// Checkin returns unsupported.
func (s *StandardAdapter) Checkin(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error) {
	return &CheckinResult{Success: false, Message: s.CheckinUnsupportedMessage}, nil
}

// GetBalance returns zero balance.
func (s *StandardAdapter) GetBalance(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error) {
	return &BalanceInfo{Balance: 0, Used: 0, Quota: 0}, nil
}

// fetchModelsFromStandardEndpoint is a helper for OpenAI-compatible /v1/models endpoints.
func (s *StandardAdapter) fetchModelsFromStandardEndpoint(ctx context.Context, baseURL string, headers map[string]string, proxy *ProxyConfig) ([]string, error) {
	normalized := normalizePlatformBaseURL(baseURL)
	modelsURL := resolveVersionedModelsURL(normalized)

	resp, err := fetchJSON(ctx, modelsURL, "GET", nil, headers, proxy)
	if err != nil {
		return nil, err
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		return nil, err
	}

	models := make([]string, 0, len(data))
	for _, item := range data {
		if m, ok := item.(map[string]interface{}); ok {
			if id, ok := m["id"].(string); ok && strings.TrimSpace(id) != "" {
				models = append(models, strings.TrimSpace(id))
			}
		}
	}
	return models, nil
}

// --- URL normalization helpers ---

func normalizePlatformBaseURL(baseURL string) string {
	u := baseURL
	for strings.HasSuffix(u, "/") {
		u = u[:len(u)-1]
	}
	return u
}

var versionedPathRE = regexp.MustCompile(`/v\d+(?:\.\d+)?(?:beta)?$`)

func resolveVersionedModelsURL(baseURL string) string {
	normalized := normalizePlatformBaseURL(baseURL)
	if versionedPathRE.MatchString(normalized) {
		return normalized + "/models"
	}
	return normalized + "/v1/models"
}

func normalizeBaseURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return u
	}
	// Try to parse as URL
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		// Just strip trailing slashes
		for strings.HasSuffix(u, "/") {
			u = u[:len(u)-1]
		}
		return u
	}
	// Return origin (protocol + host)
	result := parsed.Scheme + "://" + parsed.Host
	return result
}

func normalizeURLForDetection(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return u
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return strings.TrimSuffix(strings.TrimSuffix(u, "/"), "\\")
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	return parsed.Scheme + "://" + parsed.Host + path
}

func normalizeURLProtocol(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return u
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return strings.TrimSuffix(strings.TrimSuffix(raw, "/"), "\\")
	}
	return parsed.Scheme + "://" + parsed.Host
}
