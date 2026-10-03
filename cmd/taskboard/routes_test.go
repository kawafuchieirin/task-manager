package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/internal/config"
	"github.com/kawafuchieirin/task-manager/internal/db/dbtest"
)

func newTestServer(t *testing.T, cfg config.Taskboard) http.Handler {
	t.Helper()
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
	}
	h, err := newHandler(cfg, dbtest.New(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	return h
}

type reqOpt func(*http.Request)

func header(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }

func serve(h http.Handler, method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8080"
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	h := newTestServer(t, config.Taskboard{})
	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodGet, "/board", http.StatusOK},
		{http.MethodGet, "/static/htmx.min.js", http.StatusOK},
		{http.MethodGet, "/api/v1/tasks", http.StatusOK},
		{http.MethodGet, "/api/v1/stats/summary", http.StatusOK},
	}
	for _, tt := range tests {
		if rec := serve(h, tt.method, tt.path, ""); rec.Code != tt.want {
			t.Errorf("%s %s: status = %d, want %d", tt.method, tt.path, rec.Code, tt.want)
		}
	}
}

func TestRoutes_APIAndUIShareData(t *testing.T) {
	h := newTestServer(t, config.Taskboard{})
	rec := serve(h, http.MethodPost, "/api/v1/tasks", `{"title":"API から追加"}`,
		header("Content-Type", "application/json"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("API での作成: %d %s", rec.Code, rec.Body.String())
	}
	if body := serve(h, http.MethodGet, "/", "").Body.String(); !strings.Contains(body, "API から追加") {
		t.Error("API で作ったタスクが画面に表示されない")
	}
}

func TestRoutes_RejectsForeignHost(t *testing.T) {
	h := newTestServer(t, config.Taskboard{})
	rec := serve(h, http.MethodGet, "/api/v1/tasks", "", func(r *http.Request) { r.Host = "evil.example.com" })
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("status = %d, want 421（DNS リバインディング対策）", rec.Code)
	}
}

func TestRoutes_CSRF(t *testing.T) {
	h := newTestServer(t, config.Taskboard{CORSOrigins: []string{"http://localhost:3000"}})
	form := header("Content-Type", "application/x-www-form-urlencoded")

	tests := []struct {
		name string
		opts []reqOpt
		want int
	}{
		{"他サイトからのフォーム送信は拒否", []reqOpt{form, header("Sec-Fetch-Site", "cross-site"), header("Origin", "https://evil.example.com")},
			http.StatusForbidden},
		{"同一オリジン（htmx）は許可", []reqOpt{form, header("Sec-Fetch-Site", "same-origin")}, http.StatusOK},
		{"ブラウザ以外（ヘッダーなし）は許可", []reqOpt{form}, http.StatusOK},
		{"CORS で許可したオリジンは許可", []reqOpt{form, header("Sec-Fetch-Site", "cross-site"), header("Origin", "http://localhost:3000")},
			http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := serve(h, http.MethodPost, "/tasks", "title=t", tt.opts...); rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestRoutes_SecurityHeaders(t *testing.T) {
	h := newTestServer(t, config.Taskboard{})
	for _, path := range []string{"/", "/api/v1/tasks", "/nope"} {
		if got := serve(h, http.MethodGet, path, "").Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
			t.Errorf("%s: CSP = %q（クリックジャッキング対策）", path, got)
		}
	}
}

func TestRoutes_TrustedOriginCanUseAPIWithKey(t *testing.T) {
	const origin = "http://localhost:3000"
	h := newTestServer(t, config.Taskboard{APIKey: "0123456789abcdef", CORSOrigins: []string{origin}})
	browser := []reqOpt{
		header("Origin", origin), header("Sec-Fetch-Site", "same-site"),
		header("Content-Type", "application/json"), header("Authorization", "Bearer 0123456789abcdef"),
	}

	rec := serve(h, http.MethodPost, "/api/v1/tasks", `{"title":"他アプリから"}`, browser...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Allow-Origin = %q", got)
	}
	if rec := serve(h, http.MethodPatch, "/api/v1/tasks/1", `{"status":"done"}`, browser...); rec.Code != http.StatusOK {
		t.Errorf("PATCH: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// 許可していないローカルの別アプリ（same-site）からは CSRF 対策で拒否される。
	other := []reqOpt{header("Origin", "http://localhost:4000"), header("Sec-Fetch-Site", "same-site"),
		header("Content-Type", "application/json"), header("Authorization", "Bearer 0123456789abcdef")}
	if rec := serve(h, http.MethodPost, "/api/v1/tasks", `{"title":"x"}`, other...); rec.Code != http.StatusForbidden {
		t.Errorf("未許可オリジン: status = %d, want 403", rec.Code)
	}
}

func TestRoutes_APIKeyOnlyProtectsAPI(t *testing.T) {
	h := newTestServer(t, config.Taskboard{APIKey: "0123456789abcdef"})
	if rec := serve(h, http.MethodGet, "/api/v1/tasks", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("API: status = %d, want 401", rec.Code)
	}
	if rec := serve(h, http.MethodGet, "/", ""); rec.Code != http.StatusOK {
		t.Errorf("画面: status = %d, want 200（画面はローカル専用のため API キー不要）", rec.Code)
	}
	if rec := serve(h, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("/healthz: status = %d, want 200", rec.Code)
	}
}
