package board

import (
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

	"github.com/kawafuchieirin/task-manager/web/internal/taskclient"
)

func newTestHandler(t *testing.T) (*Handler, *fakeAPI) {
	t.Helper()
	api, client := newFakeAPI(t)
	h, err := NewHandler(client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, api
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
	h, api := newTestHandler(t)
	api.add("Go を学ぶ", taskclient.StatusTodo, new(90))

	rec := send(t, h, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	assertContains(t, rec.Body.String(),
		"<!doctype html>", `/static/htmx.min.js`, "みちゃくしゅ", "しんこうちゅう", "クリア",
		"Go を学ぶ", "もくひょう 1時間30分", `aria-valuenow="0"`)
}

func TestIndex_APIUnavailable(t *testing.T) {
	h, api := newTestHandler(t)
	api.down = true

	rec := send(t, h, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusBadGateway)
	body := rec.Body.String()
	// API が止まっていても、エラーを表示したページは返す。
	assertContains(t, body, "<!doctype html>", "API サーバーと つうしん できない！", "make status")
	if strings.Contains(body, `id="board"`) {
		t.Error("API に接続できないときはボードを表示しないはず")
	}
}

func TestIndex_UnknownPathIs404(t *testing.T) {
	h, _ := newTestHandler(t)
	assertStatus(t, send(t, h, http.MethodGet, "/nope", nil), http.StatusNotFound)
}

func TestIndex_EscapesUserInput(t *testing.T) {
	h, api := newTestHandler(t)
	api.add(`<script>alert(1)</script>`, taskclient.StatusTodo, nil)

	body := send(t, h, http.MethodGet, "/", nil).Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("タイトルがエスケープされずに出力されている（XSS）")
	}
	assertContains(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

func TestEscapesAttributes(t *testing.T) {
	h, api := newTestHandler(t)
	api.add(`x" onmouseover="alert(1)' &amp; <b>`, taskclient.StatusTodo, nil)

	body := send(t, h, http.MethodGet, "/", nil).Body.String()
	for _, raw := range []string{`" onmouseover="`, `<b>`} {
		if strings.Contains(body, raw) {
			t.Errorf("属性・本文から %q が抜け出している", raw)
		}
	}
	assertContains(t, body, `hx-confirm="「x&#34; onmouseover=&#34;alert(1)&#39; &amp;amp; &lt;b&gt;」を すてますか？"`)
}

func TestCreate(t *testing.T) {
	h, api := newTestHandler(t)

	rec := send(t, h, http.MethodPost, "/tasks", url.Values{"title": {"新しいタスク"}, "estimated_min": {"30"}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if strings.Contains(body, "<!doctype html>") {
		t.Error("htmx 用にはボードの断片だけを返すはず")
	}
	assertContains(t, body, `id="board"`, "新しいタスク", "もくひょう 30分")

	got, ok := api.get(1)
	if !ok || got.EstimatedMin == nil || *got.EstimatedMin != 30 {
		t.Errorf("API に保存されていない: %+v", got)
	}
}

func TestCreate_ValidationKeepsInput(t *testing.T) {
	tests := []struct {
		name    string
		form    url.Values
		wantMsg string
	}{
		{"API の検証エラー（タイトルが空）", url.Values{"title": {""}, "description": {"残したい説明"}}, "タスクの なまえを いれてください。"},
		{"目標時間が数値でない", url.Values{"title": {"t"}, "description": {"残したい説明"}, "estimated_min": {"abc"}},
			"もくひょうは ふんを すうじで いれてください。"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, api := newTestHandler(t)
			rec := send(t, h, http.MethodPost, "/tasks", tt.form)

			assertStatus(t, rec, http.StatusUnprocessableEntity)
			assertContains(t, rec.Body.String(), tt.wantMsg, "残したい説明", `aria-invalid="true"`)
			if _, ok := api.get(1); ok {
				t.Error("検証エラーなのに保存された")
			}
		})
	}
}

func TestCreate_NormalizesCRLF(t *testing.T) {
	h, api := newTestHandler(t)
	rec := send(t, h, http.MethodPost, "/tasks", url.Values{"title": {"t"}, "description": {"a\r\nb"}})
	assertStatus(t, rec, http.StatusOK)

	got, _ := api.get(1)
	if got.Description != "a\nb" {
		t.Errorf("Description = %q, CRLF は LF に揃えて API に送るはず", got.Description)
	}
}

func TestChangeStatus_DoneShowsStrikethroughAndProgress(t *testing.T) {
	h, api := newTestHandler(t)
	a := api.add("a", taskclient.StatusTodo, nil)
	api.add("b", taskclient.StatusTodo, nil)

	rec := send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"done"}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	// 取り消し線は .card--done に CSS で付ける。完了列に入り、完了日時が表示される。
	if !regexp.MustCompile(`class="card card--done" id="task-` + strconv.FormatInt(a.ID, 10) + `"`).MatchString(body) {
		t.Error("完了したタスクに card--done クラスが付いていない")
	}
	assertContains(t, body, `aria-valuenow="50"`, "<strong>50%</strong>", "クリア 10/3", "やりなおす")

	if got, _ := api.get(a.ID); got.Status != taskclient.StatusDone {
		t.Errorf("Status = %q", got.Status)
	}
}

func TestChangeStatus_UndoDone(t *testing.T) {
	h, api := newTestHandler(t)
	created := api.add("a", taskclient.StatusDone, nil)

	rec := send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"doing"}})
	assertStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "card--done") {
		t.Error("完了を取り消したら取り消し線が消えるはず")
	}
	got, _ := api.get(created.ID)
	if got.Status != taskclient.StatusDoing || got.CompletedAt != nil {
		t.Errorf("完了取り消し後: %+v", got)
	}
}

