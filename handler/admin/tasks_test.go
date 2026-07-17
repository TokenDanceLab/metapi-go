package admin

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func setupTasksTest(t *testing.T) chi.Router {
	t.Helper()
	resetBackgroundTasksForTests()
	r := chi.NewRouter()
	RegisterTasksRoutes(r, nil)
	return r
}

// StartBackgroundTask must create a process-local registry entry that list/get
// can observe by id. This is intentionally not a multi-instance job store.
func TestStartBackgroundTask_ProcessLocalGetByID(t *testing.T) {
	r := setupTasksTest(t)

	var ran atomic.Bool
	task, reused := StartBackgroundTask(BackgroundTaskStartOptions{
		Type:  "test-process-local",
		Title: "process-local task",
	}, func() (any, error) {
		ran.Store(true)
		return map[string]any{"ok": true}, nil
	})
	if reused {
		t.Fatal("expected new task, got reused")
	}
	if task == nil || task.ID == "" {
		t.Fatalf("expected task with id, got %#v", task)
	}

	// Immediate get must find the process-local entry (pending or running).
	getResp := doGet(t, r, "/api/tasks/"+task.ID)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s, want 200", getResp.Code, getResp.Body.String())
	}
	var getBody map[string]any
	if err := json.Unmarshal(getResp.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if getBody["success"] != true {
		t.Fatalf("success=%v, want true", getBody["success"])
	}
	got, _ := getBody["task"].(map[string]any)
	if got == nil {
		t.Fatalf("missing task in body: %v", getBody)
	}
	if got["id"] != task.ID {
		t.Fatalf("task id=%v, want %s", got["id"], task.ID)
	}
	if got["type"] != "test-process-local" {
		t.Fatalf("type=%v, want test-process-local", got["type"])
	}

	// List must include this process's task.
	listResp := doGet(t, r, "/api/tasks?limit=50")
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listBody map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	tasks, _ := listBody["tasks"].([]any)
	found := false
	for _, item := range tasks {
		m, _ := item.(map[string]any)
		if m != nil && m["id"] == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("list did not include process-local task %s: %v", task.ID, listBody)
	}

	// Unknown id is not found (no cross-registry magic).
	miss := doGet(t, r, "/api/tasks/does-not-exist")
	if miss.Code != http.StatusNotFound {
		t.Fatalf("unknown id status=%d, want 404", miss.Code)
	}

	// Wait for runner completion within this process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ran.Load() {
			resp := doGet(t, r, "/api/tasks/"+task.ID)
			var body map[string]any
			if err := json.Unmarshal(resp.Body.Bytes(), &body); err == nil {
				if taskMap, _ := body["task"].(map[string]any); taskMap != nil {
					if taskMap["status"] == "succeeded" {
						return
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ran.Load() {
		t.Fatal("runner never executed in this process")
	}
	// Status may still be running briefly; existence + runner is the residual contract.
}

func TestStartBackgroundTask_DedupeReusesPendingInSameProcess(t *testing.T) {
	_ = setupTasksTest(t)

	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	first, reused := StartBackgroundTask(BackgroundTaskStartOptions{
		Type:      "test-dedupe",
		Title:     "dedupe task",
		DedupeKey: "same-key",
	}, func() (any, error) {
		<-block
		return nil, nil
	})
	if reused {
		t.Fatal("first start should not be reused")
	}

	second, reused := StartBackgroundTask(BackgroundTaskStartOptions{
		Type:      "test-dedupe",
		Title:     "dedupe task",
		DedupeKey: "same-key",
	}, func() (any, error) {
		t.Error("second runner should not start while first is active")
		return nil, nil
	})
	if !reused {
		t.Fatal("expected reuse for same dedupe key in this process")
	}
	if second.ID != first.ID {
		t.Fatalf("reused id=%s, want %s", second.ID, first.ID)
	}
}
