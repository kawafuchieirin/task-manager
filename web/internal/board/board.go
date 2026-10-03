// Package board はタスクボードの画面（html/template + htmx）を提供する。
// データは持たず、タスクの読み書きはすべて API サーバーを taskclient 経由で呼び出して行う。
package board

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

	"github.com/kawafuchieirin/task-manager/web/internal/taskclient"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// jst は画面表示用のタイムゾーン。日本は夏時間が無いので固定オフセットで十分。
var jst = time.FixedZone("JST", 9*60*60)

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

// progressPercent は完了率（%）を返す。切り捨てなので 100 は全件完了のときだけ。0件なら 0。
func progressPercent(done, total int) int {
	if total == 0 {
		return 0
	}
	return done * 100 / total
}

var funcs = template.FuncMap{
	"statusLabel": func(s taskclient.Status) string { return statusLabels[s] },
	"actions":     actionsFor,
	"minutes":     formatMinutes,
	"duration":    formatDuration,
	"clock":       formatClock,
	"jst":         formatJST,
}

// formView はタスク入力フォームの表示内容。検証エラー時は入力値を保持して再表示する。
type formView struct {
	ID           int64
	Title        string
	Description  string
	EstimatedMin string
	Errors       *taskclient.ValidationError
}

// Error はテンプレートから項目ごとのエラーメッセージを引くためのヘルパー。
func (f formView) Error(field string) string {
	if f.Errors == nil {
		return ""
	}
	for _, fe := range f.Errors.Errors {
		if fe.Field == field {
			return fieldMessage(fe)
		}
	}
	return ""
}

type columnView struct {
	Status taskclient.Status
	Label  string
	Tasks  []taskclient.Task
}

type boardView struct {
	Columns   []columnView
	Total     int
	Done      int
	Percent   int
	NewForm   formView
	EditingID int64
	Edit      formView
	// TimeOpenID は時間記録のパネルを開いているタスク（0 なら閉じている）。
	TimeOpenID int64
	TimePanel  timePanel
	// EstimatedMin / ActualSec はボード全体の目標時間（分）と実績時間（秒）の合計。
	EstimatedMin int
	ActualSec    int64
	// LoadError はボードを読み込めなかったとき（API 停止中など）に表示するメッセージ。
	LoadError string
	// Message は操作が成功したときにメッセージウィンドウに出す文言（htmx の out-of-band で差し替える）。
	Message string
	// noticeFormat / noticeTaskID は、タスク名を入れた Message をボードの読み込み後に組み立てるためのもの。
	// タイマー操作の応答にはタスク名が無いので、読み込んだ一覧から名前を引く。
	noticeFormat string
	noticeTaskID int64
}

// Handler は画面のリクエストを処理する。
type Handler struct {
	api    *taskclient.Client
	logger *slog.Logger
	tmpl   *template.Template
	mux    *http.ServeMux
}

// NewHandler は画面のハンドラを返す。テンプレートの構文エラーは起動時に検出する。
func NewHandler(api *taskclient.Client, logger *slog.Logger) (*Handler, error) {
	tmpl, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("テンプレートの読み込みに失敗: %w", err)
	}
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("静的ファイルの読み込みに失敗: %w", err)
	}

	h := &Handler{api: api, logger: logger, tmpl: tmpl, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /{$}", h.index)
	h.mux.HandleFunc("GET /board", h.board)
	h.mux.HandleFunc("POST /tasks", h.create)
	h.mux.HandleFunc("POST /tasks/{id}/status", h.changeStatus)
	h.mux.HandleFunc("GET /tasks/{id}/edit", h.edit)
	h.mux.HandleFunc("PUT /tasks/{id}", h.update)
	h.mux.HandleFunc("DELETE /tasks/{id}", h.delete)
	h.mux.HandleFunc("POST /tasks/{id}/timer/start", h.startTimer)
	h.mux.HandleFunc("POST /tasks/{id}/timer/stop", h.stopTimer)
	h.mux.HandleFunc("GET /tasks/{id}/time", h.showTime)
	h.mux.HandleFunc("POST /tasks/{id}/time-entries", h.addTime)
	h.mux.HandleFunc("DELETE /tasks/{id}/time-entries/{entryID}", h.deleteTime)
	h.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// index はページ全体を返す。API に接続できなくても、エラーを表示したページは返す。
func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	view, err := h.loadBoard(r, boardView{})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "ボードの読み込みに失敗", "error", err)
		h.render(w, r, statusFor(err), "index", boardView{LoadError: userMessage(err)})
		return
	}
	h.render(w, r, http.StatusOK, "index", view)
}

func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	h.renderBoard(w, r, http.StatusOK, boardView{})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	form := formFromRequest(r)
	var created taskclient.Task
	estimated, err := parseEstimated(form.EstimatedMin)
	if err == nil {
		created, err = h.api.Create(r.Context(), taskclient.CreateInput{
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
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{Message: fmt.Sprintf(msgCreated, created.Title)})
}

func (h *Handler) changeStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	status := taskclient.Status(r.FormValue("status"))
	updated, err := h.api.Update(r.Context(), id, taskclient.UpdateInput{Status: &status})
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	// from は押したボタンが置かれていた列（メッセージの選び分けにだけ使う）。
	view := boardView{}
	if format := statusNotice(taskclient.Status(r.FormValue("from")), updated.Status); format != "" {
		view.Message = fmt.Sprintf(format, updated.Title)
	}
	h.renderBoard(w, r, http.StatusOK, view)
}

