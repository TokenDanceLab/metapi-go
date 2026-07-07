package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Sub2ApiAdapter handles Sub2API platforms (JWT auth, {code, message, data} envelope).
// Directly extends BaseAdapter. Does NOT support login or checkin.
type Sub2ApiAdapter struct {
	*BaseAdapter
}

func init() {
	Register(&Sub2ApiAdapter{BaseAdapter: NewBaseAdapter("sub2api")})
}

// Detect uses a multi-path approach: URL keyword, /api/v1/auth/me probe, /v1/models probe, root title.
func (s *Sub2ApiAdapter) Detect(ctx context.Context, url string) (bool, error) {
	lower := strings.ToLower(url)
	if strings.Contains(lower, "sub2api") {
		return true, nil
	}

	base := normalizeBaseURL(url)
	if base == "" {
		return false, nil
	}

	// Probe /api/v1/auth/me
	if s.matchSub2ApiErrorEnvelope(ctx, base+"/api/v1/auth/me") {
		return true, nil
	}

	// Probe /v1/models
	if s.matchSub2ApiErrorEnvelope(ctx, base+"/v1/models") {
		return true, nil
	}

	// Fallback: check root HTML title
	body, ct, err := fetchText(ctx, base+"/", nil)
	if err != nil {
		return false, nil
	}
	if !strings.Contains(strings.ToLower(ct), "text/html") {
		return false, nil
	}

	return regexp.MustCompile(`(?i)<title>\s*sub2api\b`).MatchString(body), nil
}

func (s *Sub2ApiAdapter) matchSub2ApiErrorEnvelope(ctx context.Context, url string) bool {
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx2, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := DoWithProxy(ctx2, req, nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "application/json") {
		return false
	}

	respBody, err := readPlatformResponseBody(resp.Body, platformJSONResponseBodyLimit)
	if err != nil {
		return false
	}

	var body map[string]interface{}
	if err := json.Unmarshal(respBody, &body); err != nil {
		return false
	}

	code, _ := body["code"]
	switch c := code.(type) {
	case string:
		upper := strings.ToUpper(strings.TrimSpace(c))
		if upper == "UNAUTHORIZED" || upper == "API_KEY_REQUIRED" {
			return true
		}
	case float64:
		if c == 0 {
			_, hasData := body["data"]
			return hasData
		}
	}

	msg, _ := getString(body, "message")
	msgLower := strings.ToLower(msg)
	if strings.Contains(msgLower, "authorization header is required") ||
		strings.Contains(msgLower, "api key is required") {
		return true
	}

	return false
}

// Login: JWT-only, always unsupported.
func (s *Sub2ApiAdapter) Login(ctx context.Context, url, username, password string, platformUserId *int, proxy *ProxyConfig) (*LoginResult, error) {
	return &LoginResult{Success: false, Message: "Sub2API uses JWT authentication; login is not supported"}, nil
}

// GetUserInfo: GET /api/v1/auth/me.
func (s *Sub2ApiAdapter) GetUserInfo(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*UserInfo, error) {
	user, err := s.fetchAuthMe(ctx, normalizeBaseURL(baseURL), accessToken, proxy)
	if err != nil {
		return nil, nil
	}

	displayName := user.username
	if displayName == "" && user.email != "" {
		if idx := strings.Index(user.email, "@"); idx > 0 {
			displayName = user.email[:idx]
		} else {
			displayName = user.email
		}
	}

	result := &UserInfo{
		Username:    displayName,
		DisplayName: displayName,
	}
	if user.email != "" {
		result.Email = user.email
	}
	return result, nil
}

// Checkin: not supported.
func (s *Sub2ApiAdapter) Checkin(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error) {
	return &CheckinResult{Success: false, Message: "Check-in is not supported by Sub2API"}, nil
}

// GetBalance: USD balance from /api/v1/auth/me, converted to quota, plus subscription summary.
func (s *Sub2ApiAdapter) GetBalance(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error) {
	normalized := normalizeBaseURL(baseURL)

	type result struct {
		user *sub2apiUser
		subs *SubscriptionSummary
		err  error
	}

	userCh := make(chan result, 1)
	subsCh := make(chan result, 1)

	go func() {
		u, err := s.fetchAuthMe(ctx, normalized, accessToken, proxy)
		userCh <- result{user: u, err: err}
	}()
	go func() {
		subs, _ := s.fetchSubscriptionSummary(ctx, normalized, accessToken, proxy)
		subsCh <- result{subs: subs}
	}()

	userRes := <-userCh
	subsRes := <-subsCh

	if userRes.err != nil {
		return &BalanceInfo{}, nil
	}

	quotaValue := s.usdToQuota(userRes.user.balance)
	quotaUSD := quotaValue / 500000

	return &BalanceInfo{
		Balance:             quotaUSD,
		Used:                0,
		Quota:               quotaUSD,
		SubscriptionSummary: subsRes.subs,
	}, nil
}

