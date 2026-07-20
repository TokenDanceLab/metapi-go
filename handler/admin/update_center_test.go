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

	assertLocalUpdateStatus := func(t *testing.T, label string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s, want 200", label, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s decode: %v body=%s", label, err, rec.Body.String())
		}
		if body["updateAvailable"] != false {
			t.Fatalf("%s updateAvailable=%v, must stay false (no invented remote update)", label, body["updateAvailable"])
		}
		if body["currentVersion"] != "0.0.0" || body["latestVersion"] != "0.0.0" {
			t.Fatalf("%s versions=%v/%v, want local 0.0.0 placeholders", label, body["currentVersion"], body["latestVersion"])
		}
		if body["lastCheckedAt"] != nil {
			t.Fatalf("%s lastCheckedAt=%v, want nil (no fake poll timestamp)", label, body["lastCheckedAt"])
		}
		residual, _ := body["residual"].(string)
		if residual == "" {
			t.Fatalf("%s expected residual field for local stub honesty: %v", label, body)
		}
		if mode, _ := body["mode"].(string); mode != "external" {
			t.Fatalf("%s mode=%v, want external (UC-1)", label, body["mode"])
		}
	}

	statusResp := doGet(t, r, "/api/update-center/status")
	assertLocalUpdateStatus(t, "status", statusResp)

	checkResp := doPostJSON(t, r, "/api/update-center/check", map[string]any{})
	assertLocalUpdateStatus(t, "check", checkResp)
}

func TestUpdateCenterConfig_EchoOnlyResidual(t *testing.T) {
	r := setupUpdateCenterTest(t)

	resp := doPutJSON(t, r, "/api/update-center/config", map[string]any{
		"enabled":       true,
		"helperBaseUrl": "http://helper.example",
		"namespace":     "metapi",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("success=%v, want true (echo path)", body["success"])
	}
	residual, _ := body["residual"].(string)
	if residual == "" {
		t.Fatalf("expected residual field for echo-only config: %v", body)
	}
	cfg, _ := body["config"].(map[string]any)
	if cfg == nil {
		t.Fatalf("expected config echo: %v", body)
	}
	if cfg["enabled"] != true || cfg["helperBaseUrl"] != "http://helper.example" {
		t.Fatalf("config echo mismatch: %v", cfg)
	}
}
