// api はタスクの REST API（/api/v1）を提供するサービス。DB を持つのはこのサービスだけ。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kawafuchieirin/task-manager/api/internal/config"
	"github.com/kawafuchieirin/task-manager/api/internal/db"
	"github.com/kawafuchieirin/task-manager/shared/httpserver"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil)).With("service", "api")
	if err := run(logger); err != nil {
		logger.Error("api を終了します", "error", err)
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

	handler, err := newHandler(cfg, database, logger)
	if err != nil {
		return err
	}
	return httpserver.Run(ctx, httpserver.New(cfg.Addr, handler), logger)
}