// GetModels: standard OpenAI-compat endpoints, with API key discovery fallback.
func (s *Sub2ApiAdapter) GetModels(ctx context.Context, baseURL, apiToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	normalized := normalizeBaseURL(baseURL)
	managementBase := s.resolveManagementBaseURL(normalized)

	directModels := s.fetchModelsByToken(ctx, normalized, apiToken, proxy)
	if len(directModels) > 0 {
		return directModels, nil
	}

	// Session JWT cannot access /v1/models directly; discover a user key first
	discoveredToken, _ := s.GetAPIToken(ctx, managementBase, apiToken, platformUserId, proxy)
	if discoveredToken == nil {
		return []string{}, nil
	}
	if normalizeTokenKeyForCompare(*discoveredToken) == normalizeTokenKeyForCompare(apiToken) {
		return []string{}, nil
	}
	return s.fetchModelsByToken(ctx, normalized, *discoveredToken, proxy), nil
}

// GetAPITokens: GET /api/v1/keys?page=1&page_size=100 + /api/v1/api-keys?page=1&page_size=100.
func (s *Sub2ApiAdapter) GetAPITokens(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]ApiTokenInfo, error) {
	items, err := s.listAPIKeys(ctx, normalizeBaseURL(baseURL), accessToken, proxy)
	if err != nil {
		return []ApiTokenInfo{}, nil
	}

	result := make([]ApiTokenInfo, 0, len(items))
	for _, item := range items {
		info := ApiTokenInfo{
			Name:    item.name,
			Key:     item.key,
			Enabled: item.enabled,
		}
		if item.tokenGroup != "" {
			info.TokenGroup = item.tokenGroup
		}
		result = append(result, info)
	}
	return result, nil
}

// GetAPIToken: returns first enabled token.
func (s *Sub2ApiAdapter) GetAPIToken(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) (*string, error) {
	tokens, err := s.GetAPITokens(ctx, baseURL, accessToken, platformUserId, proxy)
	if err != nil {
		return nil, nil
	}
	for _, t := range tokens {
		if t.Enabled {
			k := t.Key
			return &k, nil
		}
	}
	if len(tokens) > 0 {
		k := tokens[0].Key
		return &k, nil
	}
	return nil, nil
}

// GetUserGroups: 5 endpoint fallback + key-based inference.
func (s *Sub2ApiAdapter) GetUserGroups(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	normalized := normalizeBaseURL(baseURL)

	directGroups := s.listGroups(ctx, normalized, accessToken, proxy)
	if len(directGroups) > 0 {
		return directGroups, nil
	}

	inferredFromKeys := s.inferGroupsFromKeys(ctx, normalized, accessToken, proxy)
	if len(inferredFromKeys) > 0 {
		return inferredFromKeys, nil
	}

	return []string{"default"}, nil
}

// CreateAPIToken: POST /api/v1/keys + POST /api/v1/api-keys.
func (s *Sub2ApiAdapter) CreateAPIToken(ctx context.Context, baseURL, accessToken string, platformUserId *int, options *CreateAPITokenOptions, proxy *ProxyConfig) (bool, error) {
	normalized := normalizeBaseURL(baseURL)

	payload := map[string]interface{}{}
	if options != nil && strings.TrimSpace(options.Name) != "" {
		payload["name"] = strings.TrimSpace(options.Name)
	} else {
		payload["name"] = "metapi"
	}

	if options != nil {
		if groupID, err := parseGroupID(options.Group); err == nil && groupID > 0 {
			payload["group_id"] = groupID
		}
		if expiresInDays := s.resolveExpiresInDays(options.ExpiredTime); expiresInDays > 0 {
			payload["expires_in_days"] = expiresInDays
		}
		if !options.UnlimitedQuota && options.RemainQuota > 0 {
			payload["quota"] = math.Max(0, options.RemainQuota)
		}
	}

	headers := s.buildAuthHeader(accessToken)
	endpoints := []string{"/api/v1/keys", "/api/v1/api-keys"}
	for _, endpoint := range endpoints {
		resp, err := fetchJSON(ctx, normalized+endpoint, "POST", payload, headers, proxy)
		if err != nil {
			continue
		}
		if err := s.parseSub2ApiEnvelope(resp, endpoint); err == nil {
			return true, nil
		}
	}

	return false, nil
}

