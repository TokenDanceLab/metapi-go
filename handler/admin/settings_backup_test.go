package admin

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	backupsvc "github.com/tokendancelab/metapi-go/service/backup"
	"github.com/tokendancelab/metapi-go/store"
)

func setupBackupTestDB(t *testing.T) *store.DB {
	t.Helper()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func allowPrivateWebdavTargetsForTest(t *testing.T) {
	t.Helper()
	old := allowPrivateWebdavTargets
	allowPrivateWebdavTargets = true
	t.Cleanup(func() { allowPrivateWebdavTargets = old })
}

func setBackupImportLimitsForTest(t *testing.T, maxRows, maxColumns, maxCellBytes int) {
	t.Helper()
	oldMaxRows := backupImportMaxRowsPerTable
	oldMaxColumns := backupImportMaxColumnsPerRow
	oldMaxCellBytes := backupImportMaxCellBytes
	backupImportMaxRowsPerTable = maxRows
	backupImportMaxColumnsPerRow = maxColumns
	backupImportMaxCellBytes = maxCellBytes
	t.Cleanup(func() {
		backupImportMaxRowsPerTable = oldMaxRows
		backupImportMaxColumnsPerRow = oldMaxColumns
		backupImportMaxCellBytes = oldMaxCellBytes
	})
}

func setBackupExportLimitsForTest(t *testing.T, maxRows int, maxCellBytes int, maxPayloadBytes int64) {
	t.Helper()
	oldMaxRows := backupsvc.MaxExportRowsPerTable
	oldMaxCellBytes := backupsvc.MaxExportCellBytes
	oldMaxPayloadBytes := backupsvc.MaxExportPayloadBytes
	backupsvc.MaxExportRowsPerTable = maxRows
	backupsvc.MaxExportCellBytes = maxCellBytes
	backupsvc.MaxExportPayloadBytes = maxPayloadBytes
	t.Cleanup(func() {
		backupsvc.MaxExportRowsPerTable = oldMaxRows
		backupsvc.MaxExportCellBytes = oldMaxCellBytes
		backupsvc.MaxExportPayloadBytes = oldMaxPayloadBytes
	})
}

func TestImportTableRowsRejectsUnknownColumns(t *testing.T) {
	db := setupBackupTestDB(t)

	_, err := importTableRows(db.DB, "settings", []map[string]any{
		{
			"key":                      "safe-key",
			"value":                    "safe-value",
			"key) VALUES ('x','y') --": "malicious",
		},
	})
	if err == nil {
		t.Fatal("expected unknown column error")
	}
	if !strings.Contains(err.Error(), "unknown column") {
		t.Fatalf("error = %v, want unknown column", err)
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", "safe-key"); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("malformed import inserted %d rows, want 0", count)
	}
}

func TestImportTableRowsAllowsKnownColumns(t *testing.T) {
	db := setupBackupTestDB(t)

	n, err := importTableRows(db.DB, "settings", []map[string]any{
		{
			"key":   "theme",
			"value": "dark",
		},
	})
	if err != nil {
		t.Fatalf("importTableRows: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported = %d, want 1", n)
	}

	var value string
	if err := db.Get(&value, "SELECT value FROM settings WHERE key = ?", "theme"); err != nil {
		t.Fatalf("get imported setting: %v", err)
	}
	if value != "dark" {
		t.Fatalf("value = %q, want dark", value)
	}
}

func TestImportBackupUsesBackupPayloadLimitNotGenericAdminLimit(t *testing.T) {
	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	oldAdminLimit := adminJSONBodyLimitBytes
	oldBackupLimit := backupWebdavImportMaxBytes
	adminJSONBodyLimitBytes = 8
	backupWebdavImportMaxBytes = 1024
	t.Cleanup(func() {
		adminJSONBodyLimitBytes = oldAdminLimit
		backupWebdavImportMaxBytes = oldBackupLimit
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/import", strings.NewReader(
		`{"tables":{"settings":[{"key":"theme","value":"\"dark\""}]}}`,
	))
	rec := httptest.NewRecorder()

	handler.importBackup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var value string
	if err := db.Get(&value, "SELECT value FROM settings WHERE key = ?", "theme"); err != nil {
		t.Fatalf("get imported setting: %v", err)
	}
	if value != `"dark"` {
		t.Fatalf("value = %q, want dark JSON string", value)
	}
}

func TestImportBackupRejectsPayloadOverBackupLimit(t *testing.T) {
	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	oldBackupLimit := backupWebdavImportMaxBytes
	backupWebdavImportMaxBytes = 8
	t.Cleanup(func() { backupWebdavImportMaxBytes = oldBackupLimit })

	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/import", strings.NewReader(
		`{"tables":{"settings":[{"key":"theme","value":"\"dark\""}]}}`,
	))
	rec := httptest.NewRecorder()

	handler.importBackup(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings"); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("settings rows inserted after oversized import, count=%d", count)
	}
}

