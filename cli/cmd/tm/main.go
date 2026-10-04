// tm はターミナルからタスクを操作するコマンド。Task API（api サービス）を呼ぶ。
//
//	tm add "Go を学ぶ" -e 30   # 追加（目標 30 分）
//	tm ls                     # 未完了の一覧
//	tm start 12 / tm stop     # タイマー
//	tm done 12                # クリア
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/kawafuchieirin/task-manager/cli/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(app.Run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}
