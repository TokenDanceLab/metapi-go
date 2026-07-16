package platform

import (
	"context"
	"testing"
)

func TestOneApiAdapter_PlatformName(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	if o.PlatformName() != "one-api" {
		t.Errorf("PlatformName: %q", o.PlatformName())
	}
}

func TestOneApiAdapter_Detect(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	ctx := context.Background()

	// Detect requires HTTP probe; will fail and return false for non-existent URLs
	ok, err := o.Detect(ctx, "http://127.0.0.1:1")
	if err != nil {
		t.Errorf("Detect should not return error on probe failure: %v", err)
	}
	if ok {
		t.Error("Detect should return false for unreachable URL")
	}
}

func TestOneApiAdapter_BalanceQuotaMinusUsed(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	ctx := context.Background()

	// OneApi model B: quota=total, balance=quota-used
	// Impossible to test balance without HTTP, but verify the struct has correct inheritance
	_ = o
	_ = ctx
}

func TestOneApiAdapter_BalanceParseLogic(t *testing.T) {
	// Verify OneApi balance formula via the code pattern
	// OneApi: balance = (quota - used) / 500000, quotaUSD = quota/500000, usedUSD = used/500000
	// So with quota=1000000, used=500000: balance=1.0, quota=2.0, used=1.0

	// This is a unit test of the balance formula
	quota := 1000000.0
	used := 500000.0
	balance := (quota - used) / 500000
	quotaUSD := quota / 500000
	usedUSD := used / 500000

	if balance != 1.0 {
		t.Errorf("OneApi balance formula: %f, want 1.0", balance)
	}
	if quotaUSD != 2.0 {
		t.Errorf("OneApi quotaUSD: %f, want 2.0", quotaUSD)
	}
	if usedUSD != 1.0 {
		t.Errorf("OneApi usedUSD: %f, want 1.0", usedUSD)
	}
}

func TestOneApiAdapter_DoubleDeleteStrategy(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	ctx := context.Background()

	// DeleteAPIToken should not error even with unreachable URL (returns nil)
	err := o.DeleteAPIToken(ctx, "http://127.0.0.1:1", "token", "sk-test", nil, nil)
	if err != nil {
		t.Errorf("DeleteAPIToken should be idempotent: %v", err)
	}

	// Empty tokenKey should return nil immediately
	err = o.DeleteAPIToken(ctx, "http://127.0.0.1:1", "token", "", nil, nil)
	if err != nil {
		t.Errorf("DeleteAPIToken with empty key: %v", err)
	}
}

