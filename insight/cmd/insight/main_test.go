package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/insight/internal/config"
)

func TestRoutes(t *testing.T) {
	h := newHandler(config.Config{Addr: "127.0.0.1:8081"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	serve := func(method, path, body, host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(http.MethodGet, "/healthz", "", "127.0.0.1:8081"); rec.Code != http.StatusOK {
		t.Errorf("/healthz: %d", rec.Code)
	}
	if rec := serve(http.MethodPost, "/api/v1/extract", `{"text":"+ a"}`, "127.0.0.1:8081"); rec.Code != http.StatusOK {
		t.Errorf("抽出: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(http.MethodPost, "/api/v1/extract", `{"text":"+ a"}`, "evil.example.com"); rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("外部の Host: %d, want 421", rec.Code)
	}
}
