// taskboard はタスク管理ボードの画面と Task API を提供するサービス。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kawafuchieirin/task-manager/internal/config"
	"github.com/kawafuchieirin/task-manager/internal/db"
	"github.com/kawafuchieirin/task-manager/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil)).With("service", "taskboard")
	if err := run(logger); err != nil {
		logger.Error("taskboard を終了します", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadTaskboard(os.Getenv)
	if err != nil {
		return fmt.Errorf("設定の読み込み: %w", err)
	}

	database, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("DB のクローズに失敗", "error", err)
		}
	}()

	if err := db.Migrate(ctx, database); err != nil {
		return fmt.Errorf("マイグレーション: %w", err)
	}
	logger.Info("DB を準備しました", "path", cfg.DBPath)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", server.HealthHandler(logger, database.PingContext))

	return server.Run(ctx, server.New(cfg.Addr, mux), logger)
}
