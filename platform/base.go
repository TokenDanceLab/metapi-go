package platform

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// BaseAdapter provides default implementations for PlatformAdapter methods.
// Concrete adapters embed this and override specific methods as needed.
type BaseAdapter struct {
	name string
}

// NewBaseAdapter creates a BaseAdapter with the given platform name.
func NewBaseAdapter(name string) *BaseAdapter {
	return &BaseAdapter{name: name}
}

// PlatformName returns the platform identifier.
func (b *BaseAdapter) PlatformName() string {
	return b.name
}

// Detect must be implemented by concrete adapters.
func (b *BaseAdapter) Detect(ctx context.Context, url string) (bool, error) {
	return false, fmt.Errorf("detect not implemented for %s", b.name)
}

// Login provides a default login implementation: POST /api/user/login.
func (b *BaseAdapter) Login(ctx context.Context, url, username, password string, platformUserId *int, proxy *ProxyConfig) (*LoginResult, error) {
	body := map[string]string{"username": username, "password": password}
	resp, err := fetchJSON(ctx, url+"/api/user/login", http.MethodPost, body, nil, proxy)
	if err != nil {
		return &LoginResult{Success: false, Message: err.Error()}, nil
	}

	success, _ := getBool(resp, "success")
	if !success {
		msg, _ := getString(resp, "message")
		if msg == "" {
			msg = "login failed"
		}
		return &LoginResult{Success: false, Message: msg}, nil
	}

	// Extract token from various possible fields
	data, _ := getMap(resp, "data")
	token := extractLoginToken(resp, data)

	return &LoginResult{
		Success:     true,
		AccessToken: token,
		Username:    username,
	}, nil
}

// GetUserInfo provides a default implementation: GET /api/user/self with Bearer auth.
func (b *BaseAdapter) GetUserInfo(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*UserInfo, error) {
	headers := authBearerHeaders(accessToken)
	resp, err := fetchJSON(ctx, url+"/api/user/self", http.MethodGet, nil, headers, proxy)
	if err != nil {
		return nil, nil
	}

	success, _ := getBool(resp, "success")
	if !success {
		return nil, nil
	}

	data, ok := getMap(resp, "data")
	if !ok {
		return nil, nil
	}

	username, _ := getString(data, "username")
	displayName, _ := getString(data, "display_name")
	if username == "" && displayName != "" {
		username = displayName
	}

	email, _ := getString(data, "email")
	role := getIntPtr(data, "role")

	return &UserInfo{
		Username:    username,
		DisplayName: displayName,
		Email:       email,
		Role:        role,
	}, nil
}

// VerifyToken provides default token verification (session + apikey paths).
func (b *BaseAdapter) VerifyToken(ctx context.Context, url, token string, platformUserId *int, proxy *ProxyConfig) (*TokenVerifyResult, error) {
	// 1. Try as session/access token
	userInfo, err := b.GetUserInfo(ctx, url, token, platformUserId, proxy)
	if err == nil && userInfo != nil {
		var balance *BalanceInfo
		balanceInfo, err := b.GetBalance(ctx, url, token, platformUserId, proxy)
		if err == nil {
			balance = balanceInfo
		}

		var apiToken *string
		apiT, err := b.GetAPIToken(ctx, url, token, platformUserId, proxy)
		if err == nil && apiT != nil {
			apiToken = apiT
		}

		apiTokenStr := ""
		if apiToken != nil {
			apiTokenStr = *apiToken
		}
		return &TokenVerifyResult{
			TokenType: "session",
			UserInfo:  userInfo,
			Balance:   balance,
			APIToken:  apiTokenStr,
		}, nil
	}

	// 2. Try as API key (via /v1/models)
	models, err := b.GetModels(ctx, url, token, platformUserId, proxy)
	if err == nil && len(models) > 0 {
		return &TokenVerifyResult{
			TokenType: "apikey",
			Models:    models,
		}, nil
	}

	return &TokenVerifyResult{TokenType: "unknown"}, nil
}

