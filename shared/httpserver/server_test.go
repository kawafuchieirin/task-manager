package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name       string
		check      CheckFunc
		wantStatus int
		wantBody   string
	}{
		{"チェックなしは正常", nil, http.StatusOK, "ok"},
		{"チェック成功は正常", func(context.Context) error { return nil }, http.StatusOK, "ok"},
		{"チェック失敗は 503", func(context.Context) error { return errors.New("db down") },
			http.StatusServiceUnavailable, "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			HealthHandler(discardLogger, tt.check)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			var got map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("JSON のデコード: %v", err)
			}
			if got["status"] != tt.wantBody {
				t.Errorf("status = %q, want %q", got["status"], tt.wantBody)
			}
			if _, leaked := got["error"]; leaked {
				t.Error("内部エラーがレスポンスに漏れている")
			}
		})
	}
}

func TestServe_ShutsDownOnContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", HealthHandler(discardLogger, nil))
	srv := New(ln.Addr().String(), mux)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, discardLogger) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("起動中のサーバーへのリクエスト: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("シャットダウン時のエラー: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("シャットダウンが終わらない")
	}
}

func TestRun_ReturnsErrorWhenPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	err = Run(context.Background(), New(ln.Addr().String(), http.NewServeMux()), discardLogger)
	if err == nil {
		t.Error("使用中ポートでエラーを期待したが nil")
	}
}
