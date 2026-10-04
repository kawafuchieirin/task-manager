package board

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kawafuchieirin/task-manager/client/taskclient"
)

// reflectForm は振り返りの入力パネルの表示内容。
type reflectForm struct {
	TaskID int64
	Body   string
	Exists bool     // 保存済みの振り返りがあるか（「けす」を出すかどうか）
	Errors []string // 入力エラー
}

// openReflect は振り返りのパネルを開いた状態のボードを作る。保存済みの本文があれば入れておく。
func openReflect(t taskclient.Task) boardView {
	form := reflectForm{TaskID: t.ID}
	if t.Reflection != nil {
		form.Body, form.Exists = t.Reflection.Body, true
	}
	return boardView{ReflectOpenID: t.ID, Reflect: form}
}

func (h *Handler) showReflect(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	t, err := h.api.Get(r.Context(), id)
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, openReflect(t))
}

func (h *Handler) saveReflect(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	body := strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n")
	ref, err := h.api.SaveReflection(r.Context(), id, body)
	if ve, ok := asValidation(err); ok {
		form := reflectForm{TaskID: id, Body: body}
		for _, fe := range ve.Errors {
			form.Errors = append(form.Errors, fieldMessage(fe))
		}
		h.renderBoard(w, r, http.StatusUnprocessableEntity, boardView{ReflectOpenID: id, Reflect: form})
		return
	}
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{Message: reflectionNotice(ref)})
}

// retryExtract は抽出をやり直す（insight が止まっていて失敗したとき）。
func (h *Handler) retryExtract(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	ref, err := h.api.ExtractReflection(r.Context(), id)
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{Message: reflectionNotice(ref)})
}

func (h *Handler) deleteReflect(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := h.api.DeleteReflection(r.Context(), id); err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{Message: msgReflectionDeleted})
}

func reflectionNotice(ref taskclient.Reflection) string {
	if ref.ExtractStatus == taskclient.ExtractOK {
		return fmt.Sprintf(msgReflectionSaved, len(ref.Learned), len(ref.NotLearned))
	}
	return msgReflectionFailed
}

// reflectionRange は振り返り画面の期間の選択肢。
type reflectionRange struct {
	Key     string
	Label   string
	Current bool
}

var reflectionRanges = []struct{ key, label string }{
	{"today", "きょう"},
	{"week", "この1しゅうかん"},
	{"month", "この1かげつ"},
	{"all", "ぜんぶ"},
}

const defaultReflectionRange = "week"

// rangeBounds は期間のキーから、JST の日付の区切りで [from, to) を返す。all は制限なし。
// 「この1しゅうかん」は今日を含む直近7日、「この1かげつ」は今日を含む直近30日。
func rangeBounds(key string, now time.Time) (from, to *time.Time) {
	today := now.In(jst)
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, jst)
	end := start.AddDate(0, 0, 1)
	var days int
	switch key {
	case "today":
		days = 1
	case "week":
		days = 7
	case "month":
		days = 30
	default:
		return nil, nil
	}
	f := end.AddDate(0, 0, -days)
	return &f, &end
}

type reflectionItem struct {
	Text      string
	TaskTitle string
}

type reflectionsView struct {
	Ranges     []reflectionRange
	Entries    []taskclient.ReflectionEntry
	Learned    []reflectionItem
	NotLearned []reflectionItem
	Failed     int // 抽出に失敗した振り返りの数
	LoadError  string
}

// reflections は期間内の振り返りから、学んだこと・できなかったことをまとめて表示する。
func (h *Handler) reflections(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("range")
	valid := false
	view := reflectionsView{}
	for _, rr := range reflectionRanges {
		if rr.key == key {
			valid = true
		}
	}
	if !valid {
		key = defaultReflectionRange
	}
	for _, rr := range reflectionRanges {
		view.Ranges = append(view.Ranges, reflectionRange{Key: rr.key, Label: rr.label, Current: rr.key == key})
	}

	from, to := rangeBounds(key, time.Now())
	entries, err := h.api.ListReflections(r.Context(), from, to)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "振り返りの読み込みに失敗", "error", err)
		view.LoadError = userMessage(err)
		h.render(w, r, statusFor(err), "reflections", view)
		return
	}
	view.Entries = entries
	for _, e := range entries {
		if e.ExtractStatus == taskclient.ExtractFailed {
			view.Failed++
		}
		for _, s := range e.Learned {
			view.Learned = append(view.Learned, reflectionItem{Text: s, TaskTitle: e.TaskTitle})
		}
		for _, s := range e.NotLearned {
			view.NotLearned = append(view.NotLearned, reflectionItem{Text: s, TaskTitle: e.TaskTitle})
		}
	}
	h.render(w, r, http.StatusOK, "reflections", view)
}
