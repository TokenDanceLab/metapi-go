package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func seedExportDownstreamKey(t *testing.T) (keyID int64, key, name string, r chi.Router) {
	t.Helper()
	db, router := setupDownstreamKeysTest(t)
	now := time.Now().UTC().Format(time.RFC3339)
	name = "export-client"
	key = "sk-export-fixture-abcdef"
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		(name, key, description, enabled, used_cost, used_requests, created_at, updated_at)
		VALUES (?, ?, 'export fixture', 1, 0, 0, ?, ?)`,
		name, key, now, now,
	)
	if err != nil {
		t.Fatalf("insert downstream key: %v", err)
	}
	keyID, err = res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return keyID, key, name, router
}

func TestDownstreamKeysExport_OpenAIProfile(t *testing.T) {
	keyID, key, name, r := seedExportDownstreamKey(t)

	resp := doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export?profile=openai&baseUrl=https://metapi.example.com")
	if resp.Code != http.StatusOK {
		t.Fatalf("export returned %d: %s", resp.Code, resp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("success=%v", body["success"])
	}
	if body["formatVersion"] != credentialExportFormatVersion {
		t.Fatalf("formatVersion=%v, want %s", body["formatVersion"], credentialExportFormatVersion)
	}
	if body["baseUrl"] != "https://metapi.example.com" {
		t.Fatalf("baseUrl=%v", body["baseUrl"])
	}
	if body["keyName"] != name {
		t.Fatalf("keyName=%v", body["keyName"])
	}

	profiles, ok := body["profiles"].([]any)
	if !ok || len(profiles) != 1 {
		t.Fatalf("profiles=%#v, want single openai profile", body["profiles"])
	}
	profile, _ := profiles[0].(map[string]any)
	if profile["id"] != exportProfileOpenAI {
		t.Fatalf("profile id=%v", profile["id"])
	}
	if profile["format"] != "env" {
		t.Fatalf("format=%v", profile["format"])
	}
	content, _ := profile["content"].(map[string]any)
	if content["OPENAI_API_KEY"] != key {
		t.Fatalf("OPENAI_API_KEY=%v, want fixture key", content["OPENAI_API_KEY"])
	}
	if content["OPENAI_BASE_URL"] != "https://metapi.example.com/v1" {
		t.Fatalf("OPENAI_BASE_URL=%v", content["OPENAI_BASE_URL"])
	}
	snippet, _ := profile["snippet"].(string)
	if !strings.Contains(snippet, "OPENAI_API_KEY="+key) {
		t.Fatalf("snippet missing key: %q", snippet)
	}
	if !strings.Contains(snippet, "OPENAI_BASE_URL=https://metapi.example.com/v1") {
		t.Fatalf("snippet missing base: %q", snippet)
	}
}

func TestDownstreamKeysExport_AllProfilesAndNoExtraSecrets(t *testing.T) {
	keyID, key, _, r := seedExportDownstreamKey(t)

	resp := doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export?baseUrl=https://gateway.example.org/")
	if resp.Code != http.StatusOK {
		t.Fatalf("export returned %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["baseUrl"] != "https://gateway.example.org" {
		t.Fatalf("baseUrl normalized=%v", body["baseUrl"])
	}
	profiles, ok := body["profiles"].([]any)
	if !ok || len(profiles) != 3 {
		t.Fatalf("want 3 profiles, got %#v", body["profiles"])
	}

	ids := map[string]bool{}
	raw := resp.Body.String()
	// Export must only surface the downstream key already readable by admin —
	// never invent upstream tokens / passwords / other secrets.
	forbidden := []string{"session-token", "sk-default-key", "sk-token-key", "password", "client_secret"}
	for _, bad := range forbidden {
		if strings.Contains(raw, bad) {
			t.Fatalf("export leaked unexpected secret material %q", bad)
		}
	}
	if !strings.Contains(raw, key) {
		t.Fatal("export missing the downstream key value")
	}

	for _, p := range profiles {
		m, _ := p.(map[string]any)
		id, _ := m["id"].(string)
		ids[id] = true
		if m["snippet"] == nil || m["content"] == nil {
			t.Fatalf("profile %s missing snippet/content: %#v", id, m)
		}
	}
	for _, want := range []string{exportProfileOpenAI, exportProfileCherry, exportProfileGeneric} {
		if !ids[want] {
			t.Fatalf("missing profile %s in %#v", want, ids)
		}
	}
}

func TestDownstreamKeysExport_CherryAndGenericShapes(t *testing.T) {
	keyID, key, name, r := seedExportDownstreamKey(t)

	resp := doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export?profile=cherry&baseUrl=http://127.0.0.1:4000")
	if resp.Code != http.StatusOK {
		t.Fatalf("cherry export %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	json.Unmarshal(resp.Body.Bytes(), &body)
	profiles := body["profiles"].([]any)
	p := profiles[0].(map[string]any)
	content := p["content"].(map[string]any)
	if content["apiKey"] != key || content["apiHost"] != "http://127.0.0.1:4000" {
		t.Fatalf("cherry content=%#v", content)
	}
	if content["name"] != name {
		t.Fatalf("cherry name=%v", content["name"])
	}

	resp = doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export?profile=generic&baseUrl=https://metapi.example.com")
	if resp.Code != http.StatusOK {
		t.Fatalf("generic export %d: %s", resp.Code, resp.Body.String())
	}
	json.Unmarshal(resp.Body.Bytes(), &body)
	profiles = body["profiles"].([]any)
	p = profiles[0].(map[string]any)
	content = p["content"].(map[string]any)
	if content["apiKey"] != key {
		t.Fatalf("generic apiKey=%v", content["apiKey"])
	}
	if content["openaiBaseUrl"] != "https://metapi.example.com/v1" {
		t.Fatalf("generic openaiBaseUrl=%v", content["openaiBaseUrl"])
	}
	if content["openaiCompatible"] != true {
		t.Fatalf("openaiCompatible=%v", content["openaiCompatible"])
	}
}

func TestDownstreamKeysExport_InvalidProfileAndMissingKey(t *testing.T) {
	keyID, _, _, r := seedExportDownstreamKey(t)

	resp := doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export?profile=webdav&baseUrl=https://x.example")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile returned %d, want 400: %s", resp.Code, resp.Body.String())
	}

	resp = doGet(t, r, "/api/downstream-keys/999999/export?baseUrl=https://x.example")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("missing key returned %d, want 404: %s", resp.Code, resp.Body.String())
	}

	resp = doGet(t, r, "/api/downstream-keys/not-an-id/export?baseUrl=https://x.example")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("bad id returned %d, want 400: %s", resp.Code, resp.Body.String())
	}
}

func TestDownstreamKeysExport_PublicBaseURLEnv(t *testing.T) {
	keyID, _, _, r := seedExportDownstreamKey(t)
	t.Setenv("PUBLIC_BASE_URL", "https://from-env.example")
	// Ensure query baseUrl still wins when present.
	resp := doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export?profile=openai&baseUrl=https://from-query.example")
	if resp.Code != http.StatusOK {
		t.Fatalf("export %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	json.Unmarshal(resp.Body.Bytes(), &body)
	if body["baseUrl"] != "https://from-query.example" {
		t.Fatalf("query override failed: %v", body["baseUrl"])
	}

	resp = doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export?profile=openai")
	if resp.Code != http.StatusOK {
		t.Fatalf("env export %d: %s", resp.Code, resp.Body.String())
	}
	json.Unmarshal(resp.Body.Bytes(), &body)
	if body["baseUrl"] != "https://from-env.example" {
		t.Fatalf("env baseUrl=%v", body["baseUrl"])
	}
}

func TestDownstreamKeysExport_RequestHostFallback(t *testing.T) {
	keyID, _, _, r := seedExportDownstreamKey(t)
	// Clear env so request host is used.
	t.Setenv("PUBLIC_BASE_URL", "")
	_ = os.Unsetenv("PUBLIC_BASE_URL")

	req := httptest.NewRequest("GET", "/api/downstream-keys/"+itoa(keyID)+"/export?profile=generic", nil)
	req.Host = "admin.metapi.local:4000"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("host export %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["baseUrl"] != "https://admin.metapi.local:4000" {
		t.Fatalf("host baseUrl=%v", body["baseUrl"])
	}
}

func TestDownstreamKeysExport_SettingsPublicBaseURL(t *testing.T) {
	db, r := setupDownstreamKeysTest(t)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		(name, key, enabled, used_cost, used_requests, created_at, updated_at)
		VALUES ('settings-export', 'sk-settings-export-key', 1, 0, 0, ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}
	keyID, _ := res.LastInsertId()

	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "public_base_url", `"https://from-settings.example"`); err != nil {
		t.Fatalf("insert settings: %v", err)
	}
	t.Setenv("PUBLIC_BASE_URL", "")
	_ = os.Unsetenv("PUBLIC_BASE_URL")

	resp := doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export?profile=openai")
	if resp.Code != http.StatusOK {
		t.Fatalf("settings export %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	json.Unmarshal(resp.Body.Bytes(), &body)
	if body["baseUrl"] != "https://from-settings.example" {
		t.Fatalf("settings baseUrl=%v", body["baseUrl"])
	}
}

func TestNormalizeExportBaseURL(t *testing.T) {
	got, errMsg := normalizeExportBaseURL("https://a.example/v1/extra?x=1")
	if errMsg != "" || got != "https://a.example" {
		t.Fatalf("path strip => (%q, %q)", got, errMsg)
	}
	got, errMsg = normalizeExportBaseURL("metapi.example.com")
	if errMsg != "" || got != "https://metapi.example.com" {
		t.Fatalf("scheme-less => (%q, %q)", got, errMsg)
	}
	_, errMsg = normalizeExportBaseURL("ftp://bad")
	if errMsg == "" {
		t.Fatal("expected ftp reject")
	}
}

func TestBuildCredentialExportProfilesFilter(t *testing.T) {
	all := buildCredentialExportProfiles("n", "sk-abc123", "https://x.example", "")
	if len(all) != 3 {
		t.Fatalf("all len=%d", len(all))
	}
	one := buildCredentialExportProfiles("n", "sk-abc123", "https://x.example", "generic")
	if len(one) != 1 || one[0]["id"] != exportProfileGeneric {
		t.Fatalf("filter generic => %#v", one)
	}
}
