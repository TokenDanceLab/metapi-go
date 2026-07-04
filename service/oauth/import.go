package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxOAuthImportBatchSize = 100

// ImportResult holds the result of an import operation.
type ImportResult struct {
	Success  bool           `json:"success"`
	Imported int            `json:"imported"`
	Skipped  int            `json:"skipped"`
	Failed   int            `json:"failed"`
	Items    []ImportItem   `json:"items"`
}

// ImportItem represents a single import result item.
type ImportItem struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // "imported" | "skipped" | "failed"
	AccountID int64  `json:"accountId,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Message   string `json:"message,omitempty"`
}

// ImportOauthConnectionsFromNativeJSON imports OAuth connections from native JSON data.
func ImportOauthConnectionsFromNativeJSON(data interface{}, items []interface{}, proxyURL *string, useSystemProxy bool) (*ImportResult, error) {
	var payloadItems []interface{}
	if len(items) > 0 {
		payloadItems = items
	} else if data != nil {
		payloadItems = []interface{}{data}
	}
	continueOnItemFailure := len(items) > 0

	if len(payloadItems) == 0 {
		return nil, fmt.Errorf("data must be a native oauth json object")
	}
	if len(payloadItems) > maxOAuthImportBatchSize {
		return nil, fmt.Errorf("oauth import supports at most %d items", maxOAuthImportBatchSize)
	}

	result := &ImportResult{
		Items: make([]ImportItem, 0, len(payloadItems)),
	}

	for _, rawPayload := range payloadItems {
		payload, ok := rawPayload.(map[string]interface{})
		if !ok {
			if !continueOnItemFailure {
				return nil, fmt.Errorf("data must be a native oauth json object")
			}
			result.Items = append(result.Items, ImportItem{Status: "failed", Message: "invalid payload"})
			result.Failed++
			continue
		}

		identity, err := resolveImportedNativeOauthIdentity(payload)
		if err != nil {
			if !continueOnItemFailure {
				return nil, err
			}
			result.Items = append(result.Items, ImportItem{Name: identity.name, Status: "failed", Message: err.Error()})
			result.Failed++
			continue
		}

		def := GetProviderDefinition(identity.provider)
		if def == nil {
			err := fmt.Errorf("unsupported oauth provider: %s", identity.provider)
			if !continueOnItemFailure {
				return nil, err
			}
			result.Items = append(result.Items, ImportItem{Name: identity.name, Status: "failed", Provider: identity.provider, Message: err.Error()})
			result.Failed++
			continue
		}

		persistStatus := "active"
		if identity.disabled {
			persistStatus = "disabled"
		}

		persistResult, err := activatePersistedOAuthAccount(context.Background(), ActivateInput{
			Definition:      def,
			Exchange:        identity.exchange,
			ProxyURL:        ptrToString(proxyURL),
			UseSystemProxy:  useSystemProxy,
			PersistedStatus: persistStatus,
		})
		if err != nil {
			if !continueOnItemFailure {
				return nil, err
			}
			result.Items = append(result.Items, ImportItem{Name: identity.name, Status: "failed", Provider: identity.provider, Message: err.Error()})
			result.Failed++
			continue
		}

		result.Imported++
		result.Items = append(result.Items, ImportItem{
			Name:      identity.name,
			Status:    "imported",
			Provider:  identity.provider,
			AccountID: persistResult.AccountID,
		})
	}

	result.Failed = 0
	for _, item := range result.Items {
		if item.Status == "failed" {
			result.Failed++
		}
	}
	result.Success = result.Failed == 0
	return result, nil
}

type importedOauthIdentity struct {
	name     string
	provider string
	disabled bool
	exchange *TokenSet
}

func resolveImportedNativeOauthIdentity(payload map[string]interface{}) (*importedOauthIdentity, error) {
	rawType := asNonEmptyString(payload["type"])

	// Reject sub2api envelopes.
	if rawType == "sub2api-data" || rawType == "sub2api-bundle" {
		return nil, fmt.Errorf("native oauth json expected; sub2api envelopes are no longer supported")
	}
	if _, hasAccounts := payload["accounts"]; hasAccounts {
		return nil, fmt.Errorf("native oauth json expected; sub2api envelopes are no longer supported")
	}
	if _, hasProxies := payload["proxies"]; hasProxies {
		return nil, fmt.Errorf("native oauth json expected; sub2api envelopes are no longer supported")
	}
	if _, hasVersion := payload["version"]; hasVersion {
		return nil, fmt.Errorf("native oauth json expected; sub2api envelopes are no longer supported")
	}
	if _, hasExportedAt := payload["exported_at"]; hasExportedAt {
		return nil, fmt.Errorf("native oauth json expected; sub2api envelopes are no longer supported")
	}

	provider := mapImportedOauthProvider(rawType)
	if provider == "" {
		return nil, fmt.Errorf("unsupported oauth import type: %s", rawType)
	}

	accessToken := asNonEmptyString(payload["access_token"])
	if accessToken == "" {
		accessToken = asNonEmptyString(payload["session_token"])
	}
	if accessToken == "" {
		return nil, fmt.Errorf("oauth credentials missing access_token/session_token")
	}

	derived := resolveImportedOauthIdentityFields(provider, payload)

	exchange := &TokenSet{
		AccessToken: accessToken,
	}
	if v := asNonEmptyString(payload["refresh_token"]); v != "" {
		exchange.RefreshToken = v
	} else if derived.RefreshToken != "" {
		exchange.RefreshToken = derived.RefreshToken
	}

	if v := parseImportedOauthExpiry(payload["expired"]); v > 0 {
		exchange.TokenExpiresAt = v
	} else if derived.TokenExpiresAt > 0 {
		exchange.TokenExpiresAt = derived.TokenExpiresAt
	}

	exchange.IDToken = derived.IDToken
	exchange.Email = coalesceStr(asNonEmptyString(payload["email"]), derived.Email)
	exchange.AccountKey = coalesceStr(asNonEmptyString(payload["account_key"]), asNonEmptyString(payload["account_id"]), derived.AccountKey)
	exchange.AccountID = coalesceStr(asNonEmptyString(payload["account_id"]), exchange.AccountKey, derived.AccountID)
	exchange.PlanType = derived.PlanType
	exchange.ProjectID = coalesceStr(asNonEmptyString(payload["project_id"]), derived.ProjectID)
	exchange.ProviderData = derived.ProviderData

	if exchange.AccountKey == "" && exchange.AccountID != "" {
		exchange.AccountKey = exchange.AccountID
	}
	if exchange.AccountID == "" && exchange.AccountKey != "" {
		exchange.AccountID = exchange.AccountKey
	}

	disabled := false
	if d, ok := payload["disabled"].(bool); ok {
		disabled = d
	}

	name := asNonEmptyString(payload["email"])
	if name == "" {
		name = exchange.AccountKey
	}
	if name == "" {
		name = provider
	}

	return &importedOauthIdentity{
		name:     name,
		provider: provider,
		disabled: disabled,
		exchange: exchange,
	}, nil
}

func mapImportedOauthProvider(platform string) string {
	normalized := strings.ToLower(strings.TrimSpace(platform))
	switch normalized {
	case "codex":
		return "codex"
	case "claude":
		return "claude"
	case "gemini-cli":
		return "gemini-cli"
	case "antigravity":
		return "antigravity"
	case "openai":
		return "codex"
	case "anthropic":
		return "claude"
	case "gemini":
		return "gemini-cli"
	default:
		return ""
	}
}

func resolveImportedOauthIdentityFields(provider string, payload map[string]interface{}) *TokenSet {
	idToken := asNonEmptyString(payload["id_token"])
	claims := decodeJWTClaims(idToken)

	var authClaims map[string]interface{}
	if ac, ok := claims["https://api.openai.com/auth"].(map[string]interface{}); ok {
		authClaims = ac
	}

	result := &TokenSet{
		IDToken: idToken,
	}

	result.Email = coalesceStr(
		asNonEmptyString(claims["email"]),
	)

	var chatgptAccountID string
	if authClaims != nil {
		chatgptAccountID = asNonEmptyString(authClaims["chatgpt_account_id"])
	}

	result.AccountKey = coalesceStr(
		asNonEmptyString(payload["chatgpt_account_id"]),
		asNonEmptyString(payload["account_key"]),
		asNonEmptyString(payload["account_id"]),
		chatgptAccountID,
		result.Email,
	)
	if result.AccountKey == "" {
		result.AccountKey = result.Email
	}
	result.AccountID = result.AccountKey

	if authClaims != nil {
		result.PlanType = asNonEmptyString(authClaims["chatgpt_plan_type"])
	}

	if pd, ok := payload["provider_data"].(map[string]interface{}); ok {
		result.ProviderData = pd
	}

	result.ProjectID = coalesceStr(
		asNonEmptyString(payload["project_id"]),
		asNonEmptyString(payload["cloudaicompanionProject"]),
	)

	return result
}

func parseImportedOauthExpiry(value interface{}) int64 {
	if value == nil {
		return 0
	}
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
		// Try numeric.
		if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 0 {
			return n
		}
		// Try ISO 8601 date.
		if t, err := time.Parse(time.RFC3339, trimmed); err == nil {
			return t.UnixMilli()
		}
		// Try other common formats.
		for _, layout := range []string{
			"2006-01-02T15:04:05.000Z",
			"2006-01-02T15:04:05Z",
			time.RFC1123,
			time.RFC1123Z,
		} {
			if t, err := time.Parse(layout, trimmed); err == nil {
				return t.UnixMilli()
			}
		}
		return 0
	}
	return 0
}

func decodeJWTClaims(token string) map[string]interface{} {
	if token == "" {
		return nil
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	decoded, err := base64Decode(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil
	}
	return claims
}

func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
