package taskclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	t.Cleanup(func() { close(block) })
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}, WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}), WithRetry(0, 0))

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