func TestChangeStatus_Errors(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("a", taskclient.StatusTodo, nil)

	assertStatus(t, send(t, h, http.MethodPost, "/tasks/999/status", url.Values{"status": {"done"}}), http.StatusNotFound)
	assertStatus(t, send(t, h, http.MethodPost, "/tasks/abc/status", url.Values{"status": {"done"}}), http.StatusNotFound)
	// 422 にすると htmx がボードを平文で置き換えてしまうため 400 で返す。
	assertStatus(t, send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"archived"}}), http.StatusBadRequest)
}

func TestMutation_APIUnavailable(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("a", taskclient.StatusTodo, nil)
	api.down = true

	rec := send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"done"}})
	assertStatus(t, rec, http.StatusBadGateway)
	assertContains(t, rec.Body.String(), "API サーバーと つうしん できない！")
}

func TestEditAndUpdate(t *testing.T) {
	h, api := newTestHandler(t)
	created := api.add("before", taskclient.StatusTodo, new(30))

	rec := send(t, h, http.MethodGet, "/tasks/1/edit", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `hx-put="/tasks/1"`, `value="before"`, `value="30"`)

	rec = send(t, h, http.MethodPut, "/tasks/1", url.Values{"title": {"after"}, "description": {""}, "estimated_min": {""}})
	assertStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), `hx-put=`) {
		t.Error("保存後は編集フォームを閉じるはず")
	}

	got, _ := api.get(created.ID)
	if got.Title != "after" || got.Description != "" || got.EstimatedMin != nil {
		t.Errorf("更新結果: %+v（空欄の目標時間は未設定に戻る）", got)
	}
}

func TestEdit_DoneTaskKeepsStatus(t *testing.T) {
	h, api := newTestHandler(t)
	created := api.add("before", taskclient.StatusDone, nil)

	assertStatus(t, send(t, h, http.MethodPut, "/tasks/1", url.Values{"title": {"after"}}), http.StatusOK)

	got, _ := api.get(created.ID)
	if got.Status != taskclient.StatusDone || got.CompletedAt == nil || !got.CompletedAt.Equal(*created.CompletedAt) {
		t.Errorf("完了済みタスクを編集してもステータスと完了日時は変わらないはず: %+v", got)
	}
}

func TestUpdate_ValidationKeepsEditing(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("before", taskclient.StatusTodo, nil)

	rec := send(t, h, http.MethodPut, "/tasks/1", url.Values{"title": {""}, "description": {"編集中の説明"}})
	assertStatus(t, rec, http.StatusUnprocessableEntity)
	assertContains(t, rec.Body.String(), `hx-put="/tasks/1"`, "タスクの なまえを いれてください。", "編集中の説明")

	if got, _ := api.get(1); got.Title != "before" {
		t.Errorf("検証エラーなのに更新された: %q", got.Title)
	}
}

func TestEdit_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	assertStatus(t, send(t, h, http.MethodGet, "/tasks/999/edit", nil), http.StatusNotFound)
	assertStatus(t, send(t, h, http.MethodPut, "/tasks/999", url.Values{"title": {"x"}}), http.StatusNotFound)
}

func TestDelete(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("消すタスク", taskclient.StatusTodo, nil)

	rec := send(t, h, http.MethodDelete, "/tasks/1", nil)
	assertStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "消すタスク") {
		t.Error("削除したタスクが表示されている")
	}
	assertContains(t, rec.Body.String(), "タスクは ない ようだ。", "（0こ のうち 0こ クリア）")
	assertStatus(t, send(t, h, http.MethodDelete, "/tasks/1", nil), http.StatusNotFound)
}

func TestDoneColumnOrder(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("先に完了", taskclient.StatusDone, nil)
	api.now = api.now.Add(time.Minute)
	api.add("後で完了", taskclient.StatusDone, nil)
	api.add("同じ秒に後から作成", taskclient.StatusDone, nil)

	body := send(t, h, http.MethodGet, "/board", nil).Body.String()
	first, second, third := strings.Index(body, "同じ秒に後から作成"), strings.Index(body, "後で完了"), strings.Index(body, "先に完了")
	if first > second || second > third {
		t.Error("完了列は直近に完了したもの（同じ秒なら後から作ったもの）が上に来るはず")
	}
}

func TestStatic(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, path := range []string{"/static/htmx.min.js", "/static/app.js", "/static/app.css",
		"/static/fonts/DotGothic16-Regular.ttf", "/static/fonts/DotGothic16-OFL.txt"} {
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

func TestProgressPercent(t *testing.T) {
	tests := []struct{ done, total, want int }{
		{0, 0, 0}, {1, 1, 100}, {1, 3, 33}, {2, 3, 66},
		{999, 1000, 99}, // 全件完了でなければ 100 にならない
	}
	for _, tt := range tests {
		if got := progressPercent(tt.done, tt.total); got != tt.want {
			t.Errorf("progressPercent(%d, %d) = %d, want %d", tt.done, tt.total, got, tt.want)
		}
	}
}
