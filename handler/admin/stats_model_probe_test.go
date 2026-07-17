package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/scheduler"
)

func TestStats_ModelProbe_ReturnsRealJobID(t *testing.T) {
	scheduler.ResetModelProbeJobsForTest()
	t.Cleanup(scheduler.ResetModelProbeJobsForTest)

	// Deterministic trigger for wait=true path.
	scheduler.SetModelProbeTrigger(func() scheduler.ProbeRunSummary {
		return scheduler.ProbeRunSummary{
			AccountsConsidered: 1,
			AccountsProbed:     1,
			TargetsScanned:     2,
			Success:            2,
			CompletedAtMs:      time.Now().UnixMilli(),
		}
	})
	t.Cleanup(func() { scheduler.SetModelProbeTrigger(nil) })

	_, r := setupStatsSQLiteTest(t)

	// Async path (default): 202 + real uuid-like job id, never stub-probe.
	resp := doPostJSON(t, r, "/api/models/probe", map[string]any{})
	if resp.Code != http.StatusAccepted {
		t.Fatalf("async expected 202, got %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["success"] != true {
		t.Fatalf("success=%v", out["success"])
	}
	jobID, _ := out["jobId"].(string)
	if jobID == "" || jobID == "stub-probe" {
		t.Fatalf("expected real jobId, got %q", jobID)
	}
	if !strings.Contains(jobID, "-") {
		t.Fatalf("expected uuid-like jobId, got %q", jobID)
	}
	if out["queued"] != true {
		t.Fatalf("queued=%v", out["queued"])
	}

	// Sync path: wait=true completes with summary.
	resp = doPostJSON(t, r, "/api/models/probe", map[string]any{"wait": true})
	if resp.Code != http.StatusOK {
		t.Fatalf("wait expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	out = map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal wait: %v", err)
	}
	jobID, _ = out["jobId"].(string)
	if jobID == "" || jobID == "stub-probe" {
		t.Fatalf("wait jobId=%q", jobID)
	}
	if out["status"] != "completed" {
		t.Fatalf("status=%v body=%s", out["status"], resp.Body.String())
	}
	if out["summary"] == nil {
		t.Fatalf("expected summary on wait path, body=%s", resp.Body.String())
	}
}

func TestStats_ModelProbe_InvalidAccountID(t *testing.T) {
	_, r := setupStatsSQLiteTest(t)
	resp := doPostJSON(t, r, "/api/models/probe", map[string]any{"accountId": 0})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}