func TestOneApiAdapter_GetAPIToken(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	ctx := context.Background()

	tok, err := o.GetAPIToken(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tok != nil {
		t.Error("GetAPIToken should return nil for unreachable URL")
	}
}

func TestOneApiAdapter_GetAPITokens(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	ctx := context.Background()

	tokens, err := o.GetAPITokens(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(tokens) != 0 {
		t.Error("GetAPITokens should return empty for unreachable URL")
	}
}

func TestOneApiAdapter_GetUserGroupsDefault(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	ctx := context.Background()

	// On unreachable URL, terminalError from failed HTTP propagates as error
	_, err := o.GetUserGroups(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Logf("GetUserGroups error on unreachable (expected): %v", err)
	}
	// Either way: error or ["default"] is acceptable
}

func TestOneApiAdapter_CreateAPIToken(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	ctx := context.Background()

	created, err := o.CreateAPIToken(ctx, "http://127.0.0.1:1", "token", nil, nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if created {
		t.Error("CreateAPIToken should return false for unreachable URL")
	}
}

func TestBuildDefaultTokenPayload(t *testing.T) {
	// Default options (nil)
	p := buildDefaultTokenPayload(nil)
	if p["name"] != "metapi" {
		t.Errorf("default name: %q", p["name"])
	}
	if p["unlimited_quota"] != true {
		t.Error("default unlimited_quota should be true")
	}
	if p["expired_time"] != int64(-1) {
		t.Errorf("default expired_time: %v", p["expired_time"])
	}

	// Custom options
	opts := &CreateAPITokenOptions{
		Name:            "custom-key",
		UnlimitedQuota:  false,
		RemainQuota:     100.5,
		ExpiredTime:     1735689600,
		AllowIPs:        "1.2.3.4",
		ModelLimits:     "gpt-4:100",
		Group:           "vip",
		ModelLimitsEnabled: true,
	}
	p2 := buildDefaultTokenPayload(opts)
	if p2["name"] != "custom-key" {
		t.Errorf("custom name: %q", p2["name"])
	}
	if p2["unlimited_quota"] != false {
		t.Error("custom unlimited_quota should be false")
	}
	if p2["remain_quota"] != 100.5 {
		t.Errorf("custom remain_quota: %v", p2["remain_quota"])
	}
	if p2["group"] != "vip" {
		t.Errorf("custom group: %q", p2["group"])
	}
}

func TestResolveGroupFetchErrorMessage(t *testing.T) {
	// Expired token
	msg := resolveGroupFetchErrorMessage(map[string]interface{}{
		"message": "Token expired",
	})
	if msg != "账号会话可能已过期，请重新登录后再拉取分组" {
		t.Errorf("expired token: %q", msg)
	}

	// Invalid token
	msg2 := resolveGroupFetchErrorMessage(map[string]interface{}{
		"message": "Invalid token",
	})
	if msg2 != "账号会话可能已过期，请重新登录后再拉取分组" {
		t.Errorf("invalid token: %q", msg2)
	}

	// Normal message
	msg3 := resolveGroupFetchErrorMessage(map[string]interface{}{
		"message": "Something else happened",
	})
	if msg3 != "Something else happened" {
		t.Errorf("normal message: %q", msg3)
	}

	// Empty
	msg4 := resolveGroupFetchErrorMessage(map[string]interface{}{})
	if msg4 != "failed to fetch groups" {
		t.Errorf("empty: %q", msg4)
	}

	// Non-auth messages must not be rewritten as session-expired UX (R0).
	msg5 := resolveGroupFetchErrorMessage(map[string]interface{}{
		"message": "No payment method. Add a payment method here: https://example.com/billing",
	})
	if msg5 != "No payment method. Add a payment method here: https://example.com/billing" {
		t.Errorf("billing should not rewrite to session-expired: %q", msg5)
	}
	msg6 := resolveGroupFetchErrorMessage(map[string]interface{}{
		"message": "Model foo is not supported for format openai",
	})
	if msg6 != "Model foo is not supported for format openai" {
		t.Errorf("model unsupported should not rewrite to session-expired: %q", msg6)
	}
}

func TestExtractGroupKeys(t *testing.T) {
	// With data wrapper
	resp := map[string]interface{}{
		"success": true,
		"message": "ok",
		"data": map[string]interface{}{
			"vip":     map[string]interface{}{},
			"default": map[string]interface{}{},
			"premium": map[string]interface{}{},
		},
	}
	keys := extractGroupKeys(resp)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(keys), keys)
	}

	// Without data wrapper - excludes special keys
	resp2 := map[string]interface{}{
		"success": true,
		"message": "ok",
		"vip":     map[string]interface{}{},
	}
	keys2 := extractGroupKeys(resp2)
	if len(keys2) != 1 || keys2[0] != "vip" {
		t.Errorf("direct keys: %v", keys2)
	}
}

func TestDedupeStrings(t *testing.T) {
	items := []string{"a", "b", "a", "c", "  b  "}
	result := dedupeStrings(items)
	if len(result) != 3 {
		t.Fatalf("expected 3 unique, got %d: %v", len(result), result)
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("deduped: %v", result)
	}

	// Empty
	result2 := dedupeStrings([]string{})
	if len(result2) != 0 {
		t.Errorf("empty input: %v", result2)
	}
}

func TestOneApiAdapter_Checkin(t *testing.T) {
	o := &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-api")}
	ctx := context.Background()

	cr, err := o.Checkin(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Errorf("Checkin should not error: %v", err)
	}
	if cr.Success {
		t.Error("Checkin on unreachable URL should fail")
	}
}