func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	t, err := h.api.Get(r.Context(), id)
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
		_, err = h.api.Update(r.Context(), id, taskclient.UpdateInput{
			Title:           &form.Title,
			Description:     &form.Description,
			SetEstimatedMin: true,
			EstimatedMin:    estimated,
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
	if err := h.api.Delete(r.Context(), id); err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{Message: msgDeleted})
}

// loadBoard は最新のタスクと進捗を view に詰める。フォームの状態は呼び出し元が設定する。
// 進捗は同じ一覧から数える。別に問い合わせると、間に他のアプリが書き込んだとき列の件数とずれる。
func (h *Handler) loadBoard(r *http.Request, view boardView) (boardView, error) {
	tasks, err := h.api.List(r.Context())
	if err != nil {
		return boardView{}, err
	}

	byStatus := make(map[taskclient.Status][]taskclient.Task, len(taskclient.Statuses))
	for _, t := range tasks {
		byStatus[t.Status] = append(byStatus[t.Status], t)
		if t.Status == taskclient.StatusDone {
			view.Done++
		}
		if t.EstimatedMin != nil {
			view.EstimatedMin += *t.EstimatedMin
		}
		view.ActualSec += t.ActualSec
	}
	// 完了列は直近に完了したものを上に出す（他の列は作成順）。
	// 完了日時は秒精度なので、同じ秒なら後から作ったものを上にする。
	slices.SortFunc(byStatus[taskclient.StatusDone], func(a, b taskclient.Task) int {
		return cmp.Or(cmp.Compare(completedUnix(b), completedUnix(a)), cmp.Compare(b.ID, a.ID))
	})

	view.Columns = make([]columnView, len(taskclient.Statuses))
	for i, s := range taskclient.Statuses {
		view.Columns[i] = columnView{Status: s, Label: statusLabels[s], Tasks: byStatus[s]}
	}
	view.Total = len(tasks)
	view.Percent = progressPercent(view.Done, view.Total)
	if view.noticeFormat != "" {
		for _, t := range tasks {
			if t.ID == view.noticeTaskID {
				view.Message = fmt.Sprintf(view.noticeFormat, t.Title)
			}
		}
	}
	return view, nil
}

func completedUnix(t taskclient.Task) int64 {
	if t.CompletedAt == nil {
		return 0
	}
	return t.CompletedAt.Unix()
}

func (h *Handler) renderBoard(w http.ResponseWriter, r *http.Request, status int, view boardView) {
	view, err := h.loadBoard(r, view)
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.render(w, r, status, "board", view)
}

// render はテンプレートをバッファに描画してから書き出す。
// 途中で失敗したときに、壊れた HTML を返さず 500 にできるようにするため。
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		h.logger.ErrorContext(r.Context(), "テンプレートの描画に失敗", "template", name, "error", err)
		http.Error(w, "サーバー内部でエラーが発生しました", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w) // ヘッダー送信後は失敗してもクライアントに伝える手段がない
}

// mutationError は htmx の操作に対するエラーを平文で返す。
// 画面では htmx:responseError を受けてメッセージを表示する（static/app.js）。
// 422 は htmx がボードを差し替えるフォーム再描画専用なので、ここでは使わない。
func (h *Handler) mutationError(w http.ResponseWriter, r *http.Request, err error) {
	status := statusFor(err)
	if status >= 500 {
		h.logger.ErrorContext(r.Context(), "画面の処理に失敗", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	http.Error(w, userMessage(err), status)
}

// statusFor は API 呼び出しのエラーを画面の HTTP ステータスに変換する。
func statusFor(err error) int {
	var apiErr *taskclient.APIError
	switch {
	case errors.Is(err, taskclient.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, taskclient.ErrUnavailable):
		return http.StatusBadGateway
	case isValidation(err):
		return http.StatusBadRequest
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized:
		return http.StatusBadGateway // web の設定（WEB_API_KEY）の問題なので、利用者の操作の誤りではない
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict:
		return http.StatusConflict // タイマーの二重起動など。API のメッセージをそのまま見せる
	default:
		return http.StatusInternalServerError
	}
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "タスクが見つかりません", http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// formFromRequest はフォームの入力値を読み取る。
// textarea の改行は CRLF で送られるので LF に揃え、ブラウザの maxlength と文字数の数え方を一致させる。
func formFromRequest(r *http.Request) formView {
	return formView{
		Title:        r.FormValue("title"),
		Description:  strings.ReplaceAll(r.FormValue("description"), "\r\n", "\n"),
		EstimatedMin: strings.TrimSpace(r.FormValue("estimated_min")),
	}
}

// parseEstimated はフォームの目標時間を解釈する。空欄は未設定（nil）。
// 数値かどうかだけをここで確かめ、範囲などの検証は API に任せる。
func parseEstimated(s string) (*int, error) {
	if s == "" {
		return nil, nil
	}
	m, err := strconv.Atoi(s)
	if err != nil {
		return nil, &taskclient.ValidationError{Errors: []taskclient.FieldError{
			{Field: "estimated_min", Code: codeNotInteger},
		}}
	}
	return &m, nil
}

func asValidation(err error) (*taskclient.ValidationError, bool) {
	var ve *taskclient.ValidationError
	ok := errors.As(err, &ve)
	return ve, ok
}

func isValidation(err error) bool {
	_, ok := asValidation(err)
	return ok
}