func TestExportBackupRejectsPayloadOverLimit(t *testing.T) {
	setBackupExportLimitsForTest(t, 50_000, 4<<20, 32)
	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/backup/export?type=preferences", nil)
	rec := httptest.NewRecorder()

	handler.exportBackup(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebdavConfigRoundTripMasksPassword(t *testing.T) {
	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	req := httptest.NewRequest(http.MethodPut, "/api/settings/backup/webdav", strings.NewReader(`{
		"enabled": true,
		"fileUrl": "https://dav.example.com/backups/metapi.json",
		"username": "alice",
		"password": "secret-pass",
		"exportType": "accounts",
		"autoSyncEnabled": true,
		"autoSyncCron": "0 */6 * * *"
	}`))
	rec := httptest.NewRecorder()

	handler.saveWebdavConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-pass") {
		t.Fatalf("save response leaked password: %s", rec.Body.String())
	}

	var stored string
	if err := db.Get(&stored, "SELECT value FROM settings WHERE key = ?", "backup_webdav_config_v1"); err != nil {
		t.Fatalf("read stored webdav config: %v", err)
	}
	if !strings.Contains(stored, "secret-pass") {
		t.Fatalf("stored config did not preserve password: %s", stored)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings/backup/webdav", nil)
	getRec := httptest.NewRecorder()
	handler.getWebdavConfig(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", getRec.Code, getRec.Body.String())
	}
	if strings.Contains(getRec.Body.String(), "secret-pass") {
		t.Fatalf("get response leaked password: %s", getRec.Body.String())
	}

	var payload struct {
		Config struct {
			Enabled         bool   `json:"enabled"`
			FileURL         string `json:"fileUrl"`
			Username        string `json:"username"`
			Password        string `json:"password"`
			PasswordMasked  string `json:"passwordMasked"`
			HasPassword     bool   `json:"hasPassword"`
			ExportType      string `json:"exportType"`
			AutoSyncEnabled bool   `json:"autoSyncEnabled"`
			AutoSyncCron    string `json:"autoSyncCron"`
		} `json:"config"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if !payload.Config.Enabled || payload.Config.FileURL != "https://dav.example.com/backups/metapi.json" || payload.Config.Username != "alice" {
		t.Fatalf("config = %+v, want saved fields", payload.Config)
	}
	if payload.Config.Password != "" {
		t.Fatalf("password field = %q, want empty", payload.Config.Password)
	}
	if !payload.Config.HasPassword || payload.Config.PasswordMasked == "" || payload.Config.PasswordMasked == "secret-pass" {
		t.Fatalf("password mask fields = has:%v masked:%q", payload.Config.HasPassword, payload.Config.PasswordMasked)
	}
	if payload.Config.ExportType != "accounts" || !payload.Config.AutoSyncEnabled || payload.Config.AutoSyncCron != "0 */6 * * *" {
		t.Fatalf("config = %+v, want saved sync fields", payload.Config)
	}
}

func TestExportToWebdavUploadsBackupPayload(t *testing.T) {
	allowPrivateWebdavTargetsForTest(t)

	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "theme", `"dark"`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}

	var observedMethod string
	var observedAuth string
	var observedContentType string
	var observedPayload map[string]any
	webdav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedMethod = r.Method
		observedAuth = r.Header.Get("Authorization")
		observedContentType = r.Header.Get("Content-Type")
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upload body: %v", err)
		}
		if err := json.Unmarshal(data, &observedPayload); err != nil {
			t.Fatalf("uploaded body is not JSON: %v; body=%s", err, string(data))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(webdav.Close)

	cfg := map[string]any{
		"enabled":         true,
		"fileUrl":         webdav.URL + "/backup.json",
		"username":        "alice",
		"password":        "secret-pass",
		"exportType":      "preferences",
		"autoSyncEnabled": false,
		"autoSyncCron":    "0 */6 * * *",
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "backup_webdav_config_v1", string(raw)); err != nil {
		t.Fatalf("insert webdav config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/webdav/export", strings.NewReader(`{"type":"preferences"}`))
	rec := httptest.NewRecorder()

	handler.exportToWebdav(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if observedMethod != http.MethodPut {
		t.Fatalf("method = %q, want PUT", observedMethod)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret-pass"))
	if observedAuth != wantAuth {
		t.Fatalf("Authorization = %q, want basic auth", observedAuth)
	}
	if !strings.Contains(observedContentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", observedContentType)
	}
	if observedPayload["type"] != "preferences" {
		t.Fatalf("uploaded type = %v, want preferences", observedPayload["type"])
	}
	tables, ok := observedPayload["tables"].(map[string]any)
	if !ok {
		t.Fatalf("uploaded tables = %#v, want object", observedPayload["tables"])
	}
	settingsRows, ok := tables["settings"].([]any)
	if !ok || len(settingsRows) == 0 {
		t.Fatalf("uploaded settings rows = %#v, want non-empty array", tables["settings"])
	}

	if strings.Contains(rec.Body.String(), "secret-pass") {
		t.Fatalf("export response leaked password: %s", rec.Body.String())
	}
}

func TestExportToWebdavRejectsOversizedPayloadBeforeUpload(t *testing.T) {
	allowPrivateWebdavTargetsForTest(t)
	setBackupExportLimitsForTest(t, 50_000, 4<<20, 32)

	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	called := false
	webdav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(webdav.Close)

	cfg := map[string]any{
		"enabled":      true,
		"fileUrl":      webdav.URL + "/backup.json",
		"exportType":   "preferences",
		"autoSyncCron": "0 */6 * * *",
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "backup_webdav_config_v1", string(raw)); err != nil {
		t.Fatalf("insert webdav config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/webdav/export", strings.NewReader(`{"type":"preferences"}`))
	rec := httptest.NewRecorder()

	handler.exportToWebdav(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("WebDAV server was called after export payload exceeded limit")
	}
}

func TestImportFromWebdavDownloadsAndImportsBackupPayload(t *testing.T) {
	allowPrivateWebdavTargetsForTest(t)

	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	backup := map[string]any{
		"metadata": map[string]any{
			"version": "1.0",
		},
		"type": "preferences",
		"tables": map[string]any{
			"settings": []map[string]any{
				{"key": "theme", "value": `"dark"`},
			},
		},
	}
	backupRaw, err := json.Marshal(backup)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}

	var observedMethod string
	var observedAuth string
	webdav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedMethod = r.Method
		observedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(backupRaw)
	}))
	t.Cleanup(webdav.Close)

	cfg := map[string]any{
		"enabled":      true,
		"fileUrl":      webdav.URL + "/backup.json",
		"username":     "alice",
		"password":     "secret-pass",
		"exportType":   "preferences",
		"autoSyncCron": "0 */6 * * *",
	}
	cfgRaw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "backup_webdav_config_v1", string(cfgRaw)); err != nil {
		t.Fatalf("insert webdav config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/webdav/import", nil)
	rec := httptest.NewRecorder()

	handler.importFromWebdav(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if observedMethod != http.MethodGet {
		t.Fatalf("method = %q, want GET", observedMethod)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret-pass"))
	if observedAuth != wantAuth {
		t.Fatalf("Authorization = %q, want basic auth", observedAuth)
	}

	var value string
	if err := db.Get(&value, "SELECT value FROM settings WHERE key = ?", "theme"); err != nil {
		t.Fatalf("read imported setting: %v", err)
	}
	if value != `"dark"` {
		t.Fatalf("value = %q, want dark JSON string", value)
	}
	if strings.Contains(rec.Body.String(), "secret-pass") {
		t.Fatalf("import response leaked password: %s", rec.Body.String())
	}

	var payload struct {
		Imported map[string]int64 `json:"imported"`
		State    struct {
			LastSyncAt string  `json:"lastSyncAt"`
			LastError  *string `json:"lastError"`
		} `json:"state"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Imported["settings"] != 1 {
		t.Fatalf("imported = %#v, want settings=1", payload.Imported)
	}
	if payload.State.LastSyncAt == "" || payload.State.LastError != nil {
		t.Fatalf("state = %+v, want successful sync state", payload.State)
	}
}

func TestImportFromWebdavRejectsOversizedBackupPayload(t *testing.T) {
	allowPrivateWebdavTargetsForTest(t)

	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	oldLimit := backupWebdavImportMaxBytes
	backupWebdavImportMaxBytes = 8
	t.Cleanup(func() { backupWebdavImportMaxBytes = oldLimit })

	webdav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tables":{"settings":[]}}`))
	}))
	t.Cleanup(webdav.Close)

	cfg := map[string]any{
		"enabled":      true,
		"fileUrl":      webdav.URL + "/backup.json",
		"password":     "secret-pass",
		"exportType":   "preferences",
		"autoSyncCron": "0 */6 * * *",
	}
	cfgRaw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "backup_webdav_config_v1", string(cfgRaw)); err != nil {
		t.Fatalf("insert webdav config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/webdav/import", nil)
	rec := httptest.NewRecorder()

	handler.importFromWebdav(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-pass") {
		t.Fatalf("response leaked password: %s", rec.Body.String())
	}
}

