package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// BackgroundTaskStatus mirrors the TS background task lifecycle.
type BackgroundTaskStatus string

const (
	BackgroundTaskPending   BackgroundTaskStatus = "pending"
	BackgroundTaskRunning   BackgroundTaskStatus = "running"
	BackgroundTaskSucceeded BackgroundTaskStatus = "succeeded"
	BackgroundTaskFailed    BackgroundTaskStatus = "failed"
)

// BackgroundTaskLogEntry is a single task log line.
type BackgroundTaskLogEntry struct {
	Seq       int    `json:"seq"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

// BackgroundTask is the in-memory admin task registry entry (camelCase JSON).
type BackgroundTask struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Title      string                   `json:"title"`
	Status     BackgroundTaskStatus     `json:"status"`
	Message    string                   `json:"message"`
	Error      *string                  `json:"error"`
	Result     any                      `json:"result"`
	DedupeKey  *string                  `json:"dedupeKey"`
	CreatedAt  string                   `json:"createdAt"`
	UpdatedAt  string                   `json:"updatedAt"`
	StartedAt  *string                  `json:"startedAt"`
	FinishedAt *string                  `json:"finishedAt"`
	Logs       []BackgroundTaskLogEntry `json:"logs"`
	expiresAt  time.Time
}

// BackgroundTaskStartOptions configures StartBackgroundTask.
type BackgroundTaskStartOptions struct {
	Type      string
	Title     string
	DedupeKey string
	KeepMs    int64
}

const (
	backgroundTaskTTLMs           = 6 * 60 * 60 * 1000
	backgroundTaskCleanupInterval = 60 * time.Second
	backgroundTaskLogLimit        = 200
)

var (
	backgroundTasksMu     sync.Mutex
	backgroundTasks       = map[string]*BackgroundTask{}
	backgroundDedupeIDs   = map[string]string{}
	backgroundCleanupOnce sync.Once
)

// RegisterTasksRoutes registers all /api/tasks routes.
func RegisterTasksRoutes(r chi.Router, db *sqlx.DB) {
	handler := &tasksHandler{db: db}
	r.Get("/api/tasks", handler.listTasks)
	r.Get("/api/tasks/{id}", handler.getTask)
}

type tasksHandler struct {
	db *sqlx.DB
}

// GET /api/tasks?limit=
func (h *tasksHandler) listTasks(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(getQueryInt(r, "limit", 50), 1, 200)
	tasks := listBackgroundTasks(limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"tasks":   tasks,
	})
}

// GET /api/tasks/:id
func (h *tasksHandler) getTask(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"success": false,
			"message": "task not found",
		})
		return
	}

	task := getBackgroundTask(id)
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"success": false,
			"message": "task not found",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"task":    task,
	})
}

// StartBackgroundTask queues a background job in the in-memory registry.
// When dedupeKey matches a pending/running task, that task is reused.
func StartBackgroundTask(opts BackgroundTaskStartOptions, runner func() (any, error)) (task *BackgroundTask, reused bool) {
	ensureBackgroundTaskCleanup()
	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339Nano)

	backgroundTasksMu.Lock()
	defer backgroundTasksMu.Unlock()

	dedupeKey := opts.DedupeKey
	if dedupeKey != "" {
		if existingID, ok := backgroundDedupeIDs[dedupeKey]; ok {
			if existing := backgroundTasks[existingID]; existing != nil &&
				(existing.Status == BackgroundTaskPending || existing.Status == BackgroundTaskRunning) {
				cp := *existing
				return &cp, true
			}
			delete(backgroundDedupeIDs, dedupeKey)
		}
	}

	keepMs := opts.KeepMs
	if keepMs < 60_000 {
		keepMs = backgroundTaskTTLMs
	}

	var dedupePtr *string
	if dedupeKey != "" {
		key := dedupeKey
		dedupePtr = &key
	}

	task = &BackgroundTask{
		ID:        newBackgroundTaskID(),
		Type:      opts.Type,
		Title:     opts.Title,
		Status:    BackgroundTaskPending,
		Message:   opts.Title + " 已开始执行",
		Error:     nil,
		Result:    nil,
		DedupeKey: dedupePtr,
		CreatedAt: nowISO,
		UpdatedAt: nowISO,
		Logs:      []BackgroundTaskLogEntry{},
		expiresAt: now.Add(time.Duration(keepMs) * time.Millisecond),
	}
	backgroundTasks[task.ID] = task
	if dedupeKey != "" {
		backgroundDedupeIDs[dedupeKey] = task.ID
	}

	// Snapshot id for goroutine
	taskID := task.ID
	title := opts.Title
	go runBackgroundTask(taskID, title, dedupeKey, runner)

	cp := *task
	return &cp, false
}

func runBackgroundTask(taskID, title, dedupeKey string, runner func() (any, error)) {
	started := time.Now().UTC().Format(time.RFC3339Nano)
	backgroundTasksMu.Lock()
	if task, ok := backgroundTasks[taskID]; ok {
		task.Status = BackgroundTaskRunning
		task.StartedAt = &started
		task.Message = title + " 正在执行"
		task.UpdatedAt = started
	}
	backgroundTasksMu.Unlock()

	result, err := runner()
	finished := time.Now().UTC().Format(time.RFC3339Nano)

	backgroundTasksMu.Lock()
	defer backgroundTasksMu.Unlock()
	task, ok := backgroundTasks[taskID]
	if !ok {
		return
	}
	task.FinishedAt = &finished
	task.UpdatedAt = finished
	if err != nil {
		msg := err.Error()
		task.Status = BackgroundTaskFailed
		task.Error = &msg
		task.Message = title + " 失败：" + msg
	} else {
		task.Status = BackgroundTaskSucceeded
		task.Error = nil
		task.Result = result
		task.Message = title + " 已完成"
	}
	if dedupeKey != "" && backgroundDedupeIDs[dedupeKey] == taskID {
		delete(backgroundDedupeIDs, dedupeKey)
	}
}

func getBackgroundTask(id string) *BackgroundTask {
	backgroundTasksMu.Lock()
	defer backgroundTasksMu.Unlock()
	cleanupExpiredBackgroundTasksLocked(time.Now())
	task, ok := backgroundTasks[id]
	if !ok {
		return nil
	}
	cp := *task
	if cp.Logs == nil {
		cp.Logs = []BackgroundTaskLogEntry{}
	}
	return &cp
}

func listBackgroundTasks(limit int) []BackgroundTask {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	backgroundTasksMu.Lock()
	defer backgroundTasksMu.Unlock()
	cleanupExpiredBackgroundTasksLocked(time.Now())

	all := make([]BackgroundTask, 0, len(backgroundTasks))
	for _, task := range backgroundTasks {
		cp := *task
		if cp.Logs == nil {
			cp.Logs = []BackgroundTaskLogEntry{}
		}
		all = append(all, cp)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt > all[j].CreatedAt
	})
	if len(all) > limit {
		all = all[:limit]
	}
	if all == nil {
		all = []BackgroundTask{}
	}
	return all
}

func ensureBackgroundTaskCleanup() {
	backgroundCleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(backgroundTaskCleanupInterval)
			defer ticker.Stop()
			for range ticker.C {
				backgroundTasksMu.Lock()
				cleanupExpiredBackgroundTasksLocked(time.Now())
				backgroundTasksMu.Unlock()
			}
		}()
	})
}

func cleanupExpiredBackgroundTasksLocked(now time.Time) {
	for id, task := range backgroundTasks {
		if !task.expiresAt.IsZero() && !task.expiresAt.After(now) {
			if task.DedupeKey != nil && backgroundDedupeIDs[*task.DedupeKey] == id {
				delete(backgroundDedupeIDs, *task.DedupeKey)
			}
			delete(backgroundTasks, id)
		}
	}
}

func newBackgroundTaskID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fall back to timestamp-based id.
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	// UUID-ish hex (no dashes) is fine for admin task ids.
	return hex.EncodeToString(b[:])
}

// resetBackgroundTasksForTests clears the in-memory registry (tests only).
func resetBackgroundTasksForTests() {
	backgroundTasksMu.Lock()
	defer backgroundTasksMu.Unlock()
	backgroundTasks = map[string]*BackgroundTask{}
	backgroundDedupeIDs = map[string]string{}
}
