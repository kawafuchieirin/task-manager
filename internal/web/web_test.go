package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kawafuchieirin/task-manager/internal/db/dbtest"
	"github.com/kawafuchieirin/task-manager/internal/task"
)

func newTestHandler(t *testing.T) (*Handler, *task.Service) {
	t.Helper()
	svc := task.NewService(dbtest.New(t))
	h, err := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, svc
}

func send(t *testing.T, h http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustCreate(t *testing.T, svc *task.Service, in task.CreateInput) task.Task {
	t.Helper()
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, want, rec.Body.String())
	}
}

func assertContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("レスポンスに %q が含まれない", want)
		}
	}
}

func TestIndex(t *testing.T) {
	h, svc := newTestHandler(t)
	mustCreate(t, svc, task.CreateInput{Title: "Go を学ぶ", EstimatedMin: new(90)})

	rec := send(t, h, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	assertContains(t, rec.Body.String(),
		"<!doctype html>", `/static/htmx.min.js`, "未着手", "進行中", "完了",
		"Go を学ぶ", "目標 1時間30分", `aria-valuenow="0"`)
}

func TestIndex_UnknownPathIs404(t *testing.T) {
	h, _ := newTestHandler(t)
	assertStatus(t, send(t, h, http.MethodGet, "/nope", nil), http.StatusNotFound)
}

func TestIndex_EscapesUserInput(t *testing.T) {
	h, svc := newTestHandler(t)
	mustCreate(t, svc, task.CreateInput{Title: `<script>alert(1)</script>`})

	body := send(t, h, http.MethodGet, "/", nil).Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("タイトルがエスケープされずに出力されている（XSS）")
	}
	assertContains(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

func TestCreate(t *testing.T) {
	h, svc := newTestHandler(t)

	rec := send(t, h, http.MethodPost, "/tasks", url.Values{"title": {"新しいタスク"}, "estimated_min": {"30"}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if strings.Contains(body, "<!doctype html>") {
		t.Error("htmx 用にはボードの断片だけを返すはず")
	}
	assertContains(t, body, `id="board"`, "新しいタスク", "目標 30分")

	tasks, err := svc.List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].EstimatedMin == nil || *tasks[0].EstimatedMin != 30 {
		t.Errorf("DB に保存されていない: %+v", tasks)
	}
}

func TestCreate_ValidationKeepsInput(t *testing.T) {
	tests := []struct {
		name    string
		form    url.Values
		wantMsg string
	}{
		{"タイトルが空", url.Values{"title": {""}, "description": {"残したい説明"}}, "タイトルを入力してください"},
		{"目標時間が数値でない", url.Values{"title": {"t"}, "description": {"残したい説明"}, "estimated_min": {"abc"}},
			"目標時間は整数（分）で入力してください"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, svc := newTestHandler(t)
			rec := send(t, h, http.MethodPost, "/tasks", tt.form)

			assertStatus(t, rec, http.StatusUnprocessableEntity)
			assertContains(t, rec.Body.String(), tt.wantMsg, "残したい説明", `aria-invalid="true"`)
			if tasks, _ := svc.List(context.Background(), nil); len(tasks) != 0 {
				t.Errorf("検証エラーなのに保存された: %+v", tasks)
			}
		})
	}
}

func TestChangeStatus_DoneShowsStrikethroughAndProgress(t *testing.T) {
	h, svc := newTestHandler(t)
	a := mustCreate(t, svc, task.CreateInput{Title: "a"})
	mustCreate(t, svc, task.CreateInput{Title: "b"})

	rec := send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"done"}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	// 取り消し線は .card--done に CSS で付ける。完了列に入り、完了日時が表示される。
	if !regexp.MustCompile(`class="card card--done" id="task-` + itoa(a.ID) + `"`).MatchString(body) {
		t.Error("完了したタスクに card--done クラスが付いていない")
	}
	assertContains(t, body, `aria-valuenow="50"`, "<strong>50%</strong>", "完了 ", "未完了に戻す")

	got, err := svc.Get(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusDone {
		t.Errorf("Status = %q", got.Status)
	}
}

func TestChangeStatus_UndoDone(t *testing.T) {
	h, svc := newTestHandler(t)
	created := mustCreate(t, svc, task.CreateInput{Title: "a", Status: task.StatusDone})

	rec := send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"doing"}})
	assertStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "card--done") {
		t.Error("完了を取り消したら取り消し線が消えるはず")
	}
	got, _ := svc.Get(context.Background(), created.ID)
	if got.Status != task.StatusDoing || got.CompletedAt != nil {
		t.Errorf("完了取り消し後: %+v", got)
	}
}

