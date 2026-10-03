package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/internal/db/dbtest"
	"github.com/kawafuchieirin/task-manager/internal/task"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func newTestHandler(t *testing.T, opts Options) http.Handler {
	t.Helper()
	return NewHandler(task.NewService(dbtest.New(t)), discardLogger, opts)
}

// do はリクエストを送り、レスポンスを返す。body が空でなければ JSON として送る。
func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("レスポンスの JSON デコード: %v", err)
	}
	return v
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, want, rec.Body.String())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) errorDetail {
	t.Helper()
	assertStatus(t, rec, wantStatus)
	body := decode[errorBody](t, rec)
	if body.Error.Code != wantCode {
		t.Errorf("error.code = %q, want %q", body.Error.Code, wantCode)
	}
	if body.Error.Message == "" {
		t.Error("error.message が空")
	}
	return body.Error
}

func createTask(t *testing.T, h http.Handler, body string) taskResponse {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/tasks", body)
	assertStatus(t, rec, http.StatusCreated)
	return decode[taskResponse](t, rec)
}

func TestCreateTask(t *testing.T) {
	h := newTestHandler(t, Options{})
	rec := do(t, h, http.MethodPost, "/api/v1/tasks", `{"title":"Go を学ぶ","estimated_min":30}`)

	assertStatus(t, rec, http.StatusCreated)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	got := decode[taskResponse](t, rec)
	if loc := rec.Header().Get("Location"); loc != "/api/v1/tasks/1" {
		t.Errorf("Location = %q", loc)
	}
	if got.ID != 1 || got.Title != "Go を学ぶ" || got.Status != task.StatusTodo ||
		got.EstimatedMin == nil || *got.EstimatedMin != 30 || got.CompletedAt != nil {
		t.Errorf("作成結果が不正: %+v", got)
	}
}

func TestCreateTask_JSONShape(t *testing.T) {
	h := newTestHandler(t, Options{})
	rec := do(t, h, http.MethodPost, "/api/v1/tasks", `{"title":"t"}`)
	assertStatus(t, rec, http.StatusCreated)

	// 他のアプリが依存する公開フォーマットなので、キー名と null の出し方を固定する。
	raw := decode[map[string]any](t, rec)
	for _, key := range []string{"id", "title", "description", "status", "estimated_min", "completed_at", "created_at", "updated_at"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("キー %q がない: %v", key, raw)
		}
	}
	if raw["estimated_min"] != nil || raw["completed_at"] != nil {
		t.Errorf("未設定の値は null で返すはず: %v", raw)
	}
	if s, _ := raw["created_at"].(string); !strings.HasSuffix(s, "Z") {
		t.Errorf("日時は UTC の RFC3339 で返すはず: %v", raw["created_at"])
	}
}

func TestCreateTask_RequestErrors(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{"Content-Type なし", "", `{"title":"t"}`, http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
		{"Content-Type が form", "application/x-www-form-urlencoded", `title=t`, http.StatusUnsupportedMediaType, codeUnsupportedMediaType},
		{"charset 付きは許可", "application/json; charset=utf-8", `{"title":"t"}`, http.StatusCreated, ""},
		{"空ボディ", "application/json", ``, http.StatusBadRequest, codeInvalidJSON},
		{"構文エラー", "application/json", `{"title":`, http.StatusBadRequest, codeInvalidJSON},
		{"未知のフィールド", "application/json", `{"title":"t","titel":"x"}`, http.StatusBadRequest, codeInvalidJSON},
		{"型違い", "application/json", `{"title":123}`, http.StatusBadRequest, codeInvalidJSON},
		{"JSON が2つ", "application/json", `{"title":"a"}{"title":"b"}`, http.StatusBadRequest, codeInvalidJSON},
		{"ボディが大きすぎる", "application/json", `{"title":"` + strings.Repeat("a", maxBodyBytes) + `"}`,
			http.StatusRequestEntityTooLarge, codePayloadTooLarge},
		{"検証エラー", "application/json", `{"title":""}`, http.StatusUnprocessableEntity, codeValidationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t, Options{})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if tt.wantCode == "" {
				assertStatus(t, rec, tt.wantStatus)
				return
			}
			assertErrorCode(t, rec, tt.wantStatus, tt.wantCode)
		})
	}
}

func TestCreateTask_ValidationDetails(t *testing.T) {
	h := newTestHandler(t, Options{})
	rec := do(t, h, http.MethodPost, "/api/v1/tasks", `{"title":"","estimated_min":-1}`)
	detail := assertErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidationFailed)

	fields := map[string]bool{}
	for _, d := range detail.Details {
		fields[d.Field] = true
	}
	if !fields["title"] || !fields["estimated_min"] {
		t.Errorf("details に title と estimated_min が含まれるはず: %+v", detail.Details)
	}
}

func TestGetTask(t *testing.T) {
	h := newTestHandler(t, Options{})
	created := createTask(t, h, `{"title":"t"}`)

	rec := do(t, h, http.MethodGet, "/api/v1/tasks/1", "")
	assertStatus(t, rec, http.StatusOK)
	if got := decode[taskResponse](t, rec); got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}

	for _, path := range []string{"/api/v1/tasks/999", "/api/v1/tasks/abc", "/api/v1/tasks/0", "/api/v1/tasks/-1"} {
		assertErrorCode(t, do(t, h, http.MethodGet, path, ""), http.StatusNotFound, codeNotFound)
	}
}

