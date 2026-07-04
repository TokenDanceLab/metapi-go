package admin

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
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
	limit := clampInt(getQueryInt(r, "limit", 50), 0, 500)

	// Background tasks are not stored in DB in the TS implementation;
	// they live in an in-memory registry. Return empty stub for now.
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks": []any{},
	})
	_ = limit
}

// GET /api/tasks/:id
func (h *tasksHandler) getTask(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"success": false,
			"message": "task not found",
		})
		return
	}

	// Stub: task registry not yet implemented
	writeJSON(w, http.StatusNotFound, map[string]any{
		"success": false,
		"message": "task not found",
	})
}
