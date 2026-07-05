package adapter

import "testing"

type mockAdapter struct {
	name string
}

func (m *mockAdapter) Checkin(baseURL, accessToken string, platformUserID int64, proxyURL string) (*CheckinResult, error) {
	return &CheckinResult{Success: true, Message: "ok", Reward: "10"}, nil
}

func (m *mockAdapter) Login(baseURL, username, password, proxyURL string) (*LoginResult, error) {
	return &LoginResult{Success: true, AccessToken: "mock-token"}, nil
}

func (m *mockAdapter) GetBalance(baseURL, accessToken string, platformUserID int64, proxyURL string) (*BalanceInfo, error) {
	return &BalanceInfo{Balance: 100.0, Used: 50.0, Quota: 200.0}, nil
}

func TestRegisterAndGetAdapter(t *testing.T) {
	ma := &mockAdapter{name: "test-platform"}
	RegisterAdapter("test-platform", ma)

	got := GetAdapter("test-platform")
	if got == nil {
		t.Fatal("expected adapter, got nil")
	}

	result, err := got.Checkin("url", "token", 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success || result.Reward != "10" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestGetAdapterNotFound(t *testing.T) {
	if got := GetAdapter("nonexistent"); got != nil {
		t.Errorf("expected nil for unknown platform, got %v", got)
	}
}

func TestBalanceInfo(t *testing.T) {
	ma := &mockAdapter{name: "balance-test"}
	RegisterAdapter("balance-test", ma)
	got := GetAdapter("balance-test")
	if got == nil {
		t.Fatal("expected adapter")
	}
	info, err := got.GetBalance("url", "token", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Balance != 100.0 || info.Used != 50.0 || info.Quota != 200.0 {
		t.Errorf("wrong balance info: %+v", info)
	}
}