// DeleteAPIToken: list -> find key -> DELETE /api/v1/keys/{id} + /api/v1/api-keys/{id}.
func (s *Sub2ApiAdapter) DeleteAPIToken(ctx context.Context, baseURL, accessToken, tokenKey string, platformUserId *int, proxy *ProxyConfig) error {
	targetKey := normalizeTokenKeyForCompare(tokenKey)
	if targetKey == "" {
		return nil
	}

	normalized := normalizeBaseURL(baseURL)
	items, err := s.listAPIKeys(ctx, normalized, accessToken, proxy)
	if err != nil {
		return nil
	}

	var tokenID *int
	for _, item := range items {
		if normalizeTokenKeyForCompare(item.key) == targetKey {
			id := item.id
			tokenID = &id
			break
		}
	}

	if tokenID == nil {
		return nil // Already absent, safe
	}

	headers := s.buildAuthHeader(accessToken)
	endpoints := []string{
		fmt.Sprintf("/api/v1/keys/%d", *tokenID),
		fmt.Sprintf("/api/v1/api-keys/%d", *tokenID),
	}
	for _, endpoint := range endpoints {
		resp, err := fetchJSON(ctx, normalized+endpoint, "DELETE", nil, headers, proxy)
		if err != nil {
			continue
		}
		if err := s.parseSub2ApiEnvelope(resp, endpoint); err == nil {
			return nil
		}
	}

	return nil
}

// GetSiteAnnouncements: GET /api/v1/announcements?page=1&page_size=100.
func (s *Sub2ApiAdapter) GetSiteAnnouncements(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]SiteAnnouncement, error) {
	endpoint := "/api/v1/announcements?page=1&page_size=100"
	headers := s.buildAuthHeader(accessToken)
	resp, err := fetchJSON(ctx, normalizeBaseURL(baseURL)+endpoint, "GET", nil, headers, proxy)
	if err != nil {
		return []SiteAnnouncement{}, nil
	}

	data, err := s.parseSub2ApiEnvelopeRaw(resp, endpoint)
	if err != nil {
		return []SiteAnnouncement{}, nil
	}

	var rawItems []interface{}
	switch v := data.(type) {
	case []interface{}:
		rawItems = v
	case map[string]interface{}:
		if items, ok := v["items"].([]interface{}); ok {
			rawItems = items
		}
	}

	rows := make([]SiteAnnouncement, 0, len(rawItems))
	for _, item := range rawItems {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		idFloat, ok := m["id"].(float64)
		if !ok || int(idFloat) <= 0 {
			continue
		}
		id := int(idFloat)

		title, _ := getString(m, "title")
		content, _ := getString(m, "content")
		if title == "" && content == "" {
			continue
		}
		if title == "" {
			title = fmt.Sprintf("Announcement %d", id)
		}
		if content == "" {
			content = title
		}

		ann := SiteAnnouncement{
			SourceKey: fmt.Sprintf("announcement:%d", id),
			Title:     title,
			Content:   content,
			Level:     "info",
		}
		if v, ok := getString(m, "starts_at"); ok {
			ann.StartsAt = v
		}
		if v, ok := getString(m, "ends_at"); ok {
			ann.EndsAt = v
		}
		if v, ok := getString(m, "created_at"); ok {
			ann.UpstreamCreatedAt = v
		}
		if v, ok := getString(m, "updated_at"); ok {
			ann.UpstreamUpdatedAt = v
		}
		rows = append(rows, ann)
	}
	return rows, nil
}

// --- Internal types and helpers ---

type sub2apiUser struct {
	id       int
	username string
	email    string
	balance  float64
}

type sub2apiKeyItem struct {
	id         int
	key        string
	name       string
	enabled    bool
	tokenGroup string
}

func (s *Sub2ApiAdapter) buildAuthHeader(accessToken string) map[string]string {
	return authBearerHeaders(accessToken)
}

func (s *Sub2ApiAdapter) parseSub2ApiEnvelope(body map[string]interface{}, endpoint string) error {
	code, ok := body["code"]
	if !ok {
		return fmt.Errorf("Invalid response format from %s", endpoint)
	}

	codeFloat, ok := code.(float64)
	if !ok {
		return fmt.Errorf("Invalid response format from %s", endpoint)
	}

	if codeFloat != 0 {
		msg, _ := getString(body, "message")
		if msg == "" {
			msg = fmt.Sprintf("Error code %v from %s", codeFloat, endpoint)
		}
		return fmt.Errorf("%s", msg)
	}

	if _, ok := body["data"]; !ok {
		return fmt.Errorf("Missing data in response from %s", endpoint)
	}
	return nil
}

