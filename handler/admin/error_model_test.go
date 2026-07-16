package admin

import (
	"encoding/json"
	"net/http"
	"testing"
)

// B3: high-traffic admin create/update mutation failures must use non-2xx
// status codes with camelCase {"error":"..."} bodies (no silent HTTP 200).

func TestB3_SitesCreate_InvalidPayload_StatusAndErrorJSON(t *testing.T) {
	_, r := setupSitesTest(t)
	resp := doPostJSON(t, r, "/api/sites", map[string]any{"name": "", "url": ""})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", resp.Code, resp.Body.String())
	}
	assertUnifiedErrorBody(t, resp.Body.Bytes())
}

func TestB3_SitesCreate_MissingName_StatusAndErrorJSON(t *testing.T) {
	_, r := setupSitesTest(t)
	resp := doPostJSON(t, r, "/api/sites", map[string]any{"name": "  ", "url": "https://api.openai.com"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", resp.Code, resp.Body.String())
	}
	assertUnifiedErrorBody(t, resp.Body.Bytes())
}

func TestB3_SitesUpdate_InvalidID_StatusAndErrorJSON(t *testing.T) {
	_, r := setupSitesTest(t)
	resp := doPutJSON(t, r, "/api/sites/not-a-number", map[string]any{"name": "x"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", resp.Code, resp.Body.String())
	}
	assertUnifiedErrorBody(t, resp.Body.Bytes())
}

func TestB3_AccountsCreate_InvalidPayload_StatusAndErrorJSON(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	resp := doPostJSON(t, r, "/api/accounts", map[string]any{"siteId": 0})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", resp.Code, resp.Body.String())
	}
	assertUnifiedErrorBody(t, resp.Body.Bytes())
}

func TestB3_AccountsUpdate_InvalidID_StatusAndErrorJSON(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	resp := doPutJSON(t, r, "/api/accounts/abc", map[string]any{"status": "active"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", resp.Code, resp.Body.String())
	}
	assertUnifiedErrorBody(t, resp.Body.Bytes())
}

func TestB3_AccountsLogin_SiteNotFound_NotSilent200(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	resp := doPostJSON(t, r, "/api/accounts/login", map[string]any{
		"siteId":   99999,
		"username": "u",
		"password": "p",
	})
	if resp.Code == http.StatusOK {
		t.Fatalf("login failure must not return HTTP 200, body=%s", resp.Body.String())
	}
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", resp.Code, resp.Body.String())
	}
	assertUnifiedErrorBody(t, resp.Body.Bytes())
}

func TestB3_DownstreamKeysCreate_InvalidKey_StatusAndErrorJSON(t *testing.T) {
	_, r := setupDownstreamKeysTest(t)
	resp := doPostJSON(t, r, "/api/downstream-keys", map[string]any{
		"name": "demo",
		"key":  "bad",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", resp.Code, resp.Body.String())
	}
	assertUnifiedErrorBody(t, resp.Body.Bytes())
}

func TestB3_DownstreamKeysUpdate_InvalidID_StatusAndErrorJSON(t *testing.T) {
	_, r := setupDownstreamKeysTest(t)
	resp := doPutJSON(t, r, "/api/downstream-keys/0", map[string]any{"name": "x"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", resp.Code, resp.Body.String())
	}
	assertUnifiedErrorBody(t, resp.Body.Bytes())
}

func assertUnifiedErrorBody(t *testing.T, raw []byte) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal error body: %v raw=%s", err, string(raw))
	}
	errVal, ok := body["error"].(string)
	if !ok || errVal == "" {
		t.Fatalf("expected non-empty camelCase error field, got %#v", body)
	}
	// Unified mutation errors should not rely on success:false under HTTP 200.
	if success, has := body["success"]; has && success == true {
		t.Fatalf("error body must not claim success=true: %#v", body)
	}
}
