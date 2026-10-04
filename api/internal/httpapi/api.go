package httpapi

import (
	"log/slog"
	"net/http"

	apispec "github.com/kawafuchieirin/task-manager/api"
	"github.com/kawafuchieirin/task-manager/api/internal/task"
	"github.com/kawafuchieirin/task-manager/shared/jsonapi"
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
	mux.HandleFunc("POST /api/v1/tasks/{id}/timer/start", h.startTimer)
	mux.HandleFunc("POST /api/v1/tasks/{id}/timer/stop", h.stopTimer)
	mux.HandleFunc("GET /api/v1/tasks/{id}/time-entries", h.listEntries)
	mux.HandleFunc("POST /api/v1/tasks/{id}/time-entries", h.addEntry)
	mux.HandleFunc("GET /api/v1/time-entries/{id}", h.getEntry)
	mux.HandleFunc("PATCH /api/v1/time-entries/{id}", h.updateEntry)
	mux.HandleFunc("DELETE /api/v1/time-entries/{id}", h.deleteEntry)
	mux.HandleFunc("GET /api/v1/tasks/{id}/reflection", h.getReflection)
	mux.HandleFunc("PUT /api/v1/tasks/{id}/reflection", h.saveReflection)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}/reflection", h.deleteReflection)
	mux.HandleFunc("POST /api/v1/tasks/{id}/reflection/extract", h.extractReflection)
	mux.HandleFunc("GET /api/v1/reflections", h.listReflections)
	mux.HandleFunc("GET /api/v1/openapi.yaml", serveOpenAPI)

	// ServeMux 既定の 404 / 405 は平文なので、エラー形式を揃えるため JSON で返す。
	// メソッド付きのパターンが優先されるため、以下はメソッドが合わないときだけ呼ばれる。
	mux.Handle("/api/v1/tasks", jsonapi.MethodNotAllowed(http.MethodGet, http.MethodPost))
	mux.Handle("/api/v1/tasks/{id}", jsonapi.MethodNotAllowed(http.MethodGet, http.MethodPatch, http.MethodDelete))
	mux.Handle("/api/v1/stats/summary", jsonapi.MethodNotAllowed(http.MethodGet))
	mux.Handle("/api/v1/tasks/{id}/timer/start", jsonapi.MethodNotAllowed(http.MethodPost))
	mux.Handle("/api/v1/tasks/{id}/timer/stop", jsonapi.MethodNotAllowed(http.MethodPost))
	mux.Handle("/api/v1/tasks/{id}/time-entries", jsonapi.MethodNotAllowed(http.MethodGet, http.MethodPost))
	mux.Handle("/api/v1/time-entries/{id}", jsonapi.MethodNotAllowed(http.MethodGet, http.MethodPatch, http.MethodDelete))
	mux.Handle("/api/v1/tasks/{id}/reflection", jsonapi.MethodNotAllowed(http.MethodGet, http.MethodPut, http.MethodDelete))
	mux.Handle("/api/v1/tasks/{id}/reflection/extract", jsonapi.MethodNotAllowed(http.MethodPost))
	mux.Handle("/api/v1/reflections", jsonapi.MethodNotAllowed(http.MethodGet))
	mux.Handle("/api/v1/openapi.yaml", jsonapi.MethodNotAllowed(http.MethodGet))
	mux.Handle("/api/v1/", jsonapi.NotFound("API のパスが存在しません"))

	return cors(opts.CORSOrigins, requireAPIKey(opts.APIKey, mux))
}

// serveOpenAPI は仕様書（openapi.yaml）を返す。他のアプリがクライアントの生成や確認に使える。
func serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(apispec.OpenAPI) // ヘッダー送信後は失敗してもクライアントに伝える手段がない
}
