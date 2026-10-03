package api

import (
	"log/slog"
	"net/http"
	"strings"

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

	// ServeMux 既定の 404 / 405 は平文なので、エラー形式を揃えるため JSON で返す。
	// メソッド付きのパターンが優先されるため、以下はメソッドが合わないときだけ呼ばれる。
	mux.Handle("/api/v1/tasks", methodNotAllowed(http.MethodGet, http.MethodPost))
	mux.Handle("/api/v1/tasks/{id}", methodNotAllowed(http.MethodGet, http.MethodPatch, http.MethodDelete))
	mux.Handle("/api/v1/stats/summary", methodNotAllowed(http.MethodGet))
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, codeNotFound, "API のパスが存在しません", nil)
	})

	return cors(opts.CORSOrigins, requireAPIKey(opts.APIKey, mux))
}

func methodNotAllowed(allowed ...string) http.Handler {
	allow := strings.Join(allowed, ", ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed,
			r.Method+" は使用できません（使用できるメソッド: "+allow+"）", nil)
	})
}
