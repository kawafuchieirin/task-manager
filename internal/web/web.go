// Package web はタスクボードの画面（html/template + htmx）を提供する。
package web

import (
	"bytes"
	"cmp"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kawafuchieirin/task-manager/internal/task"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// jst は画面表示用のタイムゾーン。日本は夏時間が無いので固定オフセットで十分。
var jst = time.FixedZone("JST", 9*60*60)

var statusLabels = map[task.Status]string{
	task.StatusTodo:  "未着手",
	task.StatusDoing: "進行中",
	task.StatusDone:  "完了",
}

// action はカードに表示するステータス変更ボタン。
type action struct {
	Label string
	To    task.Status
}

// actionsFor はステータスごとに表示する遷移ボタンを返す。完了からは進行中に戻せる。
func actionsFor(s task.Status) []action {
	switch s {
	case task.StatusTodo:
		return []action{{"着手する", task.StatusDoing}, {"完了にする", task.StatusDone}}
	case task.StatusDoing:
		return []action{{"未着手に戻す", task.StatusTodo}, {"完了にする", task.StatusDone}}
	case task.StatusDone:
		return []action{{"未完了に戻す", task.StatusDoing}}
	}
	return nil
}

func formatMinutes(m int) string {
	h, min := m/60, m%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d分", min)
	case min == 0:
		return fmt.Sprintf("%d時間", h)
	default:
		return fmt.Sprintf("%d時間%d分", h, min)
	}
}

func formatJST(t time.Time) string {
	return t.In(jst).Format("1/2 15:04")
}

var funcs = template.FuncMap{
	"statusLabel": func(s task.Status) string { return statusLabels[s] },
	"actions":     actionsFor,
	"minutes":     formatMinutes,
	"jst":         formatJST,
}

// formView はタスク入力フォームの表示内容。検証エラー時は入力値を保持して再表示する。
type formView struct {
	ID           int64
	Title        string
	Description  string
	EstimatedMin string
	Errors       *task.ValidationError
}

// Error はテンプレートから項目ごとのエラーメッセージを引くためのヘルパー。
func (f formView) Error(field string) string {
	if f.Errors == nil {
		return ""
	}
	return f.Errors.Message(field)
}

type columnView struct {
	Status task.Status
	Label  string
	Tasks  []task.Task
}

type boardView struct {
	Columns   []columnView
	Total     int
	Done      int
	Percent   int
	NewForm   formView
	EditingID int64
	Edit      formView
}

// Handler は画面のリクエストを処理する。
type Handler struct {
	svc    *task.Service
	logger *slog.Logger
	tmpl   *template.Template
	mux    *http.ServeMux
}

// NewHandler は画面のハンドラを返す。テンプレートの構文エラーは起動時に検出する。
func NewHandler(svc *task.Service, logger *slog.Logger) (*Handler, error) {
	tmpl, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("テンプレートの読み込みに失敗: %w", err)
	}
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("静的ファイルの読み込みに失敗: %w", err)
	}

	h := &Handler{svc: svc, logger: logger, tmpl: tmpl, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /{$}", h.index)
	h.mux.HandleFunc("GET /board", h.board)
	h.mux.HandleFunc("POST /tasks", h.create)
	h.mux.HandleFunc("POST /tasks/{id}/status", h.changeStatus)
	h.mux.HandleFunc("GET /tasks/{id}/edit", h.edit)
	h.mux.HandleFunc("PUT /tasks/{id}", h.update)
	h.mux.HandleFunc("DELETE /tasks/{id}", h.delete)
	h.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	view, err := h.loadBoard(r, boardView{})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "index", view)
}

func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	h.renderBoard(w, r, http.StatusOK, boardView{})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	form := formFromRequest(r)
	estimated, err := parseEstimated(form.EstimatedMin)
	if err == nil {
		_, err = h.svc.Create(r.Context(), task.CreateInput{
			Title:        form.Title,
			Description:  form.Description,
			EstimatedMin: estimated,
		})
	}
	if ve, ok := asValidation(err); ok {
		form.Errors = ve
		h.renderBoard(w, r, http.StatusUnprocessableEntity, boardView{NewForm: form})
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{})
}

func (h *Handler) changeStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	status := task.Status(r.FormValue("status"))
	if _, err := h.svc.Update(r.Context(), id, task.UpdateInput{Status: &status}); err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{})
}

