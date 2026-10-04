package main

import (
	"log/slog"
	"net/http"

	"github.com/kawafuchieirin/task-manager/client/taskclient"
	"github.com/kawafuchieirin/task-manager/shared/httpserver"
	"github.com/kawafuchieirin/task-manager/web/internal/board"
	"github.com/kawafuchieirin/task-manager/web/internal/config"
)

// newHandler は画面アプリの全ルートを組み立てる。
func newHandler(cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	client, err := taskclient.New(cfg.APIURL, cfg.APIKey)
	if err != nil {
		return nil, err
	}
	boardHandler, err := board.NewHandler(client, logger)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	// web 自身の死活監視。API の状態は make status / API の /healthz で確認する。
	mux.Handle("GET /healthz", httpserver.HealthHandler(logger, nil))
	mux.Handle("/", boardHandler)

	// 他サイトからのフォーム送信による CSRF を防ぐ。画面は同一オリジンの htmx からしか操作しない。
	csrf := http.NewCrossOriginProtection()
	return httpserver.RequireHost(httpserver.LocalHosts(cfg.Addr), httpserver.SecurityHeaders(csrf.Handler(mux))), nil
}
