package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// fakeNewAPIUpstream simulates NewAPI token endpoints used by platform.NewApiAdapter.
type fakeNewAPIUpstream struct {
	mu     sync.Mutex
	tokens map[string]map[string]any // key -> item
	nextID int
}

func newFakeNewAPIUpstream() *fakeNewAPIUpstream {
	return &fakeNewAPIUpstream{
		tokens: map[string]map[string]any{},
		nextID: 1,
	}
}

func (f *fakeNewAPIUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/status":
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"data":    map[string]any{"system_name": "New API"},
		})
		return
	case strings.HasPrefix(path, "/api/token"):
		f.mu.Lock()
		defer f.mu.Unlock()

		// /api/token or /api/token/ or /api/token/?p=0
		trimmed := strings.TrimPrefix(path, "/api/token")
		trimmed = strings.TrimPrefix(trimmed, "/")
		if trimmed == "" {
			switch r.Method {
			case http.MethodGet:
				items := make([]map[string]any, 0, len(f.tokens))
				for _, item := range f.tokens {
					items = append(items, item)
				}
				writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": items})
				return
			case http.MethodPost:
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				name, _ := body["name"].(string)
				if strings.TrimSpace(name) == "" {
					name = "metapi"
				}
				group, _ := body["group"].(string)
				key := "sk-newapi-" + strings.ReplaceAll(name, " ", "-") + "-" + itoa(int64(f.nextID))
				item := map[string]any{
					"id":     f.nextID,
					"name":   name,
					"key":    key,
					"status": 1,
					"group":  group,
				}
				f.tokens[key] = item
				f.nextID++
				writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": item})
				return
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
		}

		// DELETE /api/token/{id}
		if r.Method == http.MethodDelete {
			idStr := strings.Trim(trimmed, "/")
			for key, item := range f.tokens {
				if idToString(item["id"]) == idStr {
					delete(f.tokens, key)
					writeJSON(w, http.StatusOK, map[string]any{"success": true})
					return
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "not found"})
			return
		}

		// Fallback GET list for odd paths.
		if r.Method == http.MethodGet {
			items := make([]map[string]any, 0, len(f.tokens))
			for _, item := range f.tokens {
				items = append(items, item)
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": items})
			return
		}
		http.NotFound(w, r)
		return
	default:
		http.NotFound(w, r)
	}
}

func idToString(v any) string {
	switch n := v.(type) {
	case int:
		return itoa(int64(n))
	case int64:
		return itoa(n)
	case float64:
		return itoa(int64(n))
	default:
		return ""
	}
}

func tokenFixtureWithPlatform(t *testing.T, db *store.DB, name, siteURL, platform string) (int64, int64) {
	t.Helper()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)`,
		name, siteURL, platform, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT site: %v", err)
	}
	siteID, _ := res.LastInsertId()
	extraConfig := `{"credentialMode":"session","platformUserId":1}`
	username := "42"
	res, err = db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, status, is_pinned, sort_order,
		 checkin_enabled, extra_config, created_at, updated_at)
		 VALUES (?, ?, 'session-access-token', 'active', 0, 0, 1, ?, ?, ?)`,
		siteID, &username, &extraConfig, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT account: %v", err)
	}
	accountID, _ := res.LastInsertId()
	return siteID, accountID
}

