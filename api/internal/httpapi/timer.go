package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/kawafuchieirin/task-manager/api/internal/task"
)

type entryResponse struct {
	ID          int64      `json:"id"`
	TaskID      int64      `json:"task_id"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"` // null は計測中
	DurationSec int64      `json:"duration_sec"`
}

func toEntryResponse(e task.TimeEntry, now time.Time) entryResponse {
	return entryResponse{
		ID:          e.ID,
		TaskID:      e.TaskID,
		StartedAt:   e.StartedAt,
		EndedAt:     e.EndedAt,
		DurationSec: int64(e.Duration(now) / time.Second),
	}
}

type addEntryRequest struct {
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

type updateEntryRequest struct {
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
}

func entryLocation(id int64) string {
	return "/api/v1/time-entries/" + strconv.FormatInt(id, 10)
}

func (h *taskHandler) startTimer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	entry, err := h.svc.StartTimer(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	w.Header().Set("Location", entryLocation(entry.ID))
	writeJSON(w, http.StatusCreated, toEntryResponse(entry, time.Now()))
}

func (h *taskHandler) stopTimer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	entry, err := h.svc.StopTimer(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toEntryResponse(entry, time.Now()))
}

func (h *taskHandler) listEntries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	entries, err := h.svc.ListTimeEntries(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	now := time.Now()
	resp := make([]entryResponse, len(entries))
	for i, e := range entries {
		resp[i] = toEntryResponse(e, now)
	}
	writeJSON(w, http.StatusOK, map[string]any{"time_entries": resp})
}

func (h *taskHandler) addEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req addEntryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	entry, err := h.svc.AddTimeEntry(r.Context(), id, task.EntryInput{StartedAt: req.StartedAt, EndedAt: req.EndedAt})
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	w.Header().Set("Location", entryLocation(entry.ID))
	writeJSON(w, http.StatusCreated, toEntryResponse(entry, time.Now()))
}

func (h *taskHandler) getEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathEntryID(w, r)
	if !ok {
		return
	}
	entry, err := h.svc.GetTimeEntry(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toEntryResponse(entry, time.Now()))
}

func (h *taskHandler) updateEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathEntryID(w, r)
	if !ok {
		return
	}
	var req updateEntryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	entry, err := h.svc.UpdateTimeEntry(r.Context(), id, task.EntryPatch{StartedAt: req.StartedAt, EndedAt: req.EndedAt})
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toEntryResponse(entry, time.Now()))
}

func (h *taskHandler) deleteEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathEntryID(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteTimeEntry(r.Context(), id); err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathEntryID はパスの {id} を時間記録の ID として読み取る。数値でなければ 404。
func pathEntryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, task.ErrEntryNotFound.Error(), nil)
		return 0, false
	}
	return id, true
}
