package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

func setupMaintenanceTest(t *testing.T) (*store.DB, chi.Router, *routing.RouteCache) {
	t.Helper()
	resetBackgroundTasksForTests()
	// Keep the process-lifetime singleton; only clear contents (#328).
	globalAccountsCache.clear()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Seed a warm accounts snapshot so clear-cache can prove invalidation.
	globalAccountsCache.set([]byte(`{"accounts":[],"sites":[]}`))

	// Seed a warm route cache so clear-cache can prove invalidation.
	rc := routing.NewRouteCache(5_000)
	rc.SetRoutes([]store.TokenRoute{{ID: 1, ModelPattern: "gpt-*"}})
	routing.SetGlobalCache(rc)
	t.Cleanup(func() { routing.SetGlobalCache(nil) })

	r := chi.NewRouter()
	RegisterMaintenanceRoutes(r, db.DB)
	RegisterTasksRoutes(r, db.DB)
	return db, r, rc
}

func TestMaintenanceClearCache_RealInvalidationAndRealJob(t *testing.T) {
	db, r, rc := setupMaintenanceTest(t)

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO token_routes (model_pattern, enabled, created_at, updated_at)
		VALUES ('gpt-*', 1, ?, ?)`, now, now); err != nil {
		t.Fatalf("seed token_routes: %v", err)
	}

	if !globalAccountsCache.isValid() {
		t.Fatal("accounts snapshot cache should be warm before clear-cache")
	}
	if rc.GetRoutes() == nil {
		t.Fatal("route cache should be warm before clear-cache")
	}

	resp := doPostJSON(t, r, "/api/settings/maintenance/clear-cache", map[string]any{})
	if resp.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", resp.Code, resp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("success=%v, want true", body["success"])
	}
	if body["queued"] != true {
		t.Fatalf("queued=%v, want true", body["queued"])
	}

	deletedRoutes, _ := body["deletedTokenRoutes"].(float64)
	if deletedRoutes < 1 {
		t.Fatalf("deletedTokenRoutes=%v, want >= 1", body["deletedTokenRoutes"])
	}

	jobID, _ := body["jobId"].(string)
	if jobID == "" || jobID == "stub-clear-cache" {
		t.Fatalf("jobId still stub/empty: %v", body["jobId"])
	}
	if taskID, _ := body["taskId"].(string); taskID != jobID {
		t.Fatalf("taskId=%v jobId=%v, want match", body["taskId"], jobID)
	}

	// In-process caches must be invalidated immediately.
	if globalAccountsCache.isValid() {
		t.Fatal("accounts snapshot cache should be cleared")
	}
	if routes := rc.GetRoutes(); routes != nil {
		t.Fatalf("route cache should be invalidated, got %d routes", len(routes))
	}

	// Durable rows wiped.
	var routeCount int64
	if err := db.Get(&routeCount, "SELECT COUNT(*) FROM token_routes"); err != nil {
		t.Fatalf("count token_routes: %v", err)
	}
	if routeCount != 0 {
		t.Fatalf("token_routes count=%d, want 0", routeCount)
	}

	// Real background task completes.
	deadline := time.Now().Add(2 * time.Second)
	var task map[string]any
	for time.Now().Before(deadline) {
		getResp := doGet(t, r, "/api/tasks/"+jobID)
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
		t.Fatal("clear-cache task not found in registry")
	}
	if task["status"] != "succeeded" {
		t.Fatalf("task status=%v body=%v, want succeeded", task["status"], task)
	}
	if task["type"] != clearCacheTaskType {
		t.Fatalf("task type=%v, want %s", task["type"], clearCacheTaskType)
	}
}

func TestMaintenanceClearCache_NoFakeStubJobID(t *testing.T) {
	_, r, _ := setupMaintenanceTest(t)

	resp := doPostJSON(t, r, "/api/settings/maintenance/clear-cache", nil)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	raw := resp.Body.String()
	if strings.Contains(raw, "stub-clear-cache") {
		t.Fatalf("response still contains stub job id: %s", raw)
	}
	if strings.Contains(raw, `"success":false`) {
		t.Fatalf("unexpected failure: %s", raw)
	}
}