func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	t, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	form := formView{ID: t.ID, Title: t.Title, Description: t.Description}
	if t.EstimatedMin != nil {
		form.EstimatedMin = strconv.Itoa(*t.EstimatedMin)
	}
	h.renderBoard(w, r, http.StatusOK, boardView{EditingID: id, Edit: form})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	form := formFromRequest(r)
	form.ID = id
	estimated, err := parseEstimated(form.EstimatedMin)
	if err == nil {
		_, err = h.svc.Update(r.Context(), id, task.UpdateInput{
			Title:        &form.Title,
			Description:  &form.Description,
			EstimatedMin: task.Nullable[int]{Set: true, Value: estimated},
		})
	}
	if ve, ok := asValidation(err); ok {
		form.Errors = ve
		h.renderBoard(w, r, http.StatusUnprocessableEntity, boardView{EditingID: id, Edit: form})
		return
	}
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{})
}

// loadBoard は最新のタスクと進捗を view に詰める。フォームの状態は呼び出し元が設定する。
func (h *Handler) loadBoard(r *http.Request, view boardView) (boardView, error) {
	tasks, err := h.svc.List(r.Context(), nil)
	if err != nil {
		return boardView{}, err
	}
	sum, err := h.svc.Summary(r.Context())
	if err != nil {
		return boardView{}, err
	}

	byStatus := make(map[task.Status][]task.Task, len(task.Statuses))
	for _, t := range tasks {
		byStatus[t.Status] = append(byStatus[t.Status], t)
	}
	// 完了列は直近に完了したものを上に出す（他の列は作成順）。
	slices.SortStableFunc(byStatus[task.StatusDone], func(a, b task.Task) int {
		return cmp.Compare(completedUnix(b), completedUnix(a))
	})

	view.Columns = make([]columnView, len(task.Statuses))
	for i, s := range task.Statuses {
		view.Columns[i] = columnView{Status: s, Label: statusLabels[s], Tasks: byStatus[s]}
	}
	view.Total, view.Done, view.Percent = sum.Total, sum.Done, sum.ProgressPercent()
	return view, nil
}

func completedUnix(t task.Task) int64 {
	if t.CompletedAt == nil {
		return 0
	}
	return t.CompletedAt.Unix()
}

func (h *Handler) renderBoard(w http.ResponseWriter, r *http.Request, status int, view boardView) {
	view, err := h.loadBoard(r, view)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, status, "board", view)
}

// render はテンプレートをバッファに描画してから書き出す。
// 途中で失敗したときに、壊れた HTML を返さず 500 にできるようにするため。
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w) // ヘッダー送信後は失敗してもクライアントに伝える手段がない
}

// mutationError は存在しないタスクへの操作を 404、それ以外を 500 として返す。
// 画面では htmx:responseError を受けてメッセージを表示する（static/app.js）。
func (h *Handler) mutationError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, task.ErrNotFound) {
		http.Error(w, "タスクが見つかりません。画面を再読み込みしてください。", http.StatusNotFound)
		return
	}
	if _, ok := asValidation(err); ok {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	h.serverError(w, r, err)
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "画面の処理に失敗", "method", r.Method, "path", r.URL.Path, "error", err)
	http.Error(w, "サーバー内部でエラーが発生しました", http.StatusInternalServerError)
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "タスクが見つかりません", http.StatusNotFound)
		return 0, false
	}
	return id, true
}

func formFromRequest(r *http.Request) formView {
	return formView{
		Title:        r.FormValue("title"),
		Description:  r.FormValue("description"),
		EstimatedMin: strings.TrimSpace(r.FormValue("estimated_min")),
	}
}

// parseEstimated はフォームの目標時間を解釈する。空欄は未設定（nil）。
func parseEstimated(s string) (*int, error) {
	if s == "" {
		return nil, nil
	}
	m, err := strconv.Atoi(s)
	if err != nil {
		return nil, &task.ValidationError{Errors: []task.FieldError{
			{Field: "estimated_min", Message: "目標時間は整数（分）で入力してください"},
		}}
	}
	return &m, nil
}

func asValidation(err error) (*task.ValidationError, bool) {
	var ve *task.ValidationError
	ok := errors.As(err, &ve)
	return ve, ok
}
