package taskclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	opts = append([]Option{WithRetry(2, time.Millisecond)}, opts...)
	c, err := New(srv.URL, "", opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestNew_InvalidURL(t *testing.T) {
	for _, u := range []string{"", "127.0.0.1:8080", "ftp://x", "http://"} {
		if _, err := New(u, ""); err == nil {
			t.Errorf("New(%q) はエラーになるはず", u)
		}
	}
}

func TestList(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/tasks" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, `{"tasks":[{"id":1,"title":"a","status":"done","estimated_min":30,
			"completed_at":"2026-10-03T09:00:00Z","created_at":"2026-10-03T08:00:00Z","updated_at":"2026-10-03T09:00:00Z"}]}`)
	})
	tasks, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Title != "a" || tasks[0].Status != StatusDone ||
		*tasks[0].EstimatedMin != 30 || tasks[0].CompletedAt == nil {
		t.Errorf("デコード結果: %+v", tasks)
	}
}

func TestCreate_SendsJSONAndAPIKey(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-key-0123456" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeJSON(w, 201, `{"id":7,"title":"t","status":"todo"}`)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "secret-key-0123456")
	if err != nil {
		t.Fatal(err)
	}

	created, err := c.Create(context.Background(), CreateInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 7 {
		t.Errorf("ID = %d", created.ID)
	}
	if _, ok := got["estimated_min"]; ok {
		t.Errorf("未設定の目標時間は送らないはず: %v", got)
	}
}

func TestUpdateInput_MarshalJSON(t *testing.T) {
	title, m := "x", 30
	tests := []struct {
		name string
		in   UpdateInput
		want string
	}{
		{"何も指定しない", UpdateInput{}, `{}`},
		{"タイトルのみ", UpdateInput{Title: &title}, `{"title":"x"}`},
		{"ゴールを消す", UpdateInput{Goal: new("")}, `{"goal":""}`},
		{"目標時間を設定", UpdateInput{SetEstimatedMin: true, EstimatedMin: &m}, `{"estimated_min":30}`},
		{"目標時間を未設定に戻す", UpdateInput{SetEstimatedMin: true}, `{"estimated_min":null}`},
	}
	for _, tt := range tests {
		got, err := json.Marshal(tt.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		check  func(t *testing.T, err error)
	}{
		{"404 は ErrNotFound", 404, `{"error":{"code":"not_found","message":"x"}}`, func(t *testing.T, err error) {
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v", err)
			}
		}},
		{"422 は ValidationError", 422,
			`{"error":{"code":"validation_failed","message":"x","details":[{"field":"title","message":"タイトルを入力してください"}]}}`,
			func(t *testing.T, err error) {
				var ve *ValidationError
				if !errors.As(err, &ve) || ve.Message("title") != "タイトルを入力してください" {
					t.Errorf("err = %v", err)
				}
			}},
		{"401 は APIError", 401, `{"error":{"code":"unauthorized","message":"API キーが無効です"}}`, func(t *testing.T, err error) {
			var ae *APIError
			if !errors.As(err, &ae) || ae.StatusCode != 401 || ae.Code != "unauthorized" {
				t.Errorf("err = %v", err)
			}
		}},
		{"JSON でない 421 も APIError", 421, "許可されていないホストです\n", func(t *testing.T, err error) {
			var ae *APIError
			if !errors.As(err, &ae) || ae.Message != "許可されていないホストです" {
				t.Errorf("err = %v", err)
			}
		}},
		{"500 は ErrUnavailable", 500, `{"error":{"code":"internal_error","message":"x"}}`, func(t *testing.T, err error) {
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("err = %v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tt.status, tt.body) })
			_, err := c.Get(context.Background(), 1)
			tt.check(t, err)
		})
	}
}

func TestGet_RetriesTransientErrors(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			writeJSON(w, 503, `{}`)
			return
		}
		writeJSON(w, 200, `{"id":1,"title":"ok"}`)
	})
	got, err := c.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("2回の 503 の後は成功するはず: %v", err)
	}
	if got.Title != "ok" || calls.Load() != 3 {
		t.Errorf("title = %q, calls = %d", got.Title, calls.Load())
	}
}

func TestGet_GivesUpAfterRetries(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 503, `{}`)
	})
	if _, err := c.Get(context.Background(), 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3（初回 + 再試行2回）", calls.Load())
	}
}

func TestWrites_AreNotRetried(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 503, `{}`)
	})
	ctx := context.Background()
	_, _ = c.Create(ctx, CreateInput{Title: "t"})
	_, _ = c.Update(ctx, 1, UpdateInput{})
	_ = c.Delete(ctx, 1)
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3（二重登録を防ぐため書き込みは再試行しない）", calls.Load())
	}
}

func TestUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // 停止中の API を再現する
	c, err := New(url, "", WithRetry(1, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
}

func TestTimeout(t *testing.T) {
	block := make(chan struct{})
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-block
	}, WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}), WithRetry(0, 0))
	// httptest.Server.Close は処理中のリクエストの終了を待つので、Close より先にハンドラを解放する。
	// （Cleanup は登録の逆順に実行されるため、サーバーを作った後に登録する）
	t.Cleanup(func() { close(block) })

	start := time.Now()
	_, err := c.List(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("タイムアウトせず %v 待った", elapsed)
	}
}

func TestDelete_NoContent(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/tasks/5" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.Delete(context.Background(), 5); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestTimer(t *testing.T) {
	var calls []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/tasks/3/timer/start":
			writeJSON(w, 409, `{"error":{"code":"timer_already_running","message":"「A」のタイマーが動いています。先に停止してください"}}`)
		case "/api/v1/tasks/3/timer/stop":
			writeJSON(w, 200, `{"id":9,"task_id":3,"started_at":"2026-10-03T09:00:00Z","ended_at":"2026-10-03T09:30:00Z","duration_sec":1800}`)
		}
	})
	ctx := context.Background()

	_, err := c.StartTimer(ctx, 3)
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != 409 || ae.Code != "timer_already_running" || ae.Message == "" {
		t.Errorf("409 は APIError（メッセージ付き）: %v", err)
	}
	e, err := c.StopTimer(ctx, 3)
	if err != nil || e.DurationSec != 1800 || e.EndedAt == nil {
		t.Errorf("停止: %+v, %v", e, err)
	}
	if len(calls) != 2 || calls[0] != "POST /api/v1/tasks/3/timer/start" {
		t.Errorf("呼び出し: %v（書き込みは再試行しない）", calls)
	}
}

func TestTimeEntries(t *testing.T) {
	var body map[string]string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			writeJSON(w, 200, `{"time_entries":[{"id":1,"task_id":2,"started_at":"2026-10-03T09:00:00Z","ended_at":null,"duration_sec":60}]}`)
		case r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, 201, `{"id":2,"task_id":2,"started_at":"2026-10-03T08:00:00Z","ended_at":"2026-10-03T08:30:00Z"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/time-entries/2":
			w.WriteHeader(204)
		default:
			writeJSON(w, 404, `{"error":{"code":"not_found","message":"時間記録が見つかりません"}}`)
		}
	})
	ctx := context.Background()

	entries, err := c.ListTimeEntries(ctx, 2)
	if err != nil || len(entries) != 1 || entries[0].EndedAt != nil {
		t.Errorf("一覧: %+v, %v", entries, err)
	}

	jst := time.FixedZone("JST", 9*60*60)
	start := time.Date(2026, 10, 3, 17, 0, 0, 0, jst)
	if _, err := c.AddTimeEntry(ctx, 2, start, start.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if body["started_at"] != "2026-10-03T08:00:00Z" || body["ended_at"] != "2026-10-03T08:30:00Z" {
		t.Errorf("UTC で送るはず: %v", body)
	}

	if err := c.DeleteTimeEntry(ctx, 2); err != nil {
		t.Errorf("削除: %v", err)
	}
	err = c.DeleteTimeEntry(ctx, 99)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "時間記録が見つかりません") {
		t.Errorf("存在しない区間: %v", err)
	}
}

