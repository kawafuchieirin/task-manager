package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/api/internal/config"
	"github.com/kawafuchieirin/task-manager/api/internal/db/dbtest"
)

const testKey = "0123456789abcdef"

func newTestServer(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
	}
	if cfg.InsightURL == "" {
		cfg.InsightURL = "http://127.0.0.1:1" // 接続できない先（抽出は失敗扱いになる）
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
	h := newTestServer(t, config.Config{})
	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/api/v1/tasks", http.StatusOK},
		{http.MethodGet, "/api/v1/stats/summary", http.StatusOK},
		// 画面は web サービスに分離したので、API サーバーは HTML を返さない
		{http.MethodGet, "/", http.StatusNotFound},
		{http.MethodGet, "/static/htmx.min.js", http.StatusNotFound},
	}
	for _, tt := range tests {
		if rec := serve(h, tt.method, tt.path, ""); rec.Code != tt.want {
			t.Errorf("%s %s: status = %d, want %d", tt.method, tt.path, rec.Code, tt.want)
		}
	}
}

func TestRoutes_RejectsForeignHost(t *testing.T) {
	h := newTestServer(t, config.Config{})
	rec := serve(h, http.MethodGet, "/api/v1/tasks", "", func(r *http.Request) { r.Host = "evil.example.com" })
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("status = %d, want 421（DNS リバインディング対策）", rec.Code)
	}
}

func TestRoutes_SecurityHeaders(t *testing.T) {
	h := newTestServer(t, config.Config{})
	for _, path := range []string{"/api/v1/tasks", "/nope"} {
		if got := serve(h, http.MethodGet, path, "").Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
			t.Errorf("%s: CSP = %q（クリックジャッキング対策）", path, got)
		}
	}
}

func TestRoutes_CSRF(t *testing.T) {
	h := newTestServer(t, config.Config{CORSOrigins: []string{"http://localhost:5173"}})
	json := header("Content-Type", "application/json")

	tests := []struct {
		name string
		opts []reqOpt
		want int
	}{
		{"他サイトのブラウザからの送信は拒否",
			[]reqOpt{json, header("Sec-Fetch-Site", "cross-site"), header("Origin", "https://evil.example.com")}, http.StatusForbidden},
		{"ブラウザ以外（web サーバー・curl）は許可", []reqOpt{json}, http.StatusCreated},
		{"CORS で許可したオリジンは許可",
			[]reqOpt{json, header("Sec-Fetch-Site", "same-site"), header("Origin", "http://localhost:5173")}, http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := serve(h, http.MethodPost, "/api/v1/tasks", `{"title":"t"}`, tt.opts...); rec.Code != tt.want {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestRoutes_TrustedOriginCanUseAPIWithKey(t *testing.T) {
	const origin = "http://localhost:5173"
	h := newTestServer(t, config.Config{APIKey: testKey, CORSOrigins: []string{origin}})
	browser := []reqOpt{
		header("Origin", origin), header("Sec-Fetch-Site", "same-site"),
		header("Content-Type", "application/json"), header("Authorization", "Bearer "+testKey),
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
		header("Content-Type", "application/json"), header("Authorization", "Bearer "+testKey)}
	if rec := serve(h, http.MethodPost, "/api/v1/tasks", `{"title":"x"}`, other...); rec.Code != http.StatusForbidden {
		t.Errorf("未許可オリジン: status = %d, want 403", rec.Code)
	}
}

func TestRoutes_APIKeyProtectsAllAPIRoutes(t *testing.T) {
	h := newTestServer(t, config.Config{APIKey: testKey})
	for _, path := range []string{"/api/v1/tasks", "/api/v1/stats/summary"} {
		if rec := serve(h, http.MethodGet, path, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, rec.Code)
		}
	}
	if rec := serve(h, http.MethodGet, "/api/v1/tasks", "", header("Authorization", "Bearer "+testKey)); rec.Code != http.StatusOK {
		t.Errorf("正しいキー: status = %d, want 200", rec.Code)
	}
	if rec := serve(h, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("/healthz: status = %d, want 200（死活監視はキー不要）", rec.Code)
	}
}
