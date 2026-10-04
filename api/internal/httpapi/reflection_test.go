package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/api/internal/db/dbtest"
	"github.com/kawafuchieirin/task-manager/api/internal/task"
)

// stubExtractor は決まった結果（または失敗）を返す Extractor。
type stubExtractor struct {
	learned, notLearned []string
	err                 error
}

func (s *stubExtractor) Extract(context.Context, string) ([]string, []string, error) {
	return s.learned, s.notLearned, s.err
}

func newReflectionHandler(t *testing.T, ex task.Extractor) http.Handler {
	t.Helper()
	return NewHandler(task.NewService(dbtest.New(t), task.WithExtractor(ex)), discardLogger, Options{})
}

func TestReflection_SaveGetDelete(t *testing.T) {
	h := newReflectionHandler(t, &stubExtractor{learned: []string{"goroutine"}, notLearned: []string{}})
	createTask(t, h, `{"title":"t","status":"done"}`)

	assertErrorCode(t, do(t, h, http.MethodGet, "/api/v1/tasks/1/reflection", ""), http.StatusNotFound, codeNotFound)

	rec := do(t, h, http.MethodPut, "/api/v1/tasks/1/reflection", `{"body":"goroutine を理解した。"}`)
	assertStatus(t, rec, http.StatusOK)
	got := decode[map[string]any](t, rec)
	if got["extract_status"] != "ok" || got["body"] != "goroutine を理解した。" {
		t.Errorf("保存結果: %v", got)
	}
	if l, _ := got["learned"].([]any); len(l) != 1 || l[0] != "goroutine" {
		t.Errorf("learned: %v", got["learned"])
	}
	if nl, ok := got["not_learned"].([]any); !ok || len(nl) != 0 {
		t.Errorf("0件は空配列で返すはず: %v", got["not_learned"])
	}

	// タスクにも振り返りが含まれる。
	task := decode[map[string]any](t, do(t, h, http.MethodGet, "/api/v1/tasks/1", ""))
	if ref, _ := task["reflection"].(map[string]any); ref == nil || ref["extract_status"] != "ok" {
		t.Errorf("タスクの reflection: %v", task["reflection"])
	}

	assertStatus(t, do(t, h, http.MethodGet, "/api/v1/tasks/1/reflection", ""), http.StatusOK)
	assertStatus(t, do(t, h, http.MethodDelete, "/api/v1/tasks/1/reflection", ""), http.StatusNoContent)
	assertErrorCode(t, do(t, h, http.MethodDelete, "/api/v1/tasks/1/reflection", ""), http.StatusNotFound, codeNotFound)
	assertErrorCode(t, do(t, h, http.MethodPut, "/api/v1/tasks/999/reflection", `{"body":"a"}`), http.StatusNotFound, codeNotFound)
}

func TestReflection_ExtractFailureIs200AndRetryable(t *testing.T) {
	ex := &stubExtractor{err: errors.New("insight 停止中")}
	h := newReflectionHandler(t, ex)
	createTask(t, h, `{"title":"t"}`)

	rec := do(t, h, http.MethodPut, "/api/v1/tasks/1/reflection", `{"body":"+ a"}`)
	assertStatus(t, rec, http.StatusOK)
	if got := decode[reflectionResponse](t, rec); got.ExtractStatus != task.ExtractFailed {
		t.Errorf("抽出に失敗しても 200 で failed を返すはず: %+v", got)
	}

	ex.err, ex.learned, ex.notLearned = nil, []string{"a"}, []string{}
	rec = do(t, h, http.MethodPost, "/api/v1/tasks/1/reflection/extract", "")
	assertStatus(t, rec, http.StatusOK)
	if got := decode[reflectionResponse](t, rec); got.ExtractStatus != task.ExtractOK || len(got.Learned) != 1 {
		t.Errorf("やり直し後: %+v", got)
	}
	assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks/999/reflection/extract", ""), http.StatusNotFound, codeNotFound)
}

func TestReflection_Validation(t *testing.T) {
	h := newReflectionHandler(t, &stubExtractor{})
	createTask(t, h, `{"title":"t"}`)

	detail := assertErrorCode(t, do(t, h, http.MethodPut, "/api/v1/tasks/1/reflection", `{"body":" "}`),
		http.StatusUnprocessableEntity, codeValidationFailed)
	if len(detail.Details) != 1 || detail.Details[0].Field != "body" || detail.Details[0].Code != "required" {
		t.Errorf("details: %+v", detail.Details)
	}
	assertErrorCode(t, do(t, h, http.MethodPut, "/api/v1/tasks/1/reflection", `{"body":"`+strings.Repeat("あ", task.MaxReflectionLen+1)+`"}`),
		http.StatusUnprocessableEntity, codeValidationFailed)
	assertErrorCode(t, do(t, h, http.MethodPut, "/api/v1/tasks/1/reflection", `{"text":"a"}`), http.StatusBadRequest, codeInvalidJSON)
}

func TestListReflections(t *testing.T) {
	h := newReflectionHandler(t, &stubExtractor{learned: []string{"x"}, notLearned: []string{}})
	createTask(t, h, `{"title":"スライム","status":"done"}`)
	createTask(t, h, `{"title":"ドラキー"}`)
	assertStatus(t, do(t, h, http.MethodPut, "/api/v1/tasks/1/reflection", `{"body":"a"}`), http.StatusOK)

	rec := do(t, h, http.MethodGet, "/api/v1/reflections", "")
	assertStatus(t, rec, http.StatusOK)
	got := decode[struct {
		Reflections []map[string]any `json:"reflections"`
	}](t, rec)
	if len(got.Reflections) != 1 || got.Reflections[0]["task_title"] != "スライム" || got.Reflections[0]["task_status"] != "done" {
		t.Errorf("一覧: %+v", got.Reflections)
	}

	// 未来の期間なら0件（空配列）。タイムゾーン付きの指定を受け付ける。
	q := url.Values{"from": {"2999-01-01T00:00:00+09:00"}}
	rec = do(t, h, http.MethodGet, "/api/v1/reflections?"+q.Encode(), "")
	assertStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"reflections":[]`) {
		t.Errorf("0件は空配列のはず: %s", rec.Body.String())
	}

	detail := assertErrorCode(t, do(t, h, http.MethodGet, "/api/v1/reflections?from=2026-10-01&to=x", ""),
		http.StatusUnprocessableEntity, codeValidationFailed)
	if len(detail.Details) != 2 {
		t.Errorf("from と to の両方の誤りを返すはず: %+v", detail.Details)
	}
}

func TestReflection_MethodNotAllowed(t *testing.T) {
	h := newReflectionHandler(t, &stubExtractor{})
	for _, tt := range []struct{ method, path, allow string }{
		{http.MethodPost, "/api/v1/tasks/1/reflection", "GET, PUT, DELETE"},
		{http.MethodGet, "/api/v1/tasks/1/reflection/extract", "POST"},
		{http.MethodPost, "/api/v1/reflections", "GET"},
	} {
		rec := do(t, h, tt.method, tt.path, "")
		assertErrorCode(t, rec, http.StatusMethodNotAllowed, codeMethodNotAllowed)
		if got := rec.Header().Get("Allow"); got != tt.allow {
			t.Errorf("%s %s: Allow = %q", tt.method, tt.path, got)
		}
	}
}