// Checkin returns unsupported by default.
func (b *BaseAdapter) Checkin(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error) {
	return &CheckinResult{Success: false, Message: "checkin not implemented for " + b.name}, nil
}

// GetBalance returns zero balance by default.
func (b *BaseAdapter) GetBalance(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error) {
	return &BalanceInfo{Balance: 0, Used: 0, Quota: 0}, nil
}

// GetModels returns empty list by default.
func (b *BaseAdapter) GetModels(ctx context.Context, url, token string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	return []string{}, nil
}

// GetAPIToken returns nil by default.
func (b *BaseAdapter) GetAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*string, error) {
	return nil, nil
}

// GetAPITokens returns a single-item list if GetAPIToken finds one.
func (b *BaseAdapter) GetAPITokens(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]ApiTokenInfo, error) {
	token, err := b.GetAPIToken(ctx, url, accessToken, platformUserId, proxy)
	if err != nil || token == nil {
		return []ApiTokenInfo{}, nil
	}
	return []ApiTokenInfo{
		{Name: "default", Key: *token, Enabled: true, TokenGroup: "default"},
	}, nil
}

// CreateAPIToken returns false by default.
func (b *BaseAdapter) CreateAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, options *CreateAPITokenOptions, proxy *ProxyConfig) (bool, error) {
	return false, nil
}

// DeleteAPIToken returns nil (no-op) by default.
func (b *BaseAdapter) DeleteAPIToken(ctx context.Context, url, accessToken string, tokenKey string, platformUserId *int, proxy *ProxyConfig) error {
	return nil
}

// GetSiteAnnouncements returns empty list by default.
func (b *BaseAdapter) GetSiteAnnouncements(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]SiteAnnouncement, error) {
	return []SiteAnnouncement{}, nil
}

// GetUserGroups returns ["default"] by default.
func (b *BaseAdapter) GetUserGroups(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	return []string{"default"}, nil
}

// --- HTTP helpers ---

// fetchJSON performs an HTTP request and parses the JSON response body into a map.
func fetchJSON(ctx context.Context, url, method string, body interface{}, headers map[string]string, proxy *ProxyConfig) (map[string]interface{}, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(b))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if headers == nil {
		headers = make(map[string]string)
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := DoWithProxy(ctx, req, proxy)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := extractResponseMessageFromBytes(respBody)
		if msg == "" {
			msg = string(respBody)
			if len(msg) > 200 {
				msg = msg[:200]
			}
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	return result, nil
}

// fetchJSONRaw performs an HTTP request and returns the raw response body bytes.
// Does NOT check response status code — caller handles it.
func fetchJSONRaw(ctx context.Context, url, method string, body interface{}, headers map[string]string, proxy *ProxyConfig) (map[string]interface{}, []byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(b))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("create request: %w", err)
	}

	if headers == nil {
		headers = make(map[string]string)
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := DoWithProxy(ctx, req, proxy)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("read body: %w", err)
	}

	var parsed map[string]interface{}
	_ = json.Unmarshal(respBody, &parsed)

	return parsed, respBody, resp.StatusCode, nil
}

// fetchJSONRawWithCookie performs an HTTP request with automatic Set-Cookie tracking.
func fetchJSONRawWithCookie(ctx context.Context, url, method string, body interface{}, headers map[string]string, cookieHeader string, proxy *ProxyConfig) (map[string]interface{}, string, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, "", fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(b))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	if headers == nil {
		headers = make(map[string]string)
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}

	resp, err := DoWithProxy(ctx, req, proxy)
	if err != nil {
		return nil, "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	// Track Set-Cookie
	newCookie := mergeSetCookie(cookieHeader, resp.Header["Set-Cookie"])

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newCookie, fmt.Errorf("read body: %w", err)
	}

	var parsed map[string]interface{}
	_ = json.Unmarshal(respBody, &parsed)

	return parsed, newCookie, nil
}