func (s *Sub2ApiAdapter) parseSub2ApiEnvelopeRaw(body map[string]interface{}, endpoint string) (interface{}, error) {
	if err := s.parseSub2ApiEnvelope(body, endpoint); err != nil {
		return nil, err
	}
	return body["data"], nil
}

func (s *Sub2ApiAdapter) fetchAuthMe(ctx context.Context, baseURL, accessToken string, proxy *ProxyConfig) (*sub2apiUser, error) {
	endpoint := "/api/v1/auth/me"
	headers := s.buildAuthHeader(accessToken)
	resp, err := fetchJSON(ctx, baseURL+endpoint, "GET", nil, headers, proxy)
	if err != nil {
		return nil, err
	}

	rawData, err := s.parseSub2ApiEnvelopeRaw(resp, endpoint)
	if err != nil {
		return nil, err
	}
	data, ok := rawData.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Invalid response from %s", endpoint)
	}

	idFloat, ok := data["id"].(float64)
	if !ok || idFloat <= 0 {
		return nil, fmt.Errorf("Invalid user ID in response from %s", endpoint)
	}

	username, _ := getString(data, "username")
	email, _ := getString(data, "email")
	balance, _ := getFloat(data, "balance")

	return &sub2apiUser{
		id:       int(idFloat),
		username: username,
		email:    email,
		balance:  balance,
	}, nil
}

func (s *Sub2ApiAdapter) usdToQuota(balanceUsd float64) float64 {
	return math.Round(math.Max(0, balanceUsd) * 500000)
}

func (s *Sub2ApiAdapter) resolveExpiresInDays(expiredTime int64) int {
	if expiredTime <= 0 {
		return 0
	}

	var expiresAtMs int64
	if expiredTime > 10_000_000_000 {
		expiresAtMs = expiredTime
	} else {
		expiresAtMs = expiredTime * 1000
	}

	deltaMs := expiresAtMs - time.Now().UnixMilli()
	days := int(math.Max(1, math.Ceil(float64(deltaMs)/float64(24*60*60*1000))))
	if days > 3650 {
		days = 3650
	}
	return days
}

func (s *Sub2ApiAdapter) resolveManagementBaseURL(baseURL string) string {
	normalized := normalizeBaseURL(baseURL)
	if normalized == "" {
		return normalized
	}

	suffixes := []string{
		"/models", "/antigravity", "/antigravity/v1beta", "/antigravity/v1",
		"/api/v1", "/v1beta", "/v1",
	}

	changed := true
	for changed {
		changed = false
		for _, suffix := range suffixes {
			lower := strings.ToLower(normalized)
			if !strings.HasSuffix(lower, suffix) {
				continue
			}
			trimmed := normalizeBaseURL(normalized[:len(normalized)-len(suffix)])
			if trimmed == "" || trimmed == normalized {
				continue
			}
			normalized = trimmed
			changed = true
			break
		}
	}

	return normalized
}

func (s *Sub2ApiAdapter) resolveModelEndpoints(baseURL string) []string {
	normalized := normalizeBaseURL(baseURL)
	if normalized == "" {
		return nil
	}

	if strings.HasSuffix(strings.ToLower(normalized), "/models") {
		return []string{normalized}
	}

	if regexp.MustCompile(`(?i)/(?:antigravity/)?v\d+(?:\.\d+)?(?:beta)?$`).MatchString(normalized) {
		return []string{normalized + "/models"}
	}

	if strings.HasSuffix(strings.ToLower(normalized), "/antigravity") {
		return []string{
			normalized + "/v1/models",
			normalized + "/v1beta/models",
		}
	}

	return []string{
		normalized + "/v1/models",
		normalized + "/api/v1/models",
		normalized + "/v1beta/models",
		normalized + "/antigravity/v1beta/models",
	}
}

func (s *Sub2ApiAdapter) fetchModelsByToken(ctx context.Context, baseURL, token string, proxy *ProxyConfig) []string {
	authToken := stripBearerPrefix(token)
	if authToken == "" {
		return nil
	}

	for _, url := range s.resolveModelEndpoints(baseURL) {
		headers := map[string]string{"Authorization": "Bearer " + authToken}
		resp, err := fetchJSON(ctx, url, "GET", nil, headers, proxy)
		if err != nil {
			continue
		}
		models := extractModelIDs(resp)
		if len(models) > 0 {
			return models
		}
	}

	return nil
}

