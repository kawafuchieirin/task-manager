package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAPI は tm が使う Task API の一部を真似る、テスト用のインメモリ実装。
type fakeAPI struct {
	mu      sync.Mutex
	tasks   []map[string]any
	running int64 // タイマーが動いているタスク（0 なら無し）
	authKey string
	lastReq struct{ method, path, auth string }
}

func newFake(t *testing.T) (*fakeAPI, string) {
	t.Helper()
	f := &fakeAPI{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, code, msg string, details ...map[string]string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg, "details": details}})
}

func (f *fakeAPI) find(id int64) map[string]any {
	for _, t := range f.tasks {
		if t["id"].(int64) == id {
			return t
		}
	}
	return nil
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReq.method, f.lastReq.path, f.lastReq.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
	if f.authKey != "" && r.Header.Get("Authorization") != "Bearer "+f.authKey {
		apiError(w, 401, "unauthorized", "API キーが無効です")
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks":
		writeJSON(w, 200, map[string]any{"tasks": f.tasks})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if strings.TrimSpace(in["title"].(string)) == "" {
			apiError(w, 422, "validation_failed", "入力値が不正です", map[string]string{"field": "title", "code": "required", "message": "タイトルを入力してください"})
			return
		}
		task := map[string]any{"id": int64(len(f.tasks) + 1), "title": in["title"], "description": in["description"],
			"status": "todo", "estimated_min": in["estimated_min"], "actual_sec": 0, "running_since": nil}
		f.tasks = append(f.tasks, task)
		writeJSON(w, 201, task)
	case strings.HasPrefix(r.URL.Path, "/api/v1/tasks/"):
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
		idStr, action, _ := strings.Cut(rest, "/")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		task := f.find(id)
		if task == nil {
			apiError(w, 404, "not_found", "タスクが見つかりません")
			return
		}
		switch {
		case action == "" && r.Method == http.MethodGet:
			writeJSON(w, 200, task)
		case action == "" && r.Method == http.MethodPatch:
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if s, ok := in["status"]; ok {
				task["status"] = s
				if s == "done" && f.running == id {
					f.running, task["running_since"] = 0, nil
				}
			}
			writeJSON(w, 200, task)
		case action == "timer/start":
			if f.running != 0 {
				apiError(w, 409, "timer_already_running", "「"+f.find(f.running)["title"].(string)+"」のタイマーが動いています。先に停止してください")
				return
			}
			f.running, task["status"], task["running_since"] = id, "doing", time.Now().UTC().Format(time.RFC3339)
			writeJSON(w, 201, map[string]any{"id": 1, "task_id": id, "started_at": task["running_since"], "ended_at": nil, "duration_sec": 0})
		case action == "timer/stop":
			if f.running != id {
				apiError(w, 409, "timer_not_running", "このタスクのタイマーは動いていません")
				return
			}
			f.running, task["running_since"], task["actual_sec"] = 0, nil, 1500
			writeJSON(w, 200, map[string]any{"id": 1, "task_id": id, "started_at": time.Now().UTC().Format(time.RFC3339),
				"ended_at": time.Now().UTC().Format(time.RFC3339), "duration_sec": 1500})
		default:
			apiError(w, 404, "not_found", "x")
		}
	default:
		apiError(w, 404, "not_found", "x")
	}
}