func fetchText(ctx context.Context, url string, proxy *ProxyConfig) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")

	resp, err := DoWithProxy(ctx, req, proxy)
	if err != nil {
		return "", "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return string(body), contentType, nil
}

// --- JSON helpers ---

func getString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getBool(m map[string]interface{}, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func getFloat(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

func getIntPtr(m map[string]interface{}, key string) *int {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		i := int(n)
		return &i
	case int:
		return &n
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			val := int(i)
			return &val
		}
	}
	return nil
}

func getMap(m map[string]interface{}, key string) (map[string]interface{}, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	mm, ok := v.(map[string]interface{})
	return mm, ok
}

// --- Auth helpers ---

func authBearerHeaders(token string) map[string]string {
	t := strings.TrimPrefix(strings.TrimSpace(token), "Bearer ")
	return map[string]string{"Authorization": "Bearer " + t}
}

func stripBearerPrefix(token string) string {
	t := strings.TrimSpace(token)
	t = strings.TrimPrefix(t, "Bearer ")
	return strings.TrimSpace(t)
}

// --- Cookie helpers ---

func buildCookieCandidates(token string) []string {
	t := strings.TrimSpace(token)
	t = strings.TrimPrefix(t, "Bearer ")
	t = strings.TrimSpace(t)
	if t == "" {
		return nil
	}

	candidates := []string{}
	seen := map[string]bool{}

	if strings.Contains(t, "=") {
		candidates = append(candidates, t)
		seen[t] = true
	}

	s := "session=" + t
	if !seen[s] {
		candidates = append(candidates, s)
		seen[s] = true
	}

	tt := "token=" + t
	if !seen[tt] {
		candidates = append(candidates, tt)
	}
	return candidates
}

func mergeSetCookie(existing string, setCookies []string) string {
	merged := existing
	for _, raw := range setCookies {
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, ";", 2)
		pair := strings.TrimSpace(parts[0])
		eqIdx := strings.Index(pair, "=")
		if eqIdx <= 0 {
			continue
		}
		name := strings.TrimSpace(pair[:eqIdx])
		value := pair[eqIdx+1:]
		merged = upsertCookie(merged, name, value)
	}
	return merged
}

func upsertCookie(cookieHeader, name, value string) string {
	parts := strings.Split(cookieHeader, ";")
	filtered := make([]string, 0, len(parts))
	replaced := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eqIdx := strings.Index(part, "=")
		if eqIdx < 0 {
			filtered = append(filtered, part)
			continue
		}
		key := strings.TrimSpace(part[:eqIdx])
		if key == name {
			replaced = true
			filtered = append(filtered, name+"="+value)
		} else {
			filtered = append(filtered, part)
		}
	}
	if !replaced {
		filtered = append(filtered, name+"="+value)
	}
	return strings.Join(filtered, "; ")
}

func hasUsableSessionCookie(cookieHeader string) bool {
	if cookieHeader == "" {
		return false
	}
	ignored := map[string]bool{"acw_tc": true, "acw_sc__v2": true, "cdn_sec_tc": true}
	parts := strings.Split(cookieHeader, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		eqIdx := strings.Index(part, "=")
		if eqIdx <= 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(part[:eqIdx]))
		if name == "" || ignored[name] {
			continue
		}
		lower := strings.ToLower(name)
		if lower == "session" || lower == "token" || lower == "auth_token" ||
			lower == "access_token" || lower == "jwt" || lower == "jwt_token" ||
			strings.Contains(lower, "session") || strings.Contains(lower, "token") ||
			strings.Contains(lower, "auth") {
			return true
		}
	}
	return false
}

// --- Login helpers ---

