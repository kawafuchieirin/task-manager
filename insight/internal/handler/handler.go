// Package handler は insight の JSON API（/api/v1/extract）を提供する。
package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/kawafuchieirin/task-manager/insight/internal/extract"
	"github.com/kawafuchieirin/task-manager/shared/jsonapi"
)

// MaxTextLen は抽出するテキストの上限（文字数）。振り返りメモの上限に合わせる。
const MaxTextLen = 5000

// maxBodyBytes はリクエストボディの上限。5000 文字の日本語でも十分収まる。
const maxBodyBytes = 64 << 10

type extractRequest struct {
	Text string `json:"text"`
}

type extractResponse struct {
	Learned    []string `json:"learned"`
	NotLearned []string `json:"not_learned"`
}

// fieldError は入力エラーの項目ごとの理由。形式は api サービスと揃える。
type fieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// New は /api/v1 以下のハンドラを返す。"/api/v1/" にマウントして使う。
func New(ex extract.Extractor, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/extract", func(w http.ResponseWriter, r *http.Request) {
		var req extractRequest
		if !jsonapi.DecodeJSON(w, r, &req, maxBodyBytes) {
			return
		}
		switch n := utf8.RuneCountInString(req.Text); {
		case strings.TrimSpace(req.Text) == "":
			jsonapi.WriteError(w, http.StatusUnprocessableEntity, jsonapi.CodeValidationFailed, "入力値が不正です",
				[]fieldError{{Field: "text", Code: "required", Message: "テキストを指定してください"}})
			return
		case n > MaxTextLen:
			jsonapi.WriteError(w, http.StatusUnprocessableEntity, jsonapi.CodeValidationFailed, "入力値が不正です",
				[]fieldError{{Field: "text", Code: "too_long", Message: "テキストは5000文字以内にしてください"}})
			return
		}

		res, err := ex.Extract(r.Context(), req.Text)
		if err != nil {
			logger.ErrorContext(r.Context(), "抽出に失敗", "error", err)
			jsonapi.WriteSimpleError(w, http.StatusInternalServerError, jsonapi.CodeInternal, "抽出に失敗しました")
			return
		}
		// 0件でも null ではなく空配列で返す（受け取る側が nil の扱いを考えなくて済むように）。
		jsonapi.WriteJSON(w, http.StatusOK, extractResponse{
			Learned:    nonNil(res.Learned),
			NotLearned: nonNil(res.NotLearned),
		})
	})
	mux.Handle("/api/v1/extract", jsonapi.MethodNotAllowed(http.MethodPost))
	mux.Handle("/api/v1/", jsonapi.NotFound("API のパスが存在しません"))
	return mux
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