// run は tm を実行し、終了コード・標準出力・標準エラーを返す。
func run(t *testing.T, apiURL string, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	getenv := func(k string) string {
		if k == "TM_API_URL" {
			return apiURL
		}
		return env[k]
	}
	code := Run(context.Background(), args, getenv, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestAdd(t *testing.T) {
	f, url := newFake(t)
	tests := []struct {
		args      []string
		wantTitle string
		wantEst   any
		wantDesc  string
	}{
		{[]string{"add", "Go を学ぶ", "-e", "30"}, "Go を学ぶ", float64(30), ""},
		{[]string{"add", "-e", "45", "htmx", "を", "読む"}, "htmx を 読む", float64(45), ""}, // 引用符なしの複数語・オプションが先
		{[]string{"add", "テスト", "--desc", "テーブル駆動", "--estimate", "10"}, "テスト", float64(10), "テーブル駆動"},
		{[]string{"add", "--", "-e で始まる名前"}, "-e で始まる名前", nil, ""},
	}
	for i, tt := range tests {
		code, out, errOut := run(t, url, nil, tt.args...)
		if code != 0 {
			t.Fatalf("%v: code=%d stderr=%s", tt.args, code, errOut)
		}
		want := "＊ 「" + tt.wantTitle + "」が あらわれた！ (#" + strconv.Itoa(i+1) + ")\n"
		if out != want {
			t.Errorf("%v: 出力 %q, want %q", tt.args, out, want)
		}
		got := f.tasks[i]
		if got["title"] != tt.wantTitle || got["estimated_min"] != tt.wantEst || (tt.wantDesc != "" && got["description"] != tt.wantDesc) {
			t.Errorf("%v: 送った内容 %v", tt.args, got)
		}
	}
}

func TestAdd_UsageErrors(t *testing.T) {
	_, url := newFake(t)
	for _, args := range [][]string{
		{"add"},
		{"add", "-e"},
		{"add", "x", "-e", "abc"},
		{"add", "x", "--unknown"},
		{"add", "x", "-d"},
	} {
		code, out, errOut := run(t, url, nil, args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "つかいかた:") {
			t.Errorf("%v: code=%d（2 のはず） stdout=%q stderr=%.60q", args, code, out, errOut)
		}
	}
}

func TestAdd_ValidationFromAPI(t *testing.T) {
	_, url := newFake(t)
	code, _, errOut := run(t, url, nil, "add", "  ")
	if code != 1 || !strings.Contains(errOut, "タイトルを入力してください") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestList(t *testing.T) {
	f, url := newFake(t)
	run(t, url, nil, "add", "Go を学ぶ", "-e", "90")
	run(t, url, nil, "add", "htmx")
	run(t, url, nil, "add", "クリア済み")
	f.tasks[2]["status"] = "done"
	run(t, url, nil, "start", "1")

	code, out, _ := run(t, url, nil, "ls")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"しんちょく 33%（3こ のうち 1こ クリア）", "#1    [しんこうちゅう] Go を学ぶ  もくひょう 1時間30分  ⏱ けいそくちゅう", "#2    [みちゃくしゅ] htmx"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "クリア済み") {
		t.Error("既定ではクリアしたタスクを出さないはず")
	}
	_, all, _ := run(t, url, nil, "ls", "-a")
	if !strings.Contains(all, "#3    [クリア] クリア済み") {
		t.Errorf("-a でクリアしたタスクも出すはず:\n%s", all)
	}
	if code, _, _ := run(t, url, nil, "ls", "--bogus"); code != 2 {
		t.Errorf("不明なオプション: code=%d", code)
	}
}

func TestList_Empty(t *testing.T) {
	_, url := newFake(t)
	_, out, _ := run(t, url, nil, "ls")
	if out != "しんちょく 0%（0こ のうち 0こ クリア）\nタスクは ない ようだ。\n" {
		t.Errorf("出力 %q", out)
	}
}

func TestTimerAndDone(t *testing.T) {
	f, url := newFake(t)
	run(t, url, nil, "add", "スライム")
	run(t, url, nil, "add", "ドラキー")

	if _, out, _ := run(t, url, nil, "start", "#1"); out != "＊ 「スライム」との たたかいが はじまった！ (#1)\n" {
		t.Errorf("start: %q", out)
	}
	code, _, errOut := run(t, url, nil, "start", "2")
	if code != 1 || !strings.Contains(errOut, "「スライム」のタイマーが動いています") || !strings.Contains(errOut, "tm stop") {
		t.Errorf("二重起動: code=%d stderr=%q", code, errOut)
	}
	// id を省くと、動いているタイマーを止める。
	if _, out, _ := run(t, url, nil, "stop"); out != "＊ 「スライム」との たたかいを おえた。 こんかい 25分、 じっせき ごうけい 25分。\n" {
		t.Errorf("stop: %q", out)
	}
	if code, _, errOut := run(t, url, nil, "stop"); code != 1 || !strings.Contains(errOut, "うごいている タイマーは ない。") {
		t.Errorf("止めるタイマーが無い: code=%d stderr=%q", code, errOut)
	}
	if _, out, _ := run(t, url, nil, "done", "1"); out != "＊ 「スライム」を やっつけた！ (#1)\n" || f.tasks[0]["status"] != "done" {
		t.Errorf("done: %q, status=%v", out, f.tasks[0]["status"])
	}
}

func TestIDErrors(t *testing.T) {
	_, url := newFake(t)
	for _, args := range [][]string{{"start"}, {"done", "abc"}, {"done", "0"}, {"start", "1", "2"}} {
		if code, _, errOut := run(t, url, nil, args...); code != 2 || !strings.Contains(errOut, "id") {
			t.Errorf("%v: code=%d stderr=%q", args, code, errOut)
		}
	}
	if code, _, errOut := run(t, url, nil, "done", "99"); code != 1 || !strings.Contains(errOut, "みつからない") {
		t.Errorf("存在しない id: code=%d stderr=%q", code, errOut)
	}
}

func TestAPIKeyAndConnection(t *testing.T) {
	f, url := newFake(t)
	f.authKey = "secret-key-0123456"
	if code, _, errOut := run(t, url, nil, "ls"); code != 1 || !strings.Contains(errOut, "TM_API_KEY") {
		t.Errorf("キーなし: code=%d stderr=%q", code, errOut)
	}
	if code, _, _ := run(t, url, map[string]string{"TM_API_KEY": "secret-key-0123456"}, "ls"); code != 0 || f.lastReq.auth != "Bearer secret-key-0123456" {
		t.Errorf("キーあり: code=%d auth=%q", code, f.lastReq.auth)
	}

	srv := httptest.NewServer(http.NotFoundHandler())
	down := srv.URL
	srv.Close()
	code, _, errOut := run(t, down, nil, "ls")
	if code != 1 || !strings.Contains(errOut, "つうしん できない") || !strings.Contains(errOut, down) {
		t.Errorf("API 停止中: code=%d stderr=%q", code, errOut)
	}
	if code, _, _ := run(t, "not a url", nil, "ls"); code != 2 {
		t.Errorf("不正な TM_API_URL: code=%d", code)
	}
}

func TestHelpAndUnknown(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"-h"}} {
		var out bytes.Buffer
		if code := Run(context.Background(), args, func(string) string { return "" }, &out, io.Discard); code != 0 || !strings.Contains(out.String(), "tm add") {
			t.Errorf("%v: code=%d", args, code)
		}
	}
	_, url := newFake(t)
	if code, _, errOut := run(t, url, nil, "fly"); code != 2 || !strings.Contains(errOut, "しらない コマンド") {
		t.Errorf("不明なコマンド: code=%d stderr=%q", code, errOut)
	}
}

func TestMinutes(t *testing.T) {
	for sec, want := range map[int64]string{0: "0分", 30: "1分 みまん", 60: "1分", 5400: "1時間30分", 7200: "2時間"} {
		if got := minutes(sec); got != want {
			t.Errorf("minutes(%d) = %q, want %q", sec, got, want)
		}
	}
}

// failWriter は書き込みに必ず失敗する（閉じたパイプへの出力の再現）。
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRun_WriteFailureIsError(t *testing.T) {
	_, url := newFake(t)
	getenv := func(k string) string {
		if k == "TM_API_URL" {
			return url
		}
		return ""
	}
	if code := Run(context.Background(), []string{"ls"}, getenv, failWriter{}, io.Discard); code != 1 {
		t.Errorf("出力に失敗したら終了コード 1 のはず: %d", code)
	}
}