func TestChangeStatus_Errors(t *testing.T) {
	h, svc := newTestHandler(t)
	mustCreate(t, svc, task.CreateInput{Title: "a"})

	assertStatus(t, send(t, h, http.MethodPost, "/tasks/999/status", url.Values{"status": {"done"}}), http.StatusNotFound)
	assertStatus(t, send(t, h, http.MethodPost, "/tasks/abc/status", url.Values{"status": {"done"}}), http.StatusNotFound)
	assertStatus(t, send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"archived"}}), http.StatusUnprocessableEntity)
}

func TestEditAndUpdate(t *testing.T) {
	h, svc := newTestHandler(t)
	created := mustCreate(t, svc, task.CreateInput{Title: "before", Description: "desc", EstimatedMin: new(30)})

	rec := send(t, h, http.MethodGet, "/tasks/1/edit", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `hx-put="/tasks/1"`, `value="before"`, `value="30"`)

	rec = send(t, h, http.MethodPut, "/tasks/1", url.Values{"title": {"after"}, "description": {""}, "estimated_min": {""}})
	assertStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), `hx-put=`) {
		t.Error("保存後は編集フォームを閉じるはず")
	}

	got, _ := svc.Get(context.Background(), created.ID)
	if got.Title != "after" || got.Description != "" || got.EstimatedMin != nil {
		t.Errorf("更新結果: %+v（空欄の目標時間は未設定に戻る）", got)
	}
}

func TestUpdate_ValidationKeepsEditing(t *testing.T) {
	h, svc := newTestHandler(t)
	mustCreate(t, svc, task.CreateInput{Title: "before"})

	rec := send(t, h, http.MethodPut, "/tasks/1", url.Values{"title": {""}, "description": {"編集中の説明"}})
	assertStatus(t, rec, http.StatusUnprocessableEntity)
	assertContains(t, rec.Body.String(), `hx-put="/tasks/1"`, "タイトルを入力してください", "編集中の説明")

	got, _ := svc.Get(context.Background(), 1)
	if got.Title != "before" {
		t.Errorf("検証エラーなのに更新された: %q", got.Title)
	}
}

func TestEdit_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	assertStatus(t, send(t, h, http.MethodGet, "/tasks/999/edit", nil), http.StatusNotFound)
	assertStatus(t, send(t, h, http.MethodPut, "/tasks/999", url.Values{"title": {"x"}}), http.StatusNotFound)
}

func TestDelete(t *testing.T) {
	h, svc := newTestHandler(t)
	mustCreate(t, svc, task.CreateInput{Title: "消すタスク"})

	rec := send(t, h, http.MethodDelete, "/tasks/1", nil)
	assertStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "消すタスク") {
		t.Error("削除したタスクが表示されている")
	}
	assertContains(t, rec.Body.String(), "タスクはありません", "（0 / 0 件完了）")
	assertStatus(t, send(t, h, http.MethodDelete, "/tasks/1", nil), http.StatusNotFound)
}

func TestDoneColumnOrderedByCompletedAtDesc(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	svc := task.NewService(dbtest.New(t), task.WithClock(func() time.Time { return now }))
	h, err := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := mustCreate(t, svc, task.CreateInput{Title: "先に完了"})
	second := mustCreate(t, svc, task.CreateInput{Title: "後で完了"})
	done := task.StatusDone
	if _, err := svc.Update(ctx, first.ID, task.UpdateInput{Status: &done}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := svc.Update(ctx, second.ID, task.UpdateInput{Status: &done}); err != nil {
		t.Fatal(err)
	}

	body := send(t, h, http.MethodGet, "/board", nil).Body.String()
	if strings.Index(body, "後で完了") > strings.Index(body, "先に完了") {
		t.Error("完了列は直近に完了したものが上に来るはず")
	}
}

func TestStatic(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, path := range []string{"/static/htmx.min.js", "/static/app.js", "/static/app.css"} {
		rec := send(t, h, http.MethodGet, path, nil)
		assertStatus(t, rec, http.StatusOK)
		if rec.Body.Len() == 0 {
			t.Errorf("%s が空", path)
		}
	}
	assertStatus(t, send(t, h, http.MethodGet, "/static/nope.js", nil), http.StatusNotFound)
}

func TestFormatMinutes(t *testing.T) {
	tests := map[int]string{0: "0分", 45: "45分", 60: "1時間", 90: "1時間30分", 600: "10時間"}
	for in, want := range tests {
		if got := formatMinutes(in); got != want {
			t.Errorf("formatMinutes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatJST(t *testing.T) {
	got := formatJST(time.Date(2026, 10, 3, 15, 30, 0, 0, time.UTC))
	if got != "10/4 00:30" {
		t.Errorf("formatJST = %q, want 10/4 00:30（UTC+9 で日付が変わる）", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
