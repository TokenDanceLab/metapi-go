package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func setupOpsAdminStubsTest(t *testing.T) (*store.DB, chi.Router, *config.Config) {
	t.Helper()
	resetBackgroundTasksForTests()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{
		AuthToken: "ops-admin-test-token",
	}
	config.Set(cfg)

	r := chi.NewRouter()
	RegisterNotifyRoutes(r)
	RegisterMonitorRoutes(r, db.DB, cfg)
	RegisterTasksRoutes(r, db.DB)
	RegisterSiteAnnouncementsRoutes(r, db.DB)
	return db, r, cfg
}

func TestNotifyTest_NoChannelConfiguredReturns400(t *testing.T) {
	_, r, _ := setupOpsAdminStubsTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/notify/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "no notification channels configured") {
		t.Fatalf("message = %q, want clear channel configuration error", msg)
	}
}

func TestMonitorConfig_SaveAndGetRoundTrip(t *testing.T) {
	db, r, _ := setupOpsAdminStubsTest(t)

	cookie := "ld_auth_session=" + strings.Repeat("a", 24)
	putResp := doPutJSON(t, r, "/api/monitor/config", map[string]any{
		"ldohCookie": cookie,
	})
	if putResp.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", putResp.Code, putResp.Body.String())
	}
	var putBody map[string]any
	if err := json.Unmarshal(putResp.Body.Bytes(), &putBody); err != nil {
		t.Fatalf("decode put: %v", err)
	}
	if putBody["success"] != true || putBody["ldohCookieConfigured"] != true {
		t.Fatalf("unexpected put body: %v", putBody)
	}
	if putBody["message"] == "LDOH config stub — not yet implemented" {
		t.Fatalf("still returning stub message: %v", putBody)
	}

	getResp := doGet(t, r, "/api/monitor/config")
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getResp.Code, getResp.Body.String())
	}
	var getBody map[string]any
	if err := json.Unmarshal(getResp.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if getBody["ldohCookieConfigured"] != true {
		t.Fatalf("configured = %v, want true", getBody["ldohCookieConfigured"])
	}
	masked, _ := getBody["ldohCookieMasked"].(string)
	if masked == "" || strings.Contains(masked, strings.Repeat("a", 24)) {
		t.Fatalf("masked cookie not applied: %q", masked)
	}

	// Ensure value persisted in settings table.
	stored := getSettingValue(db.DB, ldohCookieSettingKey)
	if stored != cookie {
		t.Fatalf("stored cookie = %q, want %q", stored, cookie)
	}
}

func TestMonitorConfig_RejectsInvalidCookie(t *testing.T) {
	_, r, _ := setupOpsAdminStubsTest(t)

	resp := doPutJSON(t, r, "/api/monitor/config", map[string]any{
		"ldohCookie": "short",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
}

func TestMonitorLdohProxy_RequiresSessionAndCookie(t *testing.T) {
	_, r, cfg := setupOpsAdminStubsTest(t)

	// No session cookie → 401
	req := httptest.NewRequest(http.MethodGet, "/monitor-proxy/ldoh/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session status = %d body=%s, want 401", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("must not fake success for missing session: %s", rec.Body.String())
	}

	// Session present but LDOH cookie not configured → 400 plain text
	req2 := httptest.NewRequest(http.MethodGet, "/monitor-proxy/ldoh/", nil)
	req2.AddCookie(&http.Cookie{Name: monitorAuthCookie, Value: deriveMonitorSessionToken(cfg.AuthToken)})
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("no cookie status = %d body=%s, want 400", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "LDOH cookie not configured") {
		t.Fatalf("body = %q, want unconfigured cookie message", rec2.Body.String())
	}
}

func TestTasks_ListEmptySchemaAndGetMissing(t *testing.T) {
	_, r, _ := setupOpsAdminStubsTest(t)

	listResp := doGet(t, r, "/api/tasks?limit=10")
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listResp.Code, listResp.Body.String())
	}
	var listBody map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listBody["success"] != true {
		t.Fatalf("success = %v, want true", listBody["success"])
	}
	tasks, ok := listBody["tasks"].([]any)
	if !ok {
		t.Fatalf("tasks field missing/invalid: %v", listBody["tasks"])
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks len = %d, want 0", len(tasks))
	}

	missing := doGet(t, r, "/api/tasks/does-not-exist")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d body=%s, want 404", missing.Code, missing.Body.String())
	}
}

