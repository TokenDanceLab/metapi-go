package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func setupUpdateCenterTest(t *testing.T) chi.Router {
	t.Helper()
	r := chi.NewRouter()
	RegisterUpdateCenterRoutes(r)
	return r
}

func TestUpdateCenterDeploy_RequiresTargetTag(t *testing.T) {
	r := setupUpdateCenterTest(t)

	resp := doPostJSON(t, r, "/api/update-center/deploy", map[string]any{
		"source": "docker-hub-tag",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("success=%v, want false", body["success"])
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "targetTag") {
		t.Fatalf("message=%q, want targetTag required", msg)
	}
}

func TestUpdateCenterDeploy_Honest501NoStubTask(t *testing.T) {
	r := setupUpdateCenterTest(t)

	resp := doPostJSON(t, r, "/api/update-center/deploy", map[string]any{
		"targetTag": "v1.2.3",
	})
	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s, want 501", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("success=%v, want false (no fake success theater)", body["success"])
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Fatalf("expected residual message: %v", body)
	}
	if residual, _ := body["residual"].(string); residual == "" {
		t.Fatalf("expected residual field: %v", body)
	}
	raw := resp.Body.String()
	if strings.Contains(raw, "stub-deploy") {
		t.Fatalf("must not invent stub-deploy task id: %s", raw)
	}
	if strings.Contains(raw, `"task"`) {
		t.Fatalf("must not return fake task object: %s", raw)
	}
}

func TestUpdateCenterDeploy_AcceptsTargetVersionAlias(t *testing.T) {
	r := setupUpdateCenterTest(t)

	// targetVersion alone is enough to pass validation, then residual 501.
	resp := doPostJSON(t, r, "/api/update-center/deploy", map[string]any{
		"targetVersion": "1.0.0",
	})
	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s, want 501", resp.Code, resp.Body.String())
	}
}

func TestUpdateCenterRollback_RequiresTargetRevision(t *testing.T) {
	r := setupUpdateCenterTest(t)

	resp := doPostJSON(t, r, "/api/update-center/rollback", map[string]any{})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", resp.Code, resp.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if body["success"] != false {
		t.Fatalf("success=%v, want false", body["success"])
	}
}

func TestUpdateCenterRollback_Honest501NoStubTask(t *testing.T) {
	r := setupUpdateCenterTest(t)

	resp := doPostJSON(t, r, "/api/update-center/rollback", map[string]any{
		"targetRevision": "42",
	})
	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s, want 501", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("success=%v, want false", body["success"])
	}
	if residual, _ := body["residual"].(string); residual == "" {
		t.Fatalf("expected residual field: %v", body)
	}
	raw := resp.Body.String()
	if strings.Contains(raw, "stub-rollback") {
		t.Fatalf("must not invent stub-rollback task id: %s", raw)
	}
}

func TestUpdateCenterTaskStream_Honest501(t *testing.T) {
	r := setupUpdateCenterTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/update-center/tasks/any-id/stream", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s, want 501", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"status":"stub"`) {
		t.Fatalf("must not emit fake SSE stub done: %s", rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("success=%v, want false", body["success"])
	}
}

func TestUpdateCenterStatusAndCheck_LocalOnly(t *testing.T) {
	r := setupUpdateCenterTest(t)

	statusResp := doGet(t, r, "/api/update-center/status")
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status endpoint=%d body=%s", statusResp.Code, statusResp.Body.String())
	}
	checkResp := doPostJSON(t, r, "/api/update-center/check", map[string]any{})
	if checkResp.Code != http.StatusOK {
		t.Fatalf("check endpoint=%d body=%s", checkResp.Code, checkResp.Body.String())
	}
}
