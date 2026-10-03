// Package httpserver は各サービス共通の HTTP サーバー起動処理・ヘルスチェック・防御ミドルウェアを提供する。
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

// New はタイムアウトを設定した http.Server を返す。
// タイムアウト未設定だと遅いクライアントに接続を握られ続けるため、必ず設定する。
func New(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// Run は srv を起動し、ctx がキャンセルされたらグレースフルにシャットダウンする。
// リッスンに失敗した場合はそのエラーを返す。
func Run(ctx context.Context, srv *http.Server, logger *slog.Logger) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("%s で待ち受けできません: %w", srv.Addr, err)
	}
	return Serve(ctx, srv, ln, logger)
}

// Serve は既存の Listener で srv を動かす。テストで空きポートを使うために Run から分けている。
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	// scripts/service.sh がこのメッセージと addr から接続先を読み取るため、変更するときは合わせて直す。
	logger.Info("サーバーを起動しました", "addr", ln.Addr().String())

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("シャットダウンします")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("シャットダウンに失敗: %w", err)
		}
		return nil
	}
}

// CheckFunc は依存先（DB など）の疎通確認。nil なら正常。
type CheckFunc func(ctx context.Context) error

// HealthHandler は /healthz 用のハンドラを返す。
// check が失敗した場合は 503 を返す。詳細はログにのみ出し、レスポンスには含めない。
func HealthHandler(logger *slog.Logger, check CheckFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, body := http.StatusOK, "ok"
		if check != nil {
			if err := check(r.Context()); err != nil {
				logger.ErrorContext(r.Context(), "ヘルスチェックに失敗", "error", err)
				status, body = http.StatusServiceUnavailable, "unavailable"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": body})
	}
}