func extractModelIDs(payload map[string]interface{}) []string {
	var source interface{} = payload
	if data, ok := payload["data"]; ok {
		if m, ok := data.(map[string]interface{}); ok {
			source = m
		} else {
			source = data
		}
	}

	var rawModels []interface{}
	if source != nil {
		switch v := source.(type) {
		case []interface{}:
			rawModels = v
		case map[string]interface{}:
			if items, ok := v["items"].([]interface{}); ok {
				rawModels = items
			} else if models, ok := v["models"].([]interface{}); ok {
				rawModels = models
			}
		}
	}

	seen := make(map[string]bool)
	result := make([]string, 0, len(rawModels))
	for _, item := range rawModels {
		var name string
		switch v := item.(type) {
		case string:
			name = strings.TrimSpace(v)
		case map[string]interface{}:
			if id, ok := v["id"].(string); ok {
				name = strings.TrimSpace(id)
			} else if n, ok := v["name"].(string); ok {
				name = strings.TrimSpace(n)
			}
		}
		name = strings.TrimPrefix(name, "models/")
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}

func (s *Sub2ApiAdapter) listAPIKeys(ctx context.Context, baseURL, accessToken string, proxy *ProxyConfig) ([]sub2apiKeyItem, error) {
	endpoints := []string{
		"/api/v1/keys?page=1&page_size=100",
		"/api/v1/api-keys?page=1&page_size=100",
	}

	headers := s.buildAuthHeader(accessToken)
	for _, endpoint := range endpoints {
		resp, err := fetchJSON(ctx, baseURL+endpoint, "GET", nil, headers, proxy)
		if err != nil {
			continue
		}
		rawData, err := s.parseSub2ApiEnvelopeRaw(resp, endpoint)
		if err != nil {
			continue
		}
		items := s.parseTokenItems(rawData)
		if len(items) > 0 {
			return items, nil
		}
	}

	return nil, nil
}

func (s *Sub2ApiAdapter) parseTokenItems(raw interface{}) []sub2apiKeyItem {
	var rawItems []interface{}
	switch v := raw.(type) {
	case []interface{}:
		rawItems = v
	case map[string]interface{}:
		if items, ok := v["items"].([]interface{}); ok {
			rawItems = items
		} else if items, ok := v["list"].([]interface{}); ok {
			rawItems = items
		} else if items, ok := v["data"].([]interface{}); ok {
			rawItems = items
		}
	}

	items := make([]sub2apiKeyItem, 0, len(rawItems))
	for _, rawItem := range rawItems {
		m, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}

		key, _ := getString(m, "key")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		idFloat, ok := m["id"].(float64)
		if !ok || idFloat <= 0 {
			continue
		}

		name, _ := getString(m, "name")
		name = strings.TrimSpace(name)
		if name == "" {
			name = fmt.Sprintf("token-%d", int(idFloat))
		}

		enabled := true
		if status, ok := m["status"]; ok {
			switch v := status.(type) {
			case bool:
				enabled = v
			case float64:
				enabled = v == 1
			case string:
				lower := strings.ToLower(strings.TrimSpace(v))
				enabled = lower != "inactive" && lower != "disabled" && lower != "false" && lower != "0" && lower != "off"
			}
		}

		tokenGroup := ""
		if gid, ok := m["group_id"].(float64); ok && gid > 0 {
			tokenGroup = fmt.Sprintf("%d", int(gid))
		} else if g, ok := getString(m, "group_name"); ok {
			tokenGroup = g
		} else if g, ok := getString(m, "group"); ok {
			tokenGroup = g
		}

		items = append(items, sub2apiKeyItem{
			id:         int(idFloat),
			key:        key,
			name:       name,
			enabled:    enabled,
			tokenGroup: tokenGroup,
		})
	}
	return items
}

func (s *Sub2ApiAdapter) listGroups(ctx context.Context, baseURL, accessToken string, proxy *ProxyConfig) []string {
	endpoints := []string{
		"/api/v1/groups/available",
		"/api/v1/groups?page=1&page_size=100",
		"/api/v1/groups",
		"/api/v1/group?page=1&page_size=100",
		"/api/v1/group",
	}

	headers := s.buildAuthHeader(accessToken)
	for _, endpoint := range endpoints {
		resp, err := fetchJSON(ctx, baseURL+endpoint, "GET", nil, headers, proxy)
		if err != nil {
			continue
		}

		parsed := s.tryParseEnvelope(resp)
		groups := s.parseGroupItems(parsed)
		if len(groups) > 0 {
			return groups
		}
	}
	return nil
}

