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

	groups       []string
	groupsErr    error
	groupsCalled int
}

func (s *stubTokenAdapter) PlatformName() string { return "stub-token" }

func (s *stubTokenAdapter) GetAPITokens(ctx context.Context, url, accessToken string, platformUserId *int, proxy *platform.ProxyConfig) ([]platform.ApiTokenInfo, error) {
	return s.tokens, s.err
}

func (s *stubTokenAdapter) GetAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, proxy *platform.ProxyConfig) (*string, error) {
	return s.single, s.err
}

func (s *stubTokenAdapter) GetUserGroups(ctx context.Context, url, accessToken string, platformUserId *int, proxy *platform.ProxyConfig) ([]string, error) {
	s.groupsCalled++
	return s.groups, s.groupsErr
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

func TestNormalizeTokenGroups(t *testing.T) {
	got := NormalizeTokenGroups([]string{" vip ", "", "vip", "default", "  "})
	if len(got) != 2 || got[0] != "vip" || got[1] != "default" {
		t.Fatalf("unexpected groups: %#v", got)
	}
	got = NormalizeTokenGroups(nil)
	if len(got) != 1 || got[0] != "default" {
		t.Fatalf("empty should become default, got %#v", got)
	}
}

func TestGetTokenGroups_PrefersUpstream(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "GroupSite", "https://groups.example.com", "new-api")
	accountID := createTestAccount(t, db, siteID, strPtr("u1"), "session-token")
	_ = createTestAccountToken(t, db, accountID, "local", "sk-local", true)
	// local group default only; upstream should win
	if _, err := db.Exec(`UPDATE account_tokens SET token_group = ? WHERE account_id = ?`, "local-only", accountID); err != nil {
		t.Fatalf("set local group: %v", err)
	}

	adp := &stubTokenAdapter{groups: []string{" vip ", "vip", "default", " "}}
	got, err := GetTokenGroups(context.Background(), db.DB, accountID, adp, "https://groups.example.com", "session-token", nil, nil)
	if err != nil {
		t.Fatalf("GetTokenGroups: %v", err)
	}
	if adp.groupsCalled != 1 {
		t.Fatalf("groupsCalled=%d want 1", adp.groupsCalled)
	}
	if len(got) != 2 || got[0] != "vip" || got[1] != "default" {
		t.Fatalf("unexpected upstream groups: %#v", got)
	}
}

func TestGetTokenGroups_FallbackOnErrorEmptyNilAdapter(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "GroupFallback", "https://groups-fallback.example.com", "new-api")
	accountID := createTestAccount(t, db, siteID, strPtr("u1"), "session-token")
	if _, err := db.Exec(
		`INSERT INTO account_tokens (account_id, name, token, token_group, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, 'a', 'sk-a', 'group-a', 'ready', 'manual', 1, 1, datetime('now'), datetime('now'))`,
		accountID,
	); err != nil {
		t.Fatalf("insert local token: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO account_tokens (account_id, name, token, token_group, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, 'b', 'sk-b', 'group-b', 'ready', 'manual', 1, 0, datetime('now'), datetime('now'))`,
		accountID,
	); err != nil {
		t.Fatalf("insert local token b: %v", err)
	}

	// adapter error → local
	errAdp := &stubTokenAdapter{groupsErr: context.DeadlineExceeded}
	got, err := GetTokenGroups(context.Background(), db.DB, accountID, errAdp, "https://x", "session-token", nil, nil)
	if err != nil {
		t.Fatalf("error path: %v", err)
	}
	if !containsAllStrings(got, "group-a", "group-b") {
		t.Fatalf("error fallback groups=%#v", got)
	}

	// empty upstream → local
	emptyAdp := &stubTokenAdapter{groups: []string{"", "  "}}
	got, err = GetTokenGroups(context.Background(), db.DB, accountID, emptyAdp, "https://x", "session-token", nil, nil)
	if err != nil {
		t.Fatalf("empty path: %v", err)
	}
	if !containsAllStrings(got, "group-a", "group-b") {
		t.Fatalf("empty fallback groups=%#v", got)
	}

	// nil adapter → local, no panic
	got, err = GetTokenGroups(context.Background(), db.DB, accountID, nil, "https://x", "session-token", nil, nil)
	if err != nil {
		t.Fatalf("nil adapter path: %v", err)
	}
	if !containsAllStrings(got, "group-a", "group-b") {
		t.Fatalf("nil adapter groups=%#v", got)
	}

	// missing access token → local without calling adapter
	skipAdp := &stubTokenAdapter{groups: []string{"should-not-use"}}
	got, err = GetTokenGroups(context.Background(), db.DB, accountID, skipAdp, "https://x", "  ", nil, nil)
	if err != nil {
		t.Fatalf("no access token path: %v", err)
	}
	if skipAdp.groupsCalled != 0 {
		t.Fatalf("adapter should not be called without access token")
	}
	if !containsAllStrings(got, "group-a", "group-b") {
		t.Fatalf("no-token groups=%#v", got)
	}

	// no local rows → default
	emptyAccount := createTestAccount(t, db, siteID, strPtr("u2"), "session-token")
	got, err = GetTokenGroups(context.Background(), db.DB, emptyAccount, nil, "https://x", "", nil, nil)
	if err != nil {
		t.Fatalf("empty local path: %v", err)
	}
	if len(got) != 1 || got[0] != "default" {
		t.Fatalf("expected default, got %#v", got)
	}
}

func containsAllStrings(haystack []string, needles ...string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, h := range haystack {
		set[h] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}
