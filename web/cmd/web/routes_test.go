package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/web/internal/config"
)

// newTestServer は、tasks を返す最小の API サーバーに接続した画面アプリを返す。
func newTestServer(t *testing.T, apiKey string, api http.HandlerFunc) http.Handler {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	h, err := newHandler(config.Config{Addr: "127.0.0.1:3000", APIURL: srv.URL, APIKey: apiKey},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	return h
}

func emptyAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
}

type reqOpt func(*http.Request)

func header(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }

func serve(h http.Handler, method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:3000"
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	h := newTestServer(t, "", emptyAPI)
	tests := []struct {
		path string
		want int
	}{
		{"/healthz", http.StatusOK},
		{"/", http.StatusOK},
		{"/board", http.StatusOK},
		{"/static/htmx.min.js", http.StatusOK},
		// API は api サービスに分離したので、画面アプリは /api/v1 を提供しない
		{"/api/v1/tasks", http.StatusNotFound},
	}
	for _, tt := range tests {
		if rec := serve(h, http.MethodGet, tt.path, ""); rec.Code != tt.want {
			t.Errorf("GET %s: status = %d, want %d", tt.path, rec.Code, tt.want)
		}
	}
}

func TestRoutes_SendsAPIKeyToAPI(t *testing.T) {
	var got string
	h := newTestServer(t, "secret-key-0123456", func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		emptyAPI(w, r)
	})
	serve(h, http.MethodGet, "/", "")
	if got != "Bearer secret-key-0123456" {
		t.Errorf("Authorization = %q, WEB_API_KEY を API に送るはず", got)
	}
}

func TestRoutes_APIKeyMismatch(t *testing.T) {
	h := newTestServer(t, "wrong-key-0123456", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"API キーが無効です"}}`)
	})
	rec := serve(h, http.MethodGet, "/", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "WEB_API_KEY") {
		t.Errorf("status = %d, body に設定の確認方法を表示するはず", rec.Code)
	}
}

func TestRoutes_RejectsForeignHost(t *testing.T) {
	h := newTestServer(t, "", emptyAPI)
	rec := serve(h, http.MethodGet, "/", "", func(r *http.Request) { r.Host = "evil.example.com" })
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("status = %d, want 421（DNS リバインディング対策）", rec.Code)
	}
}

func TestRoutes_SecurityHeaders(t *testing.T) {
	h := newTestServer(t, "", emptyAPI)
	if got := serve(h, http.MethodGet, "/", "").Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
		t.Errorf("CSP = %q（クリックジャッキング対策）", got)
	}
}

func TestRoutes_CSRF(t *testing.T) {
	h := newTestServer(t, "", emptyAPI)
	form := header("Content-Type", "application/x-www-form-urlencoded")

	rec := serve(h, http.MethodPost, "/tasks", "title=t", form,
		header("Sec-Fetch-Site", "cross-site"), header("Origin", "https://evil.example.com"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("他サイトからのフォーム送信: status = %d, want 403", rec.Code)
	}
}
