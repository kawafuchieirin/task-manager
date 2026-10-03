package api

import (
	"log/slog"
	"net/http"

	"github.com/kawafuchieirin/task-manager/internal/task"
)

// Options は API ハンドラの設定。
type Options struct {
	APIKey      string
	CORSOrigins []string
}

// NewHandler は /api/v1 以下のハンドラを返す。"/api/v1/" にマウントして使う。
func NewHandler(svc *task.Service, logger *slog.Logger, opts Options) http.Handler {
	h := &taskHandler{svc: svc, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks", h.list)
	mux.HandleFunc("POST /api/v1/tasks", h.create)
	mux.HandleFunc("GET /api/v1/tasks/{id}", h.get)
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", h.delete)
	mux.HandleFunc("GET /api/v1/stats/summary", h.summary)

	return cors(opts.CORSOrigins, requireAPIKey(opts.APIKey, mux))
}
