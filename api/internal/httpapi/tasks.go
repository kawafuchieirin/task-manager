package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kawafuchieirin/task-manager/api/internal/task"
)

type taskResponse struct {
	ID           int64       `json:"id"`
	Title        string      `json:"title"`
	Description  string      `json:"description"`
	Status       task.Status `json:"status"`
	EstimatedMin *int        `json:"estimated_min"`
	CompletedAt  *time.Time  `json:"completed_at"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	ActualSec    int64       `json:"actual_sec"`
	RunningSince *time.Time  `json:"running_since"`
}

func toTaskResponse(t task.Task) taskResponse {
	return taskResponse{
		ID:           t.ID,
		Title:        t.Title,
		Description:  t.Description,
		Status:       t.Status,
		EstimatedMin: t.EstimatedMin,
		CompletedAt:  t.CompletedAt,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
		ActualSec:    t.ActualSec,
		RunningSince: t.RunningSince,
	}
}

type createTaskRequest struct {
	Title        string      `json:"title"`
	Description  string      `json:"description"`
	Status       task.Status `json:"status"`
	EstimatedMin *int        `json:"estimated_min"`
}

type updateTaskRequest struct {
	Title        *string            `json:"title"`
	Description  *string            `json:"description"`
	Status       *task.Status       `json:"status"`
	EstimatedMin task.Nullable[int] `json:"estimated_min"`
}

type summaryResponse struct {
	Total           int   `json:"total"`
	Done            int   `json:"done"`
	ProgressPercent int   `json:"progress_percent"`
	EstimatedMin    int   `json:"estimated_min"`
	ActualSec       int64 `json:"actual_sec"`
}

type taskHandler struct {
	svc    *task.Service
	logger *slog.Logger
}

func (h *taskHandler) list(w http.ResponseWriter, r *http.Request) {
	var filter *task.Status
	if s := r.URL.Query().Get("status"); s != "" {
		st := task.Status(s)
		filter = &st
	}
	tasks, err := h.svc.List(r.Context(), filter)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	resp := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = toTaskResponse(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": resp})
}

func (h *taskHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	created, err := h.svc.Create(r.Context(), task.CreateInput{
		Title:        req.Title,
		Description:  req.Description,
		Status:       req.Status,
		EstimatedMin: req.EstimatedMin,
	})
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	w.Header().Set("Location", "/api/v1/tasks/"+strconv.FormatInt(created.ID, 10))
	writeJSON(w, http.StatusCreated, toTaskResponse(created))
}

func (h *taskHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskResponse(t))
}

func (h *taskHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req updateTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := h.svc.Update(r.Context(), id, task.UpdateInput{
		Title:        req.Title,
		Description:  req.Description,
		Status:       req.Status,
		EstimatedMin: req.EstimatedMin,
	})
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskResponse(updated))
}

func (h *taskHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *taskHandler) summary(w http.ResponseWriter, r *http.Request) {
	sum, err := h.svc.Summary(r.Context())
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, summaryResponse{
		Total:           sum.Total,
		Done:            sum.Done,
		ProgressPercent: sum.ProgressPercent(),
		EstimatedMin:    sum.EstimatedMin,
		ActualSec:       sum.ActualSec,
	})
}

// pathID はパスの {id} を正の整数として読み取る。数値でなければ該当タスクなしとして 404 を返す。
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, task.ErrNotFound.Error(), nil)
		return 0, false
	}
	return id, true
}
