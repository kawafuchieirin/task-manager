// web はタスクボードの画面を提供するサービス。DB は持たず、API サーバーを HTTP で呼び出す。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kawafuchieirin/task-manager/shared/httpserver"
	"github.com/kawafuchieirin/task-manager/web/internal/config"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil)).With("service", "web")
	if err := run(logger); err != nil {
		logger.Error("web を終了します", "error", err)
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

	handler, err := newHandler(cfg, logger)
	if err != nil {
		return err
	}
	logger.Info("API サーバーを利用します", "api_url", cfg.APIURL)
	return httpserver.Run(ctx, httpserver.New(cfg.Addr, handler), logger)
}
