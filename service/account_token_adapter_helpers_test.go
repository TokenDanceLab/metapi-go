package service

import (
	"context"
	"testing"

	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/store"
)

type stubTokenAdapter struct {
	platform.BaseAdapter
	tokens []platform.ApiTokenInfo
	single *string
	err    error
}

func (s *stubTokenAdapter) PlatformName() string { return "stub-token" }

func (s *stubTokenAdapter) GetAPITokens(ctx context.Context, url, accessToken string, platformUserId *int, proxy *platform.ProxyConfig) ([]platform.ApiTokenInfo, error) {
	return s.tokens, s.err
}

func (s *stubTokenAdapter) GetAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, proxy *platform.ProxyConfig) (*string, error) {
	return s.single, s.err
}

func TestPlatformAPITokensToUpstream(t *testing.T) {
	out := PlatformAPITokensToUpstream([]platform.ApiTokenInfo{
		{Name: " a ", Key: " sk-1 ", Enabled: true, TokenGroup: " g "},
		{Name: "empty", Key: "   ", Enabled: false},
	})
	if len(out) != 1 {
		t.Fatalf("len=%d want 1", len(out))
	}
	if out[0].Key != "sk-1" || out[0].Name != "a" || out[0].TokenGroup != "g" || !out[0].Enabled {
		t.Fatalf("unexpected conversion: %#v", out[0])
	}
}

func TestFetchUpstreamAPITokens_FallbackToSingle(t *testing.T) {
	single := "sk-single"
	adp := &stubTokenAdapter{single: &single}
	out, err := FetchUpstreamAPITokens(context.Background(), adp, "https://example.com", "session", nil, nil)
	if err != nil {
		t.Fatalf("FetchUpstreamAPITokens: %v", err)
	}
	if len(out) != 1 || out[0].Key != "sk-single" {
		t.Fatalf("unexpected tokens: %#v", out)
	}
}

func TestResolvePlatformUserIDPtr(t *testing.T) {
	extra := `{"platformUserId": 99}`
	username := "42"
	acct := &store.Account{ExtraConfig: &extra, Username: &username}
	id := ResolvePlatformUserIDPtr(acct)
	if id == nil || *id != 99 {
		t.Fatalf("expected 99 from extraConfig, got %#v", id)
	}

	acct2 := &store.Account{Username: &username}
	id2 := ResolvePlatformUserIDPtr(acct2)
	if id2 == nil || *id2 != 42 {
		t.Fatalf("expected 42 from username, got %#v", id2)
	}
}

func TestSyncTokensFromUpstream_CreatesAndUpdates(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "SyncSite", "https://sync.example.com", "new-api")
	accountID := createTestAccount(t, db, siteID, strPtr("u1"), "session")
	// existing ready token should be updated, not duplicated
	_ = createTestAccountToken(t, db, accountID, "old-name", "sk-keep", true)

	result, err := SyncTokensFromUpstream(db.DB, accountID, []UpstreamAPIToken{
		{Name: "upstream-name", Key: "sk-keep", Enabled: false, TokenGroup: "vip"},
		{Name: "new-one", Key: "sk-new", Enabled: true, TokenGroup: "default"},
	})
	if err != nil {
		t.Fatalf("SyncTokensFromUpstream: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("created=%d want 1", result.Created)
	}
	if result.Updated != 1 {
		t.Fatalf("updated=%d want 1", result.Updated)
	}

	var name string
	var enabled bool
	if err := db.QueryRow("SELECT name, enabled FROM account_tokens WHERE token = ?", "sk-keep").Scan(&name, &enabled); err != nil {
		t.Fatalf("read kept token: %v", err)
	}
	// preserve operator-set name/enabled
	if name != "old-name" {
		t.Fatalf("name=%q want old-name", name)
	}
	if !enabled {
		t.Fatalf("enabled should be preserved as true")
	}
}
