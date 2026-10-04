package insightclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kawafuchieirin/task-manager/api/internal/task"
)

var _ task.Extractor = (*Client)(nil)

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, append([]Option{WithRetry(2, time.Millisecond)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestExtract(t *testing.T) {
	var got map[string]string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/extract" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("%s %s %s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"learned":["a"],"not_learned":[]}`)
	})
	learned, notLearned, err := c.Extract(context.Background(), "+ a")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(learned, []string{"a"}) || notLearned == nil || len(notLearned) != 0 || got["text"] != "+ a" {
		t.Errorf("learned=%q notLearned=%q sent=%v", learned, notLearned, got)
	}
}

func TestExtract_RetriesServerErrors(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"learned":[],"not_learned":["b"]}`)
	})
	_, notLearned, err := c.Extract(context.Background(), "x")
	if err != nil || !slices.Equal(notLearned, []string{"b"}) || calls.Load() != 3 {
		t.Errorf("2回の 503 の後に成功するはず: %v, %q, calls=%d", err, notLearned, calls.Load())
	}
}

func TestExtract_GivesUpAndDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, _, err := c.Extract(context.Background(), "x"); err == nil || calls.Load() != 3 {
		t.Errorf("500 は再試行して諦める: %v, calls=%d", err, calls.Load())
	}

	calls.Store(0)
	c = newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error":{"code":"validation_failed"}}`)
	})
	if _, _, err := c.Extract(context.Background(), "x"); err == nil || calls.Load() != 1 {
		t.Errorf("4xx は再試行しない: %v, calls=%d", err, calls.Load())
	}
}

func TestExtract_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	c, err := New(u, WithRetry(1, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Extract(context.Background(), "x"); err == nil {
		t.Error("停止中の insight でエラーになるはず")
	}
}

func TestExtract_Timeout(t *testing.T) {
	block := make(chan struct{})
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-block
	}, WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}), WithRetry(0, 0))
	// httptest.Server.Close は処理中のリクエストの終了を待つので、Close より先にハンドラを解放する。
	// （Cleanup は登録の逆順に実行されるため、サーバーを作った後に登録する）
	t.Cleanup(func() { close(block) })
	start := time.Now()
	if _, _, err := c.Extract(context.Background(), "x"); err == nil {
		t.Error("タイムアウトでエラーになるはず")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("タイムアウトせずに待ち続けた")
	}
}

func TestExtract_BadResponse(t *testing.T) {
	for _, body := range []string{`not json`, `{"learned":["a"]}`} {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
		if _, _, err := c.Extract(context.Background(), "x"); err == nil {
			t.Errorf("不正な応答 %q でエラーになるはず", body)
		}
	}
}

func TestNew_InvalidURL(t *testing.T) {
	for _, u := range []string{"", "127.0.0.1:8081", "ftp://x"} {
		if _, err := New(u); err == nil {
			t.Errorf("New(%q) はエラーになるはず", u)
		}
	}
}
