package httpapi

import (
	"net/http"
	"time"

	"github.com/kawafuchieirin/task-manager/api/internal/task"
)

type reflectionResponse struct {
	TaskID        int64              `json:"task_id"`
	Body          string             `json:"body"`
	Learned       []string           `json:"learned"`
	NotLearned    []string           `json:"not_learned"`
	ExtractStatus task.ExtractStatus `json:"extract_status"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

func toReflectionResponse(r task.Reflection) reflectionResponse {
	return reflectionResponse{
		TaskID:        r.TaskID,
		Body:          r.Body,
		Learned:       nonNil(r.Learned),
		NotLearned:    nonNil(r.NotLearned),
		ExtractStatus: r.Status,
		UpdatedAt:     r.UpdatedAt,
	}
}

type reflectionEntryResponse struct {
	reflectionResponse
	TaskTitle  string      `json:"task_title"`
	TaskStatus task.Status `json:"task_status"`
}

type saveReflectionRequest struct {
	Body string `json:"body"`
}

func (h *taskHandler) getReflection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ref, err := h.svc.GetReflection(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toReflectionResponse(ref))
}

// saveReflection は振り返りを保存して抽出する。抽出に失敗しても 200 を返し、extract_status で知らせる。
func (h *taskHandler) saveReflection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req saveReflectionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ref, err := h.svc.SaveReflection(r.Context(), id, req.Body)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	h.logExtractFailure(r, ref)
	writeJSON(w, http.StatusOK, toReflectionResponse(ref))
}

func (h *taskHandler) extractReflection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ref, err := h.svc.ExtractReflection(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	h.logExtractFailure(r, ref)
	writeJSON(w, http.StatusOK, toReflectionResponse(ref))
}

func (h *taskHandler) deleteReflection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteReflection(r.Context(), id); err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listReflections は期間内の振り返りを返す。from / to は RFC3339（from 以上 to 未満。省略可）。
func (h *taskHandler) listReflections(w http.ResponseWriter, r *http.Request) {
	from, okFrom := parseTimeParam(r, "from")
	to, okTo := parseTimeParam(r, "to")
	var details []task.FieldError
	if !okFrom {
		details = append(details, task.FieldError{Field: "from", Code: task.CodeInvalid, Message: "from は RFC3339 形式（例: 2026-10-01T00:00:00+09:00）で指定してください"})
	}
	if !okTo {
		details = append(details, task.FieldError{Field: "to", Code: task.CodeInvalid, Message: "to は RFC3339 形式（例: 2026-10-08T00:00:00+09:00）で指定してください"})
	}
	if len(details) > 0 {
		writeError(w, http.StatusUnprocessableEntity, codeValidationFailed, "入力値が不正です", details)
		return
	}

	entries, err := h.svc.ListReflections(r.Context(), from, to)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	resp := make([]reflectionEntryResponse, len(entries))
	for i, e := range entries {
		resp[i] = reflectionEntryResponse{
			reflectionResponse: toReflectionResponse(e.Reflection),
			TaskTitle:          e.TaskTitle,
			TaskStatus:         e.TaskStatus,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reflections": resp})
}

// logExtractFailure は抽出に失敗したことをログに残す（応答は 200 のまま、extract_status で知らせる）。
func (h *taskHandler) logExtractFailure(r *http.Request, ref task.Reflection) {
	if ref.Status == task.ExtractFailed {
		h.logger.WarnContext(r.Context(), "振り返りの抽出に失敗（insight の状態を確認）", "task_id", ref.TaskID)
	}
}

// parseTimeParam はクエリの日時（RFC3339）を読み取る。未指定なら nil, true。形式が不正なら nil, false。
func parseTimeParam(r *http.Request, key string) (*time.Time, bool) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, false
	}
	return &t, true
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
