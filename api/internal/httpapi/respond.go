// Package httpapi は画面アプリ（web）や他のアプリから利用する JSON の REST API（/api/v1）を提供する。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/kawafuchieirin/task-manager/api/internal/task"
)

// エラーコード。他のアプリが分岐に使うため、一度公開したら変更しない。
const (
	codeInvalidJSON          = "invalid_json"
	codeValidationFailed     = "validation_failed"
	codeNotFound             = "not_found"
	codeMethodNotAllowed     = "method_not_allowed"
	codeUnauthorized         = "unauthorized"
	codeUnsupportedMediaType = "unsupported_media_type"
	codePayloadTooLarge      = "payload_too_large"
	codeInternal             = "internal_error"
)

// maxBodyBytes はリクエストボディの上限。説明 2000 文字でも十分収まる。
const maxBodyBytes = 1 << 20

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details []task.FieldError `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // ヘッダー送信後は失敗してもクライアントに伝える手段がない
}

func writeError(w http.ResponseWriter, status int, code, message string, details []task.FieldError) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message, Details: details}})
}

// writeServiceError はサービス層のエラーを HTTP レスポンスに変換する。
// 想定外のエラーは詳細をログにのみ出し、クライアントには一般的なメッセージを返す。
func writeServiceError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var ve *task.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusUnprocessableEntity, codeValidationFailed, "入力値が不正です", ve.Errors)
	case errors.Is(err, task.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, err.Error(), nil)
	default:
		logger.ErrorContext(r.Context(), "API の処理に失敗", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "サーバー内部でエラーが発生しました", nil)
	}
}

// decodeJSON はボディを dst にデコードする。失敗した場合はエラーレスポンスを書き込み false を返す。
// 他のアプリ側の書き間違いに早く気づけるよう、未知のフィールドはエラーにする。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType,
			"Content-Type は application/json を指定してください", nil)
		return false
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeDecodeError(w, err)
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, codeInvalidJSON, "JSON オブジェクトは1つだけ送信してください", nil)
		return false
	}
	return true
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var (
		maxErr  *http.MaxBytesError
		typeErr *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &maxErr):
		writeError(w, http.StatusRequestEntityTooLarge, codePayloadTooLarge,
			fmt.Sprintf("リクエストボディは%dバイト以下にしてください", maxErr.Limit), nil)
	case errors.As(err, &typeErr):
		writeError(w, http.StatusBadRequest, codeInvalidJSON,
			fmt.Sprintf("フィールド %q の型が不正です", typeErr.Field), nil)
	case errors.Is(err, io.EOF):
		writeError(w, http.StatusBadRequest, codeInvalidJSON, "リクエストボディが空です", nil)
	default:
		// 未知のフィールドや構文エラー。encoding/json のメッセージに原因が含まれる。
		writeError(w, http.StatusBadRequest, codeInvalidJSON, "JSON が不正です: "+err.Error(), nil)
	}
}