func TestReflections(t *testing.T) {
	var gotQuery, gotBody string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/tasks/1/reflection":
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			writeJSON(w, 200, `{"task_id":1,"body":"+ a","learned":["a"],"not_learned":[],"extract_status":"ok","updated_at":"2026-10-03T09:00:00Z"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/1/reflection/extract":
			writeJSON(w, 200, `{"task_id":1,"body":"+ a","learned":[],"not_learned":[],"extract_status":"failed","updated_at":"2026-10-03T09:00:00Z"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/tasks/1/reflection":
			w.WriteHeader(204)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/reflections":
			gotQuery = r.URL.RawQuery
			writeJSON(w, 200, `{"reflections":[{"task_id":1,"task_title":"スライム","task_status":"done","body":"x","learned":["a"],"not_learned":[],"extract_status":"ok","updated_at":"2026-10-03T09:00:00Z"}]}`)
		default:
			t.Errorf("想定外: %s %s", r.Method, r.URL.String())
			writeJSON(w, 404, `{"error":{"code":"not_found","message":"x"}}`)
		}
	})
	ctx := context.Background()

	r, err := c.SaveReflection(ctx, 1, "+ a")
	if err != nil || r.ExtractStatus != ExtractOK || len(r.Learned) != 1 || gotBody != `{"body":"+ a"}` {
		t.Errorf("保存: %+v, %v, body=%s", r, err, gotBody)
	}
	if r, err := c.ExtractReflection(ctx, 1); err != nil || r.ExtractStatus != ExtractFailed {
		t.Errorf("やり直し: %+v, %v", r, err)
	}
	if err := c.DeleteReflection(ctx, 1); err != nil {
		t.Errorf("削除: %v", err)
	}

	jst := time.FixedZone("JST", 9*60*60)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, jst)
	to := from.AddDate(0, 0, 7)
	list, err := c.ListReflections(ctx, &from, &to)
	if err != nil || len(list) != 1 || list[0].TaskTitle != "スライム" || list[0].TaskStatus != StatusDone {
		t.Errorf("一覧: %+v, %v", list, err)
	}
	if gotQuery != "from=2026-10-01T00%3A00%3A00%2B09%3A00&to=2026-10-08T00%3A00%3A00%2B09%3A00" {
		t.Errorf("クエリはパスと分けて送るはず: %q", gotQuery)
	}
	if _, err := c.ListReflections(ctx, nil, nil); err != nil || gotQuery != "" {
		t.Errorf("期間なし: %v, query=%q", err, gotQuery)
	}
}
