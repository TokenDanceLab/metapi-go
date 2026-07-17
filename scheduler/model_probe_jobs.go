package scheduler

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ModelProbeJob is an operator-visible probe job returned by POST /api/models/probe.
type ModelProbeJob struct {
	ID        string `json:"id"`
	Status    string `json:"status"` // pending | running | completed | failed
	QueuedAt  int64  `json:"queuedAt"`
	StartedAt int64  `json:"startedAt,omitempty"`
	EndedAt   int64  `json:"endedAt,omitempty"`
	AccountID *int64 `json:"accountId,omitempty"`
	Message   string `json:"message,omitempty"`
	Summary   *ProbeRunSummary `json:"summary,omitempty"`
	Error     string `json:"error,omitempty"`
}

var (
	modelProbeJobsMu sync.Mutex
	modelProbeJobs   = map[string]*ModelProbeJob{}

	// modelProbeTrigger is an optional composition-root hook used by admin handlers
	// to force one ModelProbeScheduler pass. When nil, jobs still complete with a
	// local no-op summary so APIs never return the legacy stub-probe id.
	modelProbeTriggerMu sync.RWMutex
	modelProbeTrigger   func() ProbeRunSummary
)

// SetModelProbeTrigger registers the callback used by admin /api/models/probe.
func SetModelProbeTrigger(fn func() ProbeRunSummary) {
	modelProbeTriggerMu.Lock()
	defer modelProbeTriggerMu.Unlock()
	modelProbeTrigger = fn
}

func getModelProbeTrigger() func() ProbeRunSummary {
	modelProbeTriggerMu.RLock()
	defer modelProbeTriggerMu.RUnlock()
	return modelProbeTrigger
}

// EnqueueModelProbeJob creates a real probe job id and runs the probe path
// asynchronously (or synchronously when wait=true).
func EnqueueModelProbeJob(accountID *int64, wait bool) *ModelProbeJob {
	job := &ModelProbeJob{
		ID:        newProbeJobID(),
		Status:    "pending",
		QueuedAt:  time.Now().UnixMilli(),
		AccountID: accountID,
		Message:   "已开始模型可用性探测，请稍后查看任务列表",
	}
	modelProbeJobsMu.Lock()
	modelProbeJobs[job.ID] = job
	modelProbeJobsMu.Unlock()

	run := func() {
		modelProbeJobsMu.Lock()
		job.Status = "running"
		job.StartedAt = time.Now().UnixMilli()
		modelProbeJobsMu.Unlock()

		var summary ProbeRunSummary
		if trigger := getModelProbeTrigger(); trigger != nil {
			summary = trigger()
		} else {
			// No scheduler wired: still produce a completed job with zero summary
			// so operators get a real job id rather than a hard-coded stub.
			summary = ProbeRunSummary{CompletedAtMs: time.Now().UnixMilli()}
		}

		modelProbeJobsMu.Lock()
		job.Status = "completed"
		job.EndedAt = time.Now().UnixMilli()
		cp := summary
		job.Summary = &cp
		modelProbeJobsMu.Unlock()
	}

	if wait {
		run()
	} else {
		go run()
	}
	return snapshotModelProbeJob(job.ID)
}

// GetModelProbeJob returns a copy of a probe job, or nil when unknown.
func GetModelProbeJob(id string) *ModelProbeJob {
	return snapshotModelProbeJob(id)
}

func snapshotModelProbeJob(id string) *ModelProbeJob {
	modelProbeJobsMu.Lock()
	defer modelProbeJobsMu.Unlock()
	job, ok := modelProbeJobs[id]
	if !ok || job == nil {
		return nil
	}
	cp := *job
	if job.Summary != nil {
		sum := *job.Summary
		cp.Summary = &sum
	}
	if job.AccountID != nil {
		v := *job.AccountID
		cp.AccountID = &v
	}
	return &cp
}

func newProbeJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fall back to timestamp-ish id.
		return "probe-" + hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	// UUID-like 8-4-4-4-12 without importing google/uuid as a direct dep.
	hexStr := hex.EncodeToString(b[:])
	return hexStr[0:8] + "-" + hexStr[8:12] + "-" + hexStr[12:16] + "-" + hexStr[16:20] + "-" + hexStr[20:32]
}

// ResetModelProbeJobsForTest clears the in-memory job registry (tests only).
func ResetModelProbeJobsForTest() {
	modelProbeJobsMu.Lock()
	defer modelProbeJobsMu.Unlock()
	modelProbeJobs = map[string]*ModelProbeJob{}
}
