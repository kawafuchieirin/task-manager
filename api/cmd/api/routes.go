package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kawafuchieirin/task-manager/api/internal/config"
	"github.com/kawafuchieirin/task-manager/api/internal/httpapi"
	"github.com/kawafuchieirin/task-manager/api/internal/task"
	"github.com/kawafuchieirin/task-manager/shared/httpserver"
)

// newHandler は API サーバーの全ルートを組み立てる。
func newHandler(cfg config.Config, database *sql.DB, logger *slog.Logger) (http.Handler, error) {
	svc := task.NewService(database)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", httpserver.HealthHandler(logger, database.PingContext))
	mux.Handle("/api/v1/", httpapi.NewHandler(svc, logger, httpapi.Options{
		APIKey:      cfg.APIKey,
		CORSOrigins: cfg.CORSOrigins,
	}))

	// ブラウザで開いた他サイトから API を操作される CSRF を防ぐ。
	// ブラウザ以外（画面アプリ web のサーバーや curl）からのリクエストは対象外なのでそのまま通る。
	csrf := http.NewCrossOriginProtection()
	for _, origin := range cfg.CORSOrigins {
		if err := csrf.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("CORS オリジン %q の登録に失敗: %w", origin, err)
		}
	}

	return httpserver.RequireHost(httpserver.LocalHosts(cfg.Addr), httpserver.SecurityHeaders(csrf.Handler(mux))), nil
}
