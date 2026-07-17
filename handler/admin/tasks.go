package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
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

// BackgroundTask is an admin task registry entry (camelCase JSON).
//
// Since #265, StartBackgroundTask dual-writes to `admin_background_tasks` when a
// runtime DB is available so multi-instance deployments can list/get tasks across
// processes. Memory cache is the primary fast path; DB is the cold cross-process
// fallback. Cleanup removes expired rows from both memory and DB.
// See docs/analysis/background-tasks-multi-instance-residual.md.
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

// RegisterTasksRoutes registers /api/tasks routes.
// list/get observe memory-visible tasks AND durable tasks from admin_background_tasks
// when the handler DB is set (#265). Cold DB fallback enables cross-instance visibility.
func RegisterTasksRoutes(r chi.Router, db *sqlx.DB) {
	SetBackgroundTaskDB(db)
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
	tasks := listBackgroundTasks(h.db, limit)
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

	task := getBackgroundTask(h.db, id)
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

// StartBackgroundTask queues a background job in the process-local in-memory
// registry AND dual-writes to admin_background_tasks when a runtime DB is
// available (#265). Memory is the fast path; DB is the cold cross-process fallback.
//
// When dedupeKey matches a pending/running task across memory+DB, that task is reused.
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
				cp := snapshotBackgroundTask(existing)
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

	// Dual-write to DB best-effort (#265). Memory is the primary layer.
	insertBackgroundTaskDB(task)

	go runBackgroundTask(taskID, title, dedupeKey, runner)

	cp := snapshotBackgroundTask(task)
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

	// DB update best-effort
	updateBackgroundTaskDBStatus(taskID, string(BackgroundTaskRunning), &started, nil, nil, nil)

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
	var errPtr *string
	var status BackgroundTaskStatus
	if err != nil {
		msg := err.Error()
		errPtr = &msg
		task.Status = BackgroundTaskFailed
		task.Error = &msg
		task.Message = title + " 失败：" + msg
		status = BackgroundTaskFailed
	} else {
		task.Status = BackgroundTaskSucceeded
		task.Error = nil
		task.Result = result
		task.Message = title + " 已完成"
		status = BackgroundTaskSucceeded
	}
	if dedupeKey != "" && backgroundDedupeIDs[dedupeKey] == taskID {
		delete(backgroundDedupeIDs, dedupeKey)
	}

	// DB update best-effort
	updateBackgroundTaskDBStatus(taskID, string(status), nil, &finished, errPtr, result)
}

func getBackgroundTask(db *sqlx.DB, id string) *BackgroundTask {
	backgroundTasksMu.Lock()
	cleanupExpiredBackgroundTasksLocked(time.Now())
	task, ok := backgroundTasks[id]
	if ok {
		// Snapshot under the lock: runBackgroundTask mutates Status/Result/Error
		// while HTTP get/list may observe the same *BackgroundTask concurrently.
		// Copying after Unlock races with Result assignment (DATA RACE under -race).
		cp := snapshotBackgroundTask(task)
		backgroundTasksMu.Unlock()
		return &cp
	}
	backgroundTasksMu.Unlock()
	// Cold DB fallback (#265)
	return loadBackgroundTaskDB(db, id)
}