func (s *Sub2ApiAdapter) tryParseEnvelope(resp map[string]interface{}) map[string]interface{} {
	if code, ok := resp["code"].(float64); ok && code == 0 {
		if data, ok := getMap(resp, "data"); ok {
			return data
		}
	}
	return resp
}

func (s *Sub2ApiAdapter) parseGroupItems(payload map[string]interface{}) []string {
	var rawItems []interface{}
	switch v := payload["data"].(type) {
	case []interface{}:
		rawItems = v
	}
	if rawItems == nil {
		if rawItems, _ = payload["data"].([]interface{}); rawItems == nil {
			rawItems, _ = payload["items"].([]interface{})
		}
	}
	if rawItems == nil {
		rawItems, _ = payload["list"].([]interface{})
	}
	if rawItems == nil {
		rawItems, _ = payload["groups"].([]interface{})
	}
	if rawItems == nil {
		// Try payload itself as array
		return nil
	}

	seen := make(map[string]bool)
	var groups []string
	for _, item := range rawItems {
		switch v := item.(type) {
		case float64:
			if v > 0 {
				s := fmt.Sprintf("%d", int(v))
				if !seen[s] {
					seen[s] = true
					groups = append(groups, s)
				}
			}
		case string:
			t := strings.TrimSpace(v)
			if t != "" && !seen[t] {
				seen[t] = true
				groups = append(groups, t)
			}
		case map[string]interface{}:
			// Try group_id, id, name
			if gid, ok := v["group_id"].(float64); ok && gid > 0 {
				s := fmt.Sprintf("%d", int(gid))
				if !seen[s] {
					seen[s] = true
					groups = append(groups, s)
				}
				continue
			}
			if id, ok := v["id"].(float64); ok && id > 0 {
				s := fmt.Sprintf("%d", int(id))
				if !seen[s] {
					seen[s] = true
					groups = append(groups, s)
				}
				continue
			}
			if name, ok := getString(v, "name"); ok && name != "" && !seen[name] {
				seen[name] = true
				groups = append(groups, name)
				continue
			}
			if name, ok := getString(v, "group_name"); ok && name != "" && !seen[name] {
				seen[name] = true
				groups = append(groups, name)
			}
		}
	}
	return groups
}

func (s *Sub2ApiAdapter) inferGroupsFromKeys(ctx context.Context, baseURL, accessToken string, proxy *ProxyConfig) []string {
	endpoints := []string{
		"/api/v1/keys?page=1&page_size=100",
		"/api/v1/api-keys?page=1&page_size=100",
	}

	headers := s.buildAuthHeader(accessToken)
	for _, endpoint := range endpoints {
		resp, err := fetchJSON(ctx, baseURL+endpoint, "GET", nil, headers, proxy)
		if err != nil {
			continue
		}

		parsed := s.tryParseEnvelope(resp)
		groupIDs := s.parseGroupIDsFromTokenPayload(parsed)
		if len(groupIDs) > 0 {
			return groupIDs
		}
	}
	return nil
}

func (s *Sub2ApiAdapter) parseGroupIDsFromTokenPayload(payload map[string]interface{}) []string {
	var rawItems []interface{}
	if data, ok := payload["data"].([]interface{}); ok {
		rawItems = data
	} else if items, ok := payload["items"].([]interface{}); ok {
		rawItems = items
	} else if list, ok := payload["list"].([]interface{}); ok {
		rawItems = list
	}

	seen := make(map[string]bool)
	var groups []string
	for _, item := range rawItems {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if gid, ok := m["group_id"].(float64); ok && gid > 0 {
			s := fmt.Sprintf("%d", int(gid))
			if !seen[s] {
				seen[s] = true
				groups = append(groups, s)
			}
		}
	}
	return groups
}

func (s *Sub2ApiAdapter) fetchSubscriptionSummary(ctx context.Context, baseURL, accessToken string, proxy *ProxyConfig) (*SubscriptionSummary, error) {
	headers := s.buildAuthHeader(accessToken)
	summaryEndpoint := "/api/v1/subscriptions/summary"

	resp, err := fetchJSON(ctx, baseURL+summaryEndpoint, "GET", nil, headers, proxy)
	if err != nil {
		// Try fallback
		return s.trySubscriptionFallback(ctx, baseURL, headers, proxy)
	}

	data, err := s.parseSub2ApiEnvelopeRaw(resp, summaryEndpoint)
	if err != nil {
		return s.trySubscriptionFallback(ctx, baseURL, headers, proxy)
	}

	return s.buildSubscriptionSummary(data), nil
}