func TestUpdateTask(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"t","estimated_min":30}`)

	rec := do(t, h, http.MethodPatch, "/api/v1/tasks/1", `{"status":"done"}`)
	assertStatus(t, rec, http.StatusOK)
	done := decode[taskResponse](t, rec)
	if done.Status != task.StatusDone || done.CompletedAt == nil {
		t.Errorf("完了にしたら completed_at が入るはず: %+v", done)
	}
	if done.EstimatedMin == nil || *done.EstimatedMin != 30 {
		t.Errorf("指定していない項目は変わらないはず: %v", done.EstimatedMin)
	}

	rec = do(t, h, http.MethodPatch, "/api/v1/tasks/1", `{"status":"doing","estimated_min":null}`)
	assertStatus(t, rec, http.StatusOK)
	reopened := decode[taskResponse](t, rec)
	if reopened.CompletedAt != nil || reopened.EstimatedMin != nil {
		t.Errorf("完了取り消しと null でのクリア: %+v", reopened)
	}
}

func TestUpdateTask_Errors(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"t"}`)

	assertErrorCode(t, do(t, h, http.MethodPatch, "/api/v1/tasks/1", `{"status":"archived"}`),
		http.StatusUnprocessableEntity, codeValidationFailed)
	assertErrorCode(t, do(t, h, http.MethodPatch, "/api/v1/tasks/1", `{"unknown":1}`),
		http.StatusBadRequest, codeInvalidJSON)
	assertErrorCode(t, do(t, h, http.MethodPatch, "/api/v1/tasks/999", `{"title":"x"}`),
		http.StatusNotFound, codeNotFound)
}

func TestDeleteTask(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"t"}`)

	rec := do(t, h, http.MethodDelete, "/api/v1/tasks/1", "")
	assertStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Errorf("204 のボディは空のはず: %q", rec.Body.String())
	}
	assertErrorCode(t, do(t, h, http.MethodGet, "/api/v1/tasks/1", ""), http.StatusNotFound, codeNotFound)
	assertErrorCode(t, do(t, h, http.MethodDelete, "/api/v1/tasks/1", ""), http.StatusNotFound, codeNotFound)
}

func TestListTasks(t *testing.T) {
	h := newTestHandler(t, Options{})

	rec := do(t, h, http.MethodGet, "/api/v1/tasks", "")
	assertStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"tasks":[]`) {
		t.Errorf("0件のときは空配列（null ではない）を返すはず: %s", rec.Body.String())
	}

	createTask(t, h, `{"title":"a"}`)
	createTask(t, h, `{"title":"b","status":"done"}`)

	rec = do(t, h, http.MethodGet, "/api/v1/tasks?status=done", "")
	assertStatus(t, rec, http.StatusOK)
	got := decode[struct{ Tasks []taskResponse }](t, rec)
	if len(got.Tasks) != 1 || got.Tasks[0].Title != "b" {
		t.Errorf("done のみに絞り込むはず: %+v", got.Tasks)
	}

	assertErrorCode(t, do(t, h, http.MethodGet, "/api/v1/tasks?status=x", ""),
		http.StatusUnprocessableEntity, codeValidationFailed)
}

func TestSummary(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"a","status":"done"}`)
	createTask(t, h, `{"title":"b"}`)
	createTask(t, h, `{"title":"c"}`)

	rec := do(t, h, http.MethodGet, "/api/v1/stats/summary", "")
	assertStatus(t, rec, http.StatusOK)
	got := decode[summaryResponse](t, rec)
	if got != (summaryResponse{Total: 3, Done: 1, ProgressPercent: 33}) {
		t.Errorf("summary = %+v", got)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	tests := []struct {
		method, path, wantAllow string
	}{
		{http.MethodPut, "/api/v1/tasks/1", "GET, PATCH, DELETE"},
		{http.MethodDelete, "/api/v1/tasks", "GET, POST"},
		{http.MethodPost, "/api/v1/stats/summary", "GET"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			h := newTestHandler(t, Options{})
			rec := do(t, h, tt.method, tt.path, "")
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, エラーも JSON で返すはず", ct)
			}
			assertErrorCode(t, rec, http.StatusMethodNotAllowed, codeMethodNotAllowed)
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
		})
	}
}

func TestUnknownPath(t *testing.T) {
	h := newTestHandler(t, Options{})
	for _, path := range []string{"/api/v1/", "/api/v1/foo", "/api/v1/tasks/1/unknown"} {
		rec := do(t, h, http.MethodGet, path, "")
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s: Content-Type = %q", path, ct)
		}
		assertErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	}
}

func TestCreateTask_FieldValidation(t *testing.T) {
	h := newTestHandler(t, Options{})
	assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks", `{"title":"t","estimated_min":"abc"}`),
		http.StatusBadRequest, codeInvalidJSON)
	assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks", `{"title":"t","estimated_min":10081}`),
		http.StatusUnprocessableEntity, codeValidationFailed)
}

func TestUpdateTask_NullAndEmpty(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"t","description":"d"}`)

	// estimated_min 以外の null は「変更しない」として扱う（README に記載）。
	rec := do(t, h, http.MethodPatch, "/api/v1/tasks/1", `{"title":null,"status":null}`)
	assertStatus(t, rec, http.StatusOK)
	if got := decode[taskResponse](t, rec); got.Title != "t" || got.Status != task.StatusTodo {
		t.Errorf("null の項目は変わらないはず: %+v", got)
	}

	rec = do(t, h, http.MethodPatch, "/api/v1/tasks/1", `{}`)
	assertStatus(t, rec, http.StatusOK)
	if got := decode[taskResponse](t, rec); got.Title != "t" || got.Description != "d" {
		t.Errorf("空の PATCH では何も変わらないはず: %+v", got)
	}
}