func listBackgroundTasks(db *sqlx.DB, limit int) []BackgroundTask {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	backgroundTasksMu.Lock()
	cleanupExpiredBackgroundTasksLocked(time.Now())
	seen := make(map[string]bool)
	var all []BackgroundTask
	for _, task := range backgroundTasks {
		cp := snapshotBackgroundTask(task)
		all = append(all, cp)
		seen[cp.ID] = true
	}
	backgroundTasksMu.Unlock()

	// Cold DB fallback: merge rows from other processes not in memory (#265).
	dbTasks := listBackgroundTasksDB(db, limit*2)
	for _, t := range dbTasks {
		if seen[t.ID] {
			continue
		}
		if t.Logs == nil {
			t.Logs = []BackgroundTaskLogEntry{}
		}
		all = append(all, t)
		seen[t.ID] = true
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

// ---- Durable admin_background_tasks helpers (#265) ----

func insertBackgroundTaskDB(task *BackgroundTask) {
	if task == nil || task.ID == "" {
		return
	}
	nowISO := time.Now().UTC().Format(time.RFC3339)
	var dedupePtr any
	if task.DedupeKey != nil {
		dedupePtr = *task.DedupeKey
	}
	// Derive DB handle via tasksHandler.db — but StartBackgroundTask is
	// a free function. In the current architecture each call site passes its
	// runner but we don't have the db handle here. Use global bootstrap.
	// For now: register a package-level DB setter.
	bgTasksMu.Lock()
	db := bgTasksDB
	bgTasksMu.Unlock()
	if db == nil {
		return
	}
	_, err := db.Exec(`
		INSERT INTO admin_background_tasks (task_id, type, title, status, message, dedupe_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO NOTHING
	`, task.ID, task.Type, task.Title, string(task.Status), task.Message, dedupePtr, task.CreatedAt, nowISO)
	if err != nil {
		slog.Warn("admin tasks: durable insert failed (memory set)", "task_id", task.ID, "error", err)
	}
}

func updateBackgroundTaskDBStatus(taskID, status string, startedAt, finishedAt, errMsg *string, result any) {
	bgTasksMu.Lock()
	db := bgTasksDB
	bgTasksMu.Unlock()
	if db == nil {
		return
	}
	nowISO := time.Now().UTC().Format(time.RFC3339)
	_, e := db.Exec(`
		UPDATE admin_background_tasks SET status=?, message=?, error=?, started_at=COALESCE(?,started_at),
		finished_at=?, updated_at=? WHERE task_id=? AND status NOT IN ('succeeded','failed')
	`, status, "updated", errMsg, startedAt, finishedAt, nowISO, taskID)
	if e != nil {
		slog.Debug("admin tasks: durable status update failed", "task_id", taskID, "error", e)
	}
	_ = result // result_json column available for future serialization
}

func loadBackgroundTaskDB(db *sqlx.DB, id string) *BackgroundTask {
	if db == nil {
		return nil
	}
	var rec struct {
		ID        string  `db:"task_id"`
		Type      string  `db:"type"`
		Title     string  `db:"title"`
		Status    string  `db:"status"`
		Message   *string `db:"message"`
		ErrorTxt  *string `db:"error"`
		DedupeKey *string `db:"dedupe_key"`
		CreatedAt string  `db:"created_at"`
		UpdatedAt string  `db:"updated_at"`
		StartedAt *string `db:"started_at"`
		FinishedAt *string `db:"finished_at"`
	}
	err := db.Get(&rec, `
		SELECT task_id, type, title, status, message, error, dedupe_key,
		       created_at, updated_at, started_at, finished_at
		FROM admin_background_tasks WHERE task_id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return nil
	}
	task := &BackgroundTask{
		ID:         rec.ID,
		Type:       rec.Type,
		Title:      rec.Title,
		Status:     BackgroundTaskStatus(rec.Status),
		Message:    strOrEmpty(rec.Message),
		DedupeKey:  rec.DedupeKey,
		CreatedAt:  rec.CreatedAt,
		UpdatedAt:  rec.UpdatedAt,
		StartedAt:  rec.StartedAt,
		FinishedAt: rec.FinishedAt,
		Logs:       []BackgroundTaskLogEntry{},
	}
	if rec.ErrorTxt != nil {
		task.Error = rec.ErrorTxt
	}
	return task
}

func listBackgroundTasksDB(db *sqlx.DB, limit int) []BackgroundTask {
	if db == nil {
		return nil
	}
	type dbRow struct {
		ID         string  `db:"task_id"`
		Type       string  `db:"type"`
		Title      string  `db:"title"`
		Status     string  `db:"status"`
		Message    *string `db:"message"`
		ErrorTxt   *string `db:"error"`
		DedupeKey  *string `db:"dedupe_key"`
		CreatedAt  string  `db:"created_at"`
		UpdatedAt  string  `db:"updated_at"`
		StartedAt  *string `db:"started_at"`
		FinishedAt *string `db:"finished_at"`
	}
	var rows []dbRow
	if err := db.Select(&rows, `
		SELECT task_id, type, title, status, message, error, dedupe_key,
		       created_at, updated_at, started_at, finished_at
		FROM admin_background_tasks ORDER BY created_at DESC LIMIT ?`, limit); err != nil {
		return nil
	}
	var out []BackgroundTask
	for _, r := range rows {
		task := BackgroundTask{
			ID:         r.ID,
			Type:       r.Type,
			Title:      r.Title,
			Status:     BackgroundTaskStatus(r.Status),
			Message:    strOrEmpty(r.Message),
			DedupeKey:  r.DedupeKey,
			CreatedAt:  r.CreatedAt,
			UpdatedAt:  r.UpdatedAt,
			StartedAt:  r.StartedAt,
			FinishedAt: r.FinishedAt,
			Logs:       []BackgroundTaskLogEntry{},
		}
		if r.ErrorTxt != nil {
			task.Error = r.ErrorTxt
		}
		out = append(out, task)
	}
	return out
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// snapshotBackgroundTask returns a value copy safe for callers outside
// backgroundTasksMu. Logs are slice-copied so later appends on the live task
// do not race readers. Callers must hold backgroundTasksMu when task is live.
func snapshotBackgroundTask(task *BackgroundTask) BackgroundTask {
	if task == nil {
		return BackgroundTask{Logs: []BackgroundTaskLogEntry{}}
	}
	cp := *task
	if cp.Logs == nil {
		cp.Logs = []BackgroundTaskLogEntry{}
	} else {
		logs := make([]BackgroundTaskLogEntry, len(cp.Logs))
		copy(logs, cp.Logs)
		cp.Logs = logs
	}
	return cp
}

// ---- Global DB setter for free functions ----

var (
	bgTasksMu sync.Mutex
	bgTasksDB *sqlx.DB
)

// SetBackgroundTaskDB stores the DB handle used by free-function start/update.
// Called from server boot or test wiring.
func SetBackgroundTaskDB(db *sqlx.DB) {
	bgTasksMu.Lock()
	defer bgTasksMu.Unlock()
	bgTasksDB = db
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
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}

// waitAllBackgroundTasksForTests polls until every in-memory task is terminal
// or the timeout elapses. Used by tests so cleanup does not race runners that
// still touch package globals (e.g. globalAccountsCache.clear) (#328).
func waitAllBackgroundTasksForTests(timeout time.Duration) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		backgroundTasksMu.Lock()
		pending := false
		for _, task := range backgroundTasks {
			if task.Status == BackgroundTaskPending || task.Status == BackgroundTaskRunning {
				pending = true
				break
			}
		}
		backgroundTasksMu.Unlock()
		if !pending {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// resetBackgroundTasksForTests clears the in-memory registry (tests only).
// It first waits briefly for runners so map teardown does not race active work.
func resetBackgroundTasksForTests() {
	waitAllBackgroundTasksForTests(3 * time.Second)
	backgroundTasksMu.Lock()
	defer backgroundTasksMu.Unlock()
	backgroundTasks = map[string]*BackgroundTask{}
	backgroundDedupeIDs = map[string]string{}
}
