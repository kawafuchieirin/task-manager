// Package jsonapi は各サービスの JSON API で共通の、応答の書き出し・エラー形式・リクエストの読み取りを提供する。
//
// エラーは全サービスで {"error": {"code": "...", "message": "...", "details": [...]}} の形に揃える。
package jsonapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// 全サービス共通のエラーコード。他のアプリが分岐に使うため、一度公開したら変更しない。
const (
	CodeInvalidJSON          = "invalid_json"
	CodeValidationFailed     = "validation_failed"
	CodeNotFound             = "not_found"
	CodeMethodNotAllowed     = "method_not_allowed"
	CodeUnsupportedMediaType = "unsupported_media_type"
	CodePayloadTooLarge      = "payload_too_large"
	CodeInternal             = "internal_error"
)

// ErrorBody はエラー応答の JSON。
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail はエラーの内容。Details は入力エラーの項目ごとの理由など（無ければ省略）。
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// WriteJSON は v を JSON で書き出す。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // ヘッダー送信後は失敗してもクライアントに伝える手段がない
}

// WriteError はエラー応答を書き出す。details が空なら JSON から省く。
func WriteError[D any](w http.ResponseWriter, status int, code, message string, details []D) {
	detail := ErrorDetail{Code: code, Message: message}
	if len(details) > 0 {
		detail.Details = details
	}
	WriteJSON(w, status, ErrorBody{Error: detail})
}

// WriteSimpleError は details の無いエラー応答を書き出す。
func WriteSimpleError(w http.ResponseWriter, status int, code, message string) {
	WriteError[struct{}](w, status, code, message, nil)
}

// DecodeJSON はリクエストボディを dst にデコードする。失敗した場合はエラー応答を書き込み false を返す。
// 他のアプリ側の書き間違いに早く気づけるよう、未知のフィールドはエラーにする。
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		WriteSimpleError(w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType,
			"Content-Type は application/json を指定してください")
		return false
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeDecodeError(w, err)
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		WriteSimpleError(w, http.StatusBadRequest, CodeInvalidJSON, "JSON オブジェクトは1つだけ送信してください")
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
		WriteSimpleError(w, http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
			fmt.Sprintf("リクエストボディは%dバイト以下にしてください", maxErr.Limit))
	case errors.As(err, &typeErr):
		WriteSimpleError(w, http.StatusBadRequest, CodeInvalidJSON, fmt.Sprintf("フィールド %q の型が不正です", typeErr.Field))
	case errors.Is(err, io.EOF):
		WriteSimpleError(w, http.StatusBadRequest, CodeInvalidJSON, "リクエストボディが空です")
	default:
		// 未知のフィールドや構文エラー。encoding/json のメッセージに原因が含まれる。
		WriteSimpleError(w, http.StatusBadRequest, CodeInvalidJSON, "JSON が不正です: "+err.Error())
	}
}

// MethodNotAllowed は 405 を JSON で返すハンドラ。ServeMux 既定の 405 は平文なので、形式を揃えるために使う。
// メソッド無しのパターンに登録すると、メソッド付きのパターンに合わないときだけ呼ばれる。
func MethodNotAllowed(allowed ...string) http.Handler {
	allow := strings.Join(allowed, ", ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		WriteSimpleError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed,
			r.Method+" は使用できません（使用できるメソッド: "+allow+"）")
	})
}

// NotFound は 404 を JSON で返すハンドラ。存在しないパスの受け皿に使う。
func NotFound(message string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteSimpleError(w, http.StatusNotFound, CodeNotFound, message)
	})
}
