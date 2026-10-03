package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kawafuchieirin/task-manager/internal/api"
	"github.com/kawafuchieirin/task-manager/internal/config"
	"github.com/kawafuchieirin/task-manager/internal/server"
	"github.com/kawafuchieirin/task-manager/internal/task"
	"github.com/kawafuchieirin/task-manager/internal/web"
)

// newHandler は taskboard の全ルートを組み立てる。
func newHandler(cfg config.Taskboard, database *sql.DB, logger *slog.Logger) (http.Handler, error) {
	svc := task.NewService(database)

	webHandler, err := web.NewHandler(svc, logger)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", server.HealthHandler(logger, database.PingContext))
	mux.Handle("/api/v1/", api.NewHandler(svc, logger, api.Options{
		APIKey:      cfg.APIKey,
		CORSOrigins: cfg.CORSOrigins,
	}))
	mux.Handle("/", webHandler)

	// 他サイトからのフォーム送信などによる CSRF を防ぐ。
	// ブラウザ以外（curl や他のアプリのサーバー）からのリクエストは対象外なのでそのまま通る。
	// 信頼オリジン（CORS で許可したアプリ）は画面のルートにも送信できる。信頼済みアプリという前提。
	csrf := http.NewCrossOriginProtection()
	for _, origin := range cfg.CORSOrigins {
		if err := csrf.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("CORS オリジン %q の登録に失敗: %w", origin, err)
		}
	}

	return server.RequireHost(server.LocalHosts(cfg.Addr), server.SecurityHeaders(csrf.Handler(mux))), nil
}
