package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/insight/internal/extract"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/extract", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestExtract(t *testing.T) {
	h := New(extract.RuleBased{}, discard)
	rec := post(h, `{"text":"goroutine の使い方を理解した。テストの書き方はまだ曖昧。"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got extractResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Learned) != 1 || len(got.NotLearned) != 1 {
		t.Errorf("抽出結果: %+v", got)
	}
}

func TestExtract_EmptyResultIsArray(t *testing.T) {
	rec := post(New(extract.RuleBased{}, discard), `{"text":"今日は雨だった。"}`)
	if body := rec.Body.String(); !strings.Contains(body, `"learned":[]`) || !strings.Contains(body, `"not_learned":[]`) {
		t.Errorf("0件は空配列で返すはず: %s", body)
	}
}

func TestExtract_Errors(t *testing.T) {
	h := New(extract.RuleBased{}, discard)
	tests := []struct {
		name, body string
		wantStatus int
		wantCode   string
	}{
		{"空のテキスト", `{"text":"  "}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"長すぎる", `{"text":"` + strings.Repeat("あ", MaxTextLen+1) + `"}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"未知のフィールド", `{"txt":"a"}`, http.StatusBadRequest, "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(h, tt.body)
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), `"code":"`+tt.wantCode+`"`) {
				t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
	if rec := post(h, `{"text":"`+strings.Repeat("あ", MaxTextLen)+`"}`); rec.Code != http.StatusOK {
		t.Errorf("上限ちょうどは受け付けるはず: %d", rec.Code)
	}
}

type failingExtractor struct{}

func (failingExtractor) Extract(context.Context, string) (extract.Result, error) {
	return extract.Result{}, errors.New("boom")
}

func TestExtract_ExtractorFailure(t *testing.T) {
	rec := post(New(failingExtractor{}, discard), `{"text":"a"}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("内部エラーは 500 で、詳細は返さないはず: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRoutes_MethodAndPath(t *testing.T) {
	h := New(extract.RuleBased{}, discard)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/extract", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Errorf("GET: status = %d, Allow = %q", rec.Code, rec.Header().Get("Allow"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知のパス: status = %d", rec.Code)
	}
}
