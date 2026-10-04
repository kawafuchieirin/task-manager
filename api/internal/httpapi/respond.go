// Package httpapi は画面アプリ（web）や他のアプリから利用する JSON の REST API（/api/v1）を提供する。
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/kawafuchieirin/task-manager/api/internal/task"
	"github.com/kawafuchieirin/task-manager/shared/jsonapi"
)

// エラーコード。他のアプリが分岐に使うため、一度公開したら変更しない。
// 全サービス共通のものは shared/jsonapi に置き、ここでは API 固有のものを定義する。
const (
	codeInvalidJSON          = jsonapi.CodeInvalidJSON
	codeValidationFailed     = jsonapi.CodeValidationFailed
	codeNotFound             = jsonapi.CodeNotFound
	codeMethodNotAllowed     = jsonapi.CodeMethodNotAllowed
	codeUnsupportedMediaType = jsonapi.CodeUnsupportedMediaType
	codePayloadTooLarge      = jsonapi.CodePayloadTooLarge
	codeInternal             = jsonapi.CodeInternal
	codeUnauthorized         = "unauthorized"
	codeTimerAlreadyRunning  = "timer_already_running"
	codeTimerNotRunning      = "timer_not_running"
	codeTaskCompleted        = "task_completed"
	codeEntryRunning         = "entry_running"
)

// maxBodyBytes はリクエストボディの上限。説明 2000 文字でも十分収まる。
const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	jsonapi.WriteJSON(w, status, v)
}

func writeError(w http.ResponseWriter, status int, code, message string, details []task.FieldError) {
	jsonapi.WriteError(w, status, code, message, details)
}

// decodeJSON はボディを dst にデコードする。失敗した場合はエラーレスポンスを書き込み false を返す。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return jsonapi.DecodeJSON(w, r, dst, maxBodyBytes)
}

// writeServiceError はサービス層のエラーを HTTP レスポンスに変換する。
// 想定外のエラーは詳細をログにのみ出し、クライアントには一般的なメッセージを返す。
func writeServiceError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var (
		ve      *task.ValidationError
		running *task.TimerRunningError
	)
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusUnprocessableEntity, codeValidationFailed, "入力値が不正です", ve.Errors)
	case errors.Is(err, task.ErrNotFound), errors.Is(err, task.ErrEntryNotFound), errors.Is(err, task.ErrReflectionNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, err.Error(), nil)
	// 状態の衝突（409）。他のアプリが分岐できるよう、原因ごとにコードを分ける。
	case errors.As(err, &running):
		writeError(w, http.StatusConflict, codeTimerAlreadyRunning, err.Error(), nil)
	case errors.Is(err, task.ErrTimerNotRunning):
		writeError(w, http.StatusConflict, codeTimerNotRunning, err.Error(), nil)
	case errors.Is(err, task.ErrTaskCompleted):
		writeError(w, http.StatusConflict, codeTaskCompleted, err.Error(), nil)
	case errors.Is(err, task.ErrEntryRunning):
		writeError(w, http.StatusConflict, codeEntryRunning, err.Error(), nil)
	default:
		logger.ErrorContext(r.Context(), "API の処理に失敗", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "サーバー内部でエラーが発生しました", nil)
	}
}