func TestImportFromWebdavFailurePreservesLastSuccessfulSync(t *testing.T) {
	allowPrivateWebdavTargetsForTest(t)

	db := setupBackupTestDB(t)
	handler := &backupHandler{db: db.DB}

	webdav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("remote failed for secret-pass"))
	}))
	t.Cleanup(webdav.Close)

	cfg := map[string]any{
		"enabled":      true,
		"fileUrl":      webdav.URL + "/backup.json",
		"password":     "secret-pass",
		"exportType":   "preferences",
		"autoSyncCron": "0 */6 * * *",
	}
	cfgRaw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "backup_webdav_config_v1", string(cfgRaw)); err != nil {
		t.Fatalf("insert webdav config: %v", err)
	}

	const previousSuccess = "2026-07-01T00:00:00Z"
	stateRaw := `{"lastSyncAt":"` + previousSuccess + `","lastAttemptAt":"2026-07-01T00:00:00Z","lastError":null}`
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "backup_webdav_state_v1", stateRaw); err != nil {
		t.Fatalf("insert webdav state: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/webdav/import", nil)
	rec := httptest.NewRecorder()

	handler.importFromWebdav(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-pass") {
		t.Fatalf("response leaked password: %s", rec.Body.String())
	}

	var payload struct {
		State struct {
			LastSyncAt    string  `json:"lastSyncAt"`
			LastAttemptAt string  `json:"lastAttemptAt"`
			LastError     *string `json:"lastError"`
		} `json:"state"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.State.LastSyncAt != previousSuccess {
		t.Fatalf("lastSyncAt = %q, want previous success %q", payload.State.LastSyncAt, previousSuccess)
	}
	if payload.State.LastAttemptAt == "" || payload.State.LastAttemptAt == previousSuccess {
		t.Fatalf("lastAttemptAt = %q, want fresh attempt time", payload.State.LastAttemptAt)
	}
	if payload.State.LastError == nil || strings.Contains(*payload.State.LastError, "secret-pass") {
		t.Fatalf("lastError = %v, want sanitized failure", payload.State.LastError)
	}

	var savedRaw string
	if err := db.Get(&savedRaw, "SELECT value FROM settings WHERE key = ?", "backup_webdav_state_v1"); err != nil {
		t.Fatalf("read saved state: %v", err)
	}
	var saved struct {
		LastSyncAt    string  `json:"lastSyncAt"`
		LastAttemptAt string  `json:"lastAttemptAt"`
		LastError     *string `json:"lastError"`
	}
	if err := json.Unmarshal([]byte(savedRaw), &saved); err != nil {
		t.Fatalf("decode saved state: %v", err)
	}
	if saved.LastSyncAt != previousSuccess || saved.LastAttemptAt == "" || saved.LastError == nil {
		t.Fatalf("saved state = %+v, want preserved success plus failed attempt", saved)
	}
}

func TestWebdavFileURLRejectsPrivateTargets(t *testing.T) {
	tests := []string{
		"http://localhost/backup.json",
		"http://localhost./backup.json",
		"http://127.0.0.1/backup.json",
		"http://[::1]/backup.json",
		"http://10.0.0.5/backup.json",
		"http://172.16.0.5/backup.json",
		"http://192.168.1.5/backup.json",
		"http://169.254.169.254/latest/meta-data",
		"http://[fe80::1]/backup.json",
		"http://224.0.0.1/backup.json",
		"http://0.0.0.0/backup.json",
	}
	for _, raw := range tests {
		if isValidWebdavFileURL(raw) {
			t.Fatalf("isValidWebdavFileURL(%q) = true, want false", raw)
		}
	}

	if !isValidWebdavFileURL("https://webdav.example.com/backups/metapi.json") {
		t.Fatal("expected public HTTPS WebDAV URL to be valid")
	}
}

func TestWebdavHTTPClientRejectsPrivateRedirectTarget(t *testing.T) {
	client := newWebdavHTTPClient()
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect is nil, want redirect validation")
	}

	from := httptest.NewRequest(http.MethodGet, "https://webdav.example.com/backup.json", nil)
	to := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/backup.json", nil)

	if err := client.CheckRedirect(to, []*http.Request{from}); err == nil {
		t.Fatal("redirect to loopback was allowed, want rejection")
	}
}

func TestWebdavHTTPTransportDoesNotUseEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")

	transport := newWebdavHTTPTransport()
	if transport.Proxy != nil {
		t.Fatal("WebDAV transport uses an environment proxy hook, want direct dial with SSRF checks")
	}
}

func TestImportBackupTablesRollsBackWhenLaterTableFails(t *testing.T) {
	db := setupBackupTestDB(t)

	tables := map[string]json.RawMessage{
		"settings": json.RawMessage(`[{"key":"partial-import-sentinel","value":"\"should-not-persist\""}]`),
		"downstream_api_keys": json.RawMessage(
			`[{"id":"key-1","key_hash":"hash-1","name":"bad-key","unknown_column":"boom"}]`,
		),
	}

	if _, err := importBackupTables(db.DB, tables); err == nil {
		t.Fatalf("importBackupTables succeeded, want failure on invalid downstream_api_keys column")
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", "partial-import-sentinel"); err != nil {
		t.Fatalf("count partial setting: %v", err)
	}
	if count != 0 {
		t.Fatalf("partial setting persisted after failed import, count=%d", count)
	}
}

func TestImportBackupTablesRejectsUnknownTableKey(t *testing.T) {
	db := setupBackupTestDB(t)

	tables := map[string]json.RawMessage{
		"settings_typo": json.RawMessage(`[{"key":"theme","value":"\"dark\""}]`),
	}

	if _, err := importBackupTables(db.DB, tables); err == nil {
		t.Fatal("importBackupTables succeeded, want unknown table error")
	} else if !strings.Contains(err.Error(), "未知表 settings_typo") {
		t.Fatalf("error = %v, want unknown table", err)
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings"); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("settings rows inserted after unknown table import, count=%d", count)
	}
}

func TestImportBackupTablesRejectsTooManyRowsBeforeInsert(t *testing.T) {
	setBackupImportLimitsForTest(t, 1, 128, 4<<20)
	db := setupBackupTestDB(t)

	tables := map[string]json.RawMessage{
		"settings": json.RawMessage(`[
			{"key":"theme","value":"\"dark\""},
			{"key":"language","value":"\"zh\""}
		]`),
	}

	if _, err := importBackupTables(db.DB, tables); err == nil {
		t.Fatal("importBackupTables succeeded, want row limit error")
	} else if !strings.Contains(err.Error(), "行数超过上限 1") {
		t.Fatalf("error = %v, want row limit", err)
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings"); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("settings rows inserted after row limit failure, count=%d", count)
	}
}

func TestImportBackupTablesRejectsTooManyColumnsBeforeInsert(t *testing.T) {
	setBackupImportLimitsForTest(t, 50_000, 1, 4<<20)
	db := setupBackupTestDB(t)

	tables := map[string]json.RawMessage{
		"settings": json.RawMessage(`[{"key":"theme","value":"\"dark\""}]`),
	}

	if _, err := importBackupTables(db.DB, tables); err == nil {
		t.Fatal("importBackupTables succeeded, want column limit error")
	} else if !strings.Contains(err.Error(), "exceeds limit 1") {
		t.Fatalf("error = %v, want column limit", err)
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings"); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("settings rows inserted after column limit failure, count=%d", count)
	}
}

func TestImportBackupTablesRejectsOversizedCellBeforeInsert(t *testing.T) {
	setBackupImportLimitsForTest(t, 50_000, 128, 4)
	db := setupBackupTestDB(t)

	tables := map[string]json.RawMessage{
		"settings": json.RawMessage(`[{"key":"theme","value":"12345"}]`),
	}

	if _, err := importBackupTables(db.DB, tables); err == nil {
		t.Fatal("importBackupTables succeeded, want cell size error")
	} else if !strings.Contains(err.Error(), "exceeds limit 4 bytes") {
		t.Fatalf("error = %v, want cell size limit", err)
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings"); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("settings rows inserted after cell size failure, count=%d", count)
	}
}

func TestImportBackupTablesSkipsRuntimeLocalSettings(t *testing.T) {
	db := setupBackupTestDB(t)

	tables := map[string]json.RawMessage{
		"settings": json.RawMessage(`[
			{"key":"theme","value":"\"dark\""},
			{"key":"auth_token","value":"\"remote-admin-token\""},
			{"key":"db_url","value":"\"postgres://remote.example/db\""}
		]`),
	}

	imported, err := importBackupTables(db.DB, tables)
	if err != nil {
		t.Fatalf("importBackupTables: %v", err)
	}
	if imported["settings"] != 1 {
		t.Fatalf("imported settings = %d, want only non-local setting", imported["settings"])
	}

	var theme string
	if err := db.Get(&theme, "SELECT value FROM settings WHERE key = ?", "theme"); err != nil {
		t.Fatalf("theme setting was not imported: %v", err)
	}
	if theme != `"dark"` {
		t.Fatalf("theme = %q, want dark JSON string", theme)
	}

	for _, key := range []string{"auth_token", "db_url"} {
		var count int
		if err := db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", key); err != nil {
			t.Fatalf("count %s: %v", key, err)
		}
		if count != 0 {
			t.Fatalf("runtime-local setting %s was imported", key)
		}
	}
}