func TestTasks_StartAndListCamelCase(t *testing.T) {
	_, r, _ := setupOpsAdminStubsTest(t)

	var started sync.WaitGroup
	started.Add(1)
	task, reused := StartBackgroundTask(BackgroundTaskStartOptions{
		Type:      "unit-test",
		Title:     "单元测试任务",
		DedupeKey: "unit-test-key",
	}, func() (any, error) {
		started.Done()
		time.Sleep(20 * time.Millisecond)
		return map[string]any{"ok": true}, nil
	})
	if reused {
		t.Fatal("expected new task, got reused")
	}
	if task.ID == "" || task.Type != "unit-test" {
		t.Fatalf("unexpected task: %+v", task)
	}

	// Wait until runner begins so list can observe it.
	started.Wait()
	// Allow a short window for completion.
	deadline := time.Now().Add(2 * time.Second)
	var found map[string]any
	for time.Now().Before(deadline) {
		listResp := doGet(t, r, "/api/tasks?limit=20")
		var listBody map[string]any
		if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		tasks, _ := listBody["tasks"].([]any)
		for _, item := range tasks {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if m["id"] == task.ID {
				found = m
				break
			}
		}
		if found != nil {
			if found["status"] == "succeeded" || found["status"] == "failed" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if found == nil {
		t.Fatalf("task %s not found in list", task.ID)
	}
	for _, key := range []string{"id", "type", "title", "status", "message", "createdAt", "updatedAt", "dedupeKey"} {
		if _, ok := found[key]; !ok {
			t.Fatalf("missing camelCase field %q in %v", key, found)
		}
	}
	if _, ok := found["created_at"]; ok {
		t.Fatalf("unexpected snake_case created_at in task payload: %v", found)
	}

	getResp := doGet(t, r, "/api/tasks/"+task.ID)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getResp.Code, getResp.Body.String())
	}
	var getBody map[string]any
	if err := json.Unmarshal(getResp.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if getBody["success"] != true {
		t.Fatalf("get success = %v", getBody["success"])
	}
}

func TestSiteAnnouncementsSync_QueuesRealTask(t *testing.T) {
	db, r, _ := setupOpsAdminStubsTest(t)

	// Seed one active site with no adapter platform → unsupported counted by worker.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES ('Sync Site', 'https://sync-site.example.com', 'unknown-platform-x', 'active', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("seed site: %v", err)
	}

	resp := doPostJSON(t, r, "/api/site-announcements/sync", map[string]any{})
	if resp.Code != http.StatusOK {
		t.Fatalf("sync status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != true || body["queued"] != true {
		t.Fatalf("unexpected body: %v", body)
	}
	if body["taskId"] == "stub" || body["taskId"] == "" || body["taskId"] == nil {
		t.Fatalf("taskId still stub/empty: %v", body["taskId"])
	}
	taskID, _ := body["taskId"].(string)

	// Wait for background completion.
	deadline := time.Now().Add(2 * time.Second)
	var task map[string]any
	for time.Now().Before(deadline) {
		getResp := doGet(t, r, "/api/tasks/"+taskID)
		if getResp.Code != http.StatusOK {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var getBody map[string]any
		if err := json.Unmarshal(getResp.Body.Bytes(), &getBody); err != nil {
			t.Fatalf("decode task: %v", err)
		}
		task, _ = getBody["task"].(map[string]any)
		if task != nil {
			if task["status"] == "succeeded" || task["status"] == "failed" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task == nil {
		t.Fatal("task not found after sync")
	}
	if task["status"] != "succeeded" {
		t.Fatalf("task status = %v body=%v, want succeeded", task["status"], task)
	}
	if task["type"] != "site-announcements-sync" {
		t.Fatalf("task type = %v", task["type"])
	}
}