func extractLoginToken(resp, data map[string]interface{}) string {
	candidates := []interface{}{
		data,
		resp["token"],
		resp["accessToken"],
		resp["access_token"],
	}
	if data != nil {
		candidates = append(candidates, data["token"], data["accessToken"], data["access_token"])
	}
	for _, c := range candidates {
		if s, ok := c.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// --- Message helpers ---

func extractResponseMessage(payload map[string]interface{}) string {
	if msg, ok := getString(payload, "message"); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	if errObj, ok := getMap(payload, "error"); ok {
		if msg, ok := getString(errObj, "message"); ok && strings.TrimSpace(msg) != "" {
			return strings.TrimSpace(msg)
		}
	}
	if msg, ok := getString(payload, "msg"); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	return ""
}

func extractResponseMessageFromBytes(body []byte) string {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	return extractResponseMessage(m)
}

// --- Notice helpers ---

func buildNoticeSourceKey(content string) string {
	normalized := strings.TrimSpace(content)
	hash := sha1.Sum([]byte(normalized))
	return fmt.Sprintf("notice:%x", hash[:])
}

// --- Token helpers ---

func normalizeTokenKeyForCompare(key string) string {
	return stripBearerPrefix(key)
}

func parseTokenItemsFromMap(resp map[string]interface{}) []map[string]interface{} {
	// Try multiple response formats
	sources := [][]interface{}{}
	if data, ok := resp["data"].([]interface{}); ok {
		sources = append(sources, data)
	}
	if data, ok := getMap(resp, "data"); ok {
		if items, ok := data["items"].([]interface{}); ok {
			sources = append(sources, items)
		}
		if items, ok := data["data"].([]interface{}); ok {
			sources = append(sources, items)
		}
		if items, ok := data["list"].([]interface{}); ok {
			sources = append(sources, items)
		}
	}
	if items, ok := resp["items"].([]interface{}); ok {
		sources = append(sources, items)
	}
	if items, ok := resp["list"].([]interface{}); ok {
		sources = append(sources, items)
	}

	for _, src := range sources {
		items := make([]map[string]interface{}, 0, len(src))
		for _, v := range src {
			if m, ok := v.(map[string]interface{}); ok {
				items = append(items, m)
			}
		}
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

func normalizeTokenItems(items []map[string]interface{}) []ApiTokenInfo {
	result := make([]ApiTokenInfo, 0, len(items))
	for i, item := range items {
		key, _ := getString(item, "key")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		name, _ := getString(item, "name")
		name = strings.TrimSpace(name)
		if name == "" {
			if i == 0 {
				name = "default"
			} else {
				name = fmt.Sprintf("token-%d", i+1)
			}
		}

		// Extract group from multiple possible fields
		group := ""
		if g, ok := getString(item, "group"); ok {
			group = strings.TrimSpace(g)
		}
		if group == "" {
			if g, ok := getString(item, "group_name"); ok {
				group = strings.TrimSpace(g)
			}
		}
		if group == "" {
			if g, ok := getString(item, "token_group"); ok {
				group = strings.TrimSpace(g)
			}
		}

		enabled := true
		if status, ok := getFloat(item, "status"); ok {
			enabled = status == 1
		}

		info := ApiTokenInfo{
			Name:    name,
			Key:     key,
			Enabled: enabled,
		}
		if group != "" {
			info.TokenGroup = group
		}
		result = append(result, info)
	}
	return result
}

// Token list: find the first enabled token key, or the first token key.
func findFirstEnabledToken(tokens []ApiTokenInfo) *string {
	for _, t := range tokens {
		if t.Enabled {
			s := t.Key
			return &s
		}
	}
	if len(tokens) > 0 {
		s := tokens[0].Key
		return &s
	}
	return nil
}

// Pick token ID from token list by matching key.
func pickTokenID(items []map[string]interface{}, targetKey string) *int {
	for _, item := range items {
		key, _ := getString(item, "key")
		if normalizeTokenKeyForCompare(key) == targetKey {
			if id := getIntPtr(item, "id"); id != nil {
				return id
			}
		}
	}
	return nil
}