func (s *Sub2ApiAdapter) trySubscriptionFallback(ctx context.Context, baseURL string, headers map[string]string, proxy *ProxyConfig) (*SubscriptionSummary, error) {
	fallbackEndpoints := []string{"/api/v1/subscriptions/active"}
	for _, endpoint := range fallbackEndpoints {
		resp, err := fetchJSON(ctx, baseURL+endpoint, "GET", nil, headers, proxy)
		if err != nil {
			continue
		}
		data, err := s.parseSub2ApiEnvelopeRaw(resp, endpoint)
		if err != nil {
			continue
		}
		return s.buildSubscriptionSummary(data), nil
	}
	return nil, nil
}

func (s *Sub2ApiAdapter) buildSubscriptionSummary(raw interface{}) *SubscriptionSummary {
	subscriptions := s.parseSubscriptionItems(raw)

	body, _ := raw.(map[string]interface{})
	var activeCount int
	var totalUsedUsd float64

	if body != nil {
		if ac, ok := getFloat(body, "active_count"); ok {
			activeCount = int(ac)
		} else if ac, ok := getFloat(body, "activeCount"); ok {
			activeCount = int(ac)
		}

		if tu, ok := getFloat(body, "total_used_usd"); ok {
			totalUsedUsd = tu
		} else if tu, ok := getFloat(body, "totalUsedUsd"); ok {
			totalUsedUsd = tu
		}
	}

	if activeCount == 0 {
		activeCount = len(subscriptions)
	}

	if totalUsedUsd == 0 {
		for _, sub := range subscriptions {
			if sub.MonthlyUsedUsd != nil {
				totalUsedUsd += *sub.MonthlyUsedUsd
			}
		}
		totalUsedUsd = math.Round(totalUsedUsd*1e6) / 1e6
	}

	return &SubscriptionSummary{
		ActiveCount:   activeCount,
		TotalUsedUsd:  totalUsedUsd,
		Subscriptions: subscriptions,
	}
}

func (s *Sub2ApiAdapter) parseSubscriptionItems(raw interface{}) []SubscriptionPlanSummary {
	var rawItems []interface{}
	switch v := raw.(type) {
	case []interface{}:
		rawItems = v
	case map[string]interface{}:
		if arr, ok := v["subscriptions"].([]interface{}); ok {
			rawItems = arr
		} else if arr, ok := v["items"].([]interface{}); ok {
			rawItems = arr
		} else if arr, ok := v["list"].([]interface{}); ok {
			rawItems = arr
		} else if arr, ok := v["data"].([]interface{}); ok {
			rawItems = arr
		}
	}

	result := make([]SubscriptionPlanSummary, 0, len(rawItems))
	for _, item := range rawItems {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if summary := s.parseSingleSubscription(m); summary != nil {
			result = append(result, *summary)
		}
	}
	return result
}