func TestTokens_CreateUpstream_AndSync(t *testing.T) {
	db, r := setupTokensTest(t)
	fake := newFakeNewAPIUpstream()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	_, accountID := tokenFixtureWithPlatform(t, db, "NewAPI Site", srv.URL, "new-api")

	resp := doPostJSON(t, r, "/api/account-tokens", map[string]any{
		"accountId": accountID,
		"name":      "from-admin",
		"group":     "default",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("upstream create: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result["success"] != true {
		t.Fatalf("expected success=true, got %#v", result)
	}
	if result["synced"] != true {
		t.Fatalf("expected synced=true, got %#v", result)
	}
	if created, _ := result["created"].(float64); created < 1 {
		t.Fatalf("expected created>=1, got %#v", result["created"])
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM account_tokens WHERE account_id = ?", accountID).Scan(&count); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count < 1 {
		t.Fatalf("expected local tokens after upstream create, got %d", count)
	}
}

func TestTokens_SyncAccount_FromUpstream(t *testing.T) {
	db, r := setupTokensTest(t)
	fake := newFakeNewAPIUpstream()
	fake.tokens["sk-remote-1"] = map[string]any{
		"id": 1, "name": "remote-1", "key": "sk-remote-1", "status": 1, "group": "default",
	}
	fake.nextID = 2
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	_, accountID := tokenFixtureWithPlatform(t, db, "NewAPI Sync Site", srv.URL, "new-api")

	resp := doPostJSON(t, r, "/api/account-tokens/sync/"+itoa(accountID), nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["status"] != "synced" {
		t.Fatalf("expected status=synced, got %#v", result)
	}
	if created, _ := result["created"].(float64); created != 1 {
		t.Fatalf("expected created=1, got %#v", result)
	}

	var tokenVal string
	if err := db.QueryRow("SELECT token FROM account_tokens WHERE account_id = ? LIMIT 1", accountID).Scan(&tokenVal); err != nil {
		t.Fatalf("read token: %v", err)
	}
	if tokenVal != "sk-remote-1" {
		t.Fatalf("token = %q, want sk-remote-1", tokenVal)
	}
}

func TestTokens_Delete_UpstreamFirst(t *testing.T) {
	db, r := setupTokensTest(t)
	fake := newFakeNewAPIUpstream()
	fake.tokens["sk-delete-me"] = map[string]any{
		"id": 9, "name": "delete-me", "key": "sk-delete-me", "status": 1, "group": "default",
	}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	_, accountID := tokenFixtureWithPlatform(t, db, "NewAPI Delete Site", srv.URL, "new-api")
	tokID := createTokenFixture(t, db, accountID, "delete-me", "sk-delete-me", "default", true, true)

	resp := doDelete(t, r, "/api/account-tokens/"+itoa(tokID))
	if resp.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", resp.Code, resp.Body.String())
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM account_tokens WHERE id = ?", tokID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected local token deleted, got %d", count)
	}
	fake.mu.Lock()
	_, stillThere := fake.tokens["sk-delete-me"]
	fake.mu.Unlock()
	if stillThere {
		t.Fatal("expected upstream token deleted")
	}
}

func TestTokens_Delete_MaskedSkipsUpstream(t *testing.T) {
	db, r := setupTokensTest(t)
	fake := newFakeNewAPIUpstream()
	fake.tokens["sk-real-but-local-masked"] = map[string]any{
		"id": 3, "name": "masked", "key": "sk-real-but-local-masked", "status": 1,
	}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	_, accountID := tokenFixtureWithPlatform(t, db, "NewAPI Masked Delete", srv.URL, "new-api")
	tokID := createTokenFixture(t, db, accountID, "masked", "sk-abc***123", "", false, false)

	resp := doDelete(t, r, "/api/account-tokens/"+itoa(tokID))
	if resp.Code != http.StatusOK {
		t.Fatalf("delete masked: %d %s", resp.Code, resp.Body.String())
	}
	fake.mu.Lock()
	if len(fake.tokens) != 1 {
		t.Fatalf("upstream tokens should be untouched for masked local token, got %d", len(fake.tokens))
	}
	fake.mu.Unlock()
}

func TestTokens_SyncAll_WaitWithUpstream(t *testing.T) {
	db, r := setupTokensTest(t)
	fake := newFakeNewAPIUpstream()
	fake.tokens["sk-all-1"] = map[string]any{
		"id": 1, "name": "all-1", "key": "sk-all-1", "status": 1,
	}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	_, _ = tokenFixtureWithPlatform(t, db, "NewAPI SyncAll", srv.URL, "new-api")

	resp := doPostJSON(t, r, "/api/account-tokens/sync-all", map[string]any{"wait": true})
	if resp.Code != http.StatusOK {
		t.Fatalf("sync-all: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	summary, _ := result["summary"].(map[string]any)
	if summary == nil {
		t.Fatalf("missing summary: %#v", result)
	}
	if total, _ := summary["total"].(float64); total < 1 {
		t.Fatalf("expected total>=1, got %#v", summary)
	}
	if synced, _ := summary["synced"].(float64); synced < 1 {
		t.Fatalf("expected synced>=1, got %#v", summary)
	}
}

func TestTokens_CreateUpstream_MissingAdapter(t *testing.T) {
	db, r := setupTokensTest(t)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	res, _ := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES ('NoAdapter', 'https://no-adapter.example.com', 'not-a-platform', 'active', ?, ?)`,
		now, now,
	)
	siteID, _ := res.LastInsertId()
	extra := `{"credentialMode":"session"}`
	res, _ = db.Exec(
		`INSERT INTO accounts (site_id, access_token, status, checkin_enabled, extra_config, created_at, updated_at)
		 VALUES (?, 'session-token', 'active', 1, ?, ?, ?)`,
		siteID, &extra, now, now,
	)
	accountID, _ := res.LastInsertId()

	resp := doPostJSON(t, r, "/api/account-tokens", map[string]any{"accountId": accountID, "name": "x"})
	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for missing adapter, got %d %s", resp.Code, resp.Body.String())
	}
}
