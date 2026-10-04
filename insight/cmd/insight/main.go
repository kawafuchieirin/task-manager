// insight は振り返りテキストから「学んだこと / できなかったこと」を抽出するステートレスな API サービス。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kawafuchieirin/task-manager/insight/internal/config"
	"github.com/kawafuchieirin/task-manager/insight/internal/extract"
	"github.com/kawafuchieirin/task-manager/insight/internal/handler"
	"github.com/kawafuchieirin/task-manager/shared/httpserver"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil)).With("service", "insight")
	if err := run(logger); err != nil {
		logger.Error("insight を終了します", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("設定の読み込み: %w", err)
	}

	return httpserver.Run(ctx, httpserver.New(cfg.Addr, newHandler(cfg, logger)), logger)
}

// newHandler は insight の全ルートを組み立てる。
func newHandler(cfg config.Config, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	// 依存先を持たないため、プロセスが応答できれば正常とみなす。
	mux.Handle("GET /healthz", httpserver.HealthHandler(logger, nil))
	mux.Handle("/api/v1/", handler.New(extract.RuleBased{}, logger))
	return httpserver.RequireHost(httpserver.LocalHosts(cfg.Addr), httpserver.SecurityHeaders(mux))
}