func (s *Sub2ApiAdapter) parseSingleSubscription(item map[string]interface{}) *SubscriptionPlanSummary {
	summary := SubscriptionPlanSummary{}

	if id := getIntPtr(item, "id"); id != nil {
		summary.ID = id
	}

	// Try group_id from nested group object or direct
	groupObj, _ := getMap(item, "group")
	if gid := getIntPtr(item, "group_id"); gid != nil {
		summary.GroupID = gid
	} else if gid := getIntPtr(item, "groupId"); gid != nil {
		summary.GroupID = gid
	} else if groupObj != nil {
		if gid := getIntPtr(groupObj, "id"); gid != nil {
			summary.GroupID = gid
		}
	}

	// Group name from multiple candidates
	groupNameCandidates := []string{}
	if s, _ := getString(item, "group_name"); s != "" {
		groupNameCandidates = append(groupNameCandidates, s)
	}
	if s, _ := getString(item, "groupName"); s != "" {
		groupNameCandidates = append(groupNameCandidates, s)
	}
	if s, _ := getString(item, "name"); s != "" {
		groupNameCandidates = append(groupNameCandidates, s)
	}
	if s, _ := getString(item, "title"); s != "" {
		groupNameCandidates = append(groupNameCandidates, s)
	}
	if groupObj != nil {
		if s, _ := getString(groupObj, "name"); s != "" {
			groupNameCandidates = append(groupNameCandidates, s)
		}
		if s, _ := getString(groupObj, "title"); s != "" {
			groupNameCandidates = append(groupNameCandidates, s)
		}
	}
	if len(groupNameCandidates) > 0 {
		summary.GroupName = groupNameCandidates[0]
	}

	if s, _ := getString(item, "status"); s != "" {
		summary.Status = s
	}

	// Expires at
	expiresAt := s.parseDateTime(
		s.getRawString(item, "expires_at"),
		s.getRawString(item, "expiresAt"),
		s.getRawString(item, "expired_at"),
		s.getRawString(item, "expiredAt"),
		s.getRawString(item, "end_at"),
		s.getRawString(item, "endAt"),
		s.getRawString(item, "end_time"),
		s.getRawString(item, "endTime"),
		s.getRawString(item, "current_period_end"),
		s.getRawString(item, "currentPeriodEnd"),
	)
	if expiresAt != "" {
		summary.ExpiresAt = expiresAt
	}

	// Daily
	if v := s.parseNonNegativeNumber(s.getRaw(item, "daily_used_usd"), s.getRaw(item, "dailyUsedUsd")); v != nil {
		summary.DailyUsedUsd = v
	}
	if v := s.parseNonNegativeNumber(s.getRaw(item, "daily_limit_usd"), s.getRaw(item, "dailyLimitUsd")); v != nil {
		summary.DailyLimitUsd = v
	}

	// Weekly
	if v := s.parseNonNegativeNumber(s.getRaw(item, "weekly_used_usd"), s.getRaw(item, "weeklyUsedUsd")); v != nil {
		summary.WeeklyUsedUsd = v
	}
	if v := s.parseNonNegativeNumber(s.getRaw(item, "weekly_limit_usd"), s.getRaw(item, "weeklyLimitUsd")); v != nil {
		summary.WeeklyLimitUsd = v
	}

	// Monthly
	if v := s.parseNonNegativeNumber(
		s.getRaw(item, "monthly_used_usd"), s.getRaw(item, "monthlyUsedUsd"),
		s.getRaw(item, "used_usd"), s.getRaw(item, "usedUsd"),
		s.getRaw(item, "total_used_usd"), s.getRaw(item, "totalUsedUsd"),
	); v != nil {
		summary.MonthlyUsedUsd = v
	}
	if v := s.parseNonNegativeNumber(
		s.getRaw(item, "monthly_limit_usd"), s.getRaw(item, "monthlyLimitUsd"),
		s.getRaw(item, "limit_usd"), s.getRaw(item, "limitUsd"),
		s.getRaw(item, "total_limit_usd"), s.getRaw(item, "totalLimitUsd"),
	); v != nil {
		summary.MonthlyLimitUsd = v
	}

	// If nothing parsed, return nil
	if summary.ID == nil && summary.GroupID == nil && summary.GroupName == "" &&
		summary.Status == "" && summary.ExpiresAt == "" &&
		summary.DailyUsedUsd == nil && summary.MonthlyUsedUsd == nil {
		return nil
	}

	return &summary
}

func (s *Sub2ApiAdapter) getRaw(item map[string]interface{}, key string) interface{} {
	return item[key]
}

func (s *Sub2ApiAdapter) getRawString(item map[string]interface{}, key string) string {
	v := item[key]
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		return fmt.Sprintf("%v", val)
	}
	return ""
}

func (s *Sub2ApiAdapter) parseNonNegativeNumber(values ...interface{}) *float64 {
	for _, v := range values {
		if v == nil {
			continue
		}
		switch val := v.(type) {
		case float64:
			if val >= 0 {
				result := math.Round(val*1e6) / 1e6
				return &result
			}
		case string:
			trimmed := strings.TrimSpace(val)
			if trimmed == "" {
				continue
			}
			// Try parse
			var f float64
			if _, err := fmt.Sscanf(trimmed, "%f", &f); err == nil && f >= 0 {
				result := math.Round(f*1e6) / 1e6
				return &result
			}
		}
	}
	return nil
}

func (s *Sub2ApiAdapter) parseDateTime(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}

		// Try numeric (unix timestamp)
		var numeric float64
		if _, err := fmt.Sscanf(v, "%f", &numeric); err == nil && numeric > 0 {
			ms := numeric
			if ms < 10_000_000_000 {
				ms *= 1000
			}
			return time.UnixMilli(int64(ms)).Format(time.RFC3339)
		}

		// Try date parsing
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return ""
}

func parseGroupID(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty")
	}
	var id int
	_, err := fmt.Sscanf(raw, "%d", &id)
	return id, err
}
