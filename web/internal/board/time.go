package board

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kawafuchieirin/task-manager/client/taskclient"
)

// maxManualMinutes は手動で追加できる作業時間の上限（API の1区間の上限 24 時間に合わせる）。
const maxManualMinutes = 24 * 60

// cardView はボードの1枚のカードの表示内容。
type cardView struct {
	taskclient.Task
	// Panel は時間記録のパネルを開いているカードだけに入る。
	Panel *timePanel
	// ReflectPanel は振り返りのパネルを開いているカードだけに入る。
	ReflectPanel *reflectForm
}

// Running はタイマーで計測中かどうかを返す。
func (c cardView) Running() bool { return c.RunningSince != nil }

// CanStartTimer はタイマーの開始ボタンを出すかを返す。完了したタスクでは API が開始を拒否する。
func (c cardView) CanStartTimer() bool {
	return !c.Running() && c.Status != taskclient.StatusDone
}

// ActualText は実績時間の表示（分単位）。
func (c cardView) ActualText() string { return formatDuration(c.ActualSec) }

// Over は目標時間を超えているかを返す。目標が未設定なら false。
func (c cardView) Over() bool {
	return c.EstimatedMin != nil && c.ActualSec > int64(*c.EstimatedMin)*60
}

// DiffText は目標との差分（「残り 15分」「15分 超過」）を返す。目標が未設定なら空文字。
func (c cardView) DiffText() string {
	if c.EstimatedMin == nil {
		return ""
	}
	diff := c.ActualSec - int64(*c.EstimatedMin)*60
	if diff > 0 {
		return formatDuration(diff) + " オーバー！"
	}
	return "のこり " + formatDuration(-diff)
}

// timePanel は時間記録のパネルの表示内容。
type timePanel struct {
	TaskID  int64
	Entries []entryView
	Minutes string   // 手動追加フォームの入力値（エラー時に保持する）
	Errors  []string // 手動追加・削除のエラーメッセージ
}

type entryView struct {
	ID       int64
	TaskID   int64
	Range    string
	Duration string
	Running  bool
}

func toEntryView(e taskclient.TimeEntry) entryView {
	start := e.StartedAt.In(jst)
	v := entryView{ID: e.ID, TaskID: e.TaskID, Duration: formatDuration(e.DurationSec), Running: e.EndedAt == nil}
	switch {
	case e.EndedAt == nil:
		v.Range = start.Format("1/2 15:04") + " – けいそくちゅう"
	case e.EndedAt.In(jst).YearDay() == start.YearDay() && e.EndedAt.In(jst).Year() == start.Year():
		v.Range = start.Format("1/2 15:04") + " – " + e.EndedAt.In(jst).Format("15:04")
	default:
		v.Range = start.Format("1/2 15:04") + " – " + e.EndedAt.In(jst).Format("1/2 15:04")
	}
	return v
}

// formatDuration は秒数を「1時間5分」の形にする（分未満は切り捨て）。1分未満で記録があれば「1分 みまん」。
func formatDuration(sec int64) string {
	if sec > 0 && sec < 60 {
		return "1分 みまん"
	}
	return formatMinutes(int(sec / 60))
}

// formatClock は秒数を「1:02:03」の形にする（計測中タイマーの表示。static/app.js と同じ書式）。
func formatClock(sec int64) string {
	sec = max(0, sec)
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
}

// Card はテンプレートからカードの表示内容を作る。時間記録のパネルを開いているカードにだけパネルを渡す。
func (b boardView) Card(t taskclient.Task) cardView {
	c := cardView{Task: t}
	if t.ID == b.TimeOpenID {
		panel := b.TimePanel
		panel.TaskID = t.ID
		c.Panel = &panel
	}
	if t.ID == b.ReflectOpenID {
		form := b.Reflect
		form.TaskID = t.ID
		c.ReflectPanel = &form
	}
	return c
}

func (h *Handler) startTimer(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if _, err := h.api.StartTimer(r.Context(), id); err != nil {
		var apiErr *taskclient.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "timer_already_running" {
			// どのタスクのタイマーが動いているかを、画面の言葉づかいで伝える。
			http.Error(w, h.runningElsewhereMessage(r), http.StatusConflict)
			return
		}
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{noticeFormat: msgTimerStarted, noticeTaskID: id})
}

// runningElsewhereMessage は、タイマーが動いているタスクの名前を入れたメッセージを返す。
// 名前を取得できなくても（その間に止められた場合など）、名前なしの文言を返す。
func (h *Handler) runningElsewhereMessage(r *http.Request) string {
	tasks, err := h.api.List(r.Context())
	if err == nil {
		for _, t := range tasks {
			if t.RunningSince != nil {
				return fmt.Sprintf(msgTimerRunningElsewhere, t.Title)
			}
		}
	}
	return msgTimerRunningElsewhereUnknown
}

func (h *Handler) stopTimer(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if _, err := h.api.StopTimer(r.Context(), id); err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderBoard(w, r, http.StatusOK, boardView{noticeFormat: msgTimerStopped, noticeTaskID: id})
}

// showTime は時間記録のパネルを開いたボードを返す。
func (h *Handler) showTime(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	h.renderTimePanel(w, r, http.StatusOK, id, timePanel{})
}

// addTime は「今までの N 分」を区間として追加する。
// 画面では分数だけを入力してもらい、細かな開始・終了時刻の指定は API（PATCH）で行える。
func (h *Handler) addTime(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	raw := strings.TrimSpace(r.FormValue("minutes"))
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes < 1 || minutes > maxManualMinutes {
		h.renderTimePanel(w, r, http.StatusUnprocessableEntity, id, timePanel{
			Minutes: raw,
			Errors:  []string{fieldMessage(taskclient.FieldError{Field: "minutes", Code: codeOutOfRange})},
		})
		return
	}

	end := time.Now().Truncate(time.Second)
	_, err = h.api.AddTimeEntry(r.Context(), id, end.Add(-time.Duration(minutes)*time.Minute), end)
	if ve, ok := asValidation(err); ok {
		panel := timePanel{Minutes: raw}
		for _, fe := range ve.Errors {
			panel.Errors = append(panel.Errors, fieldMessage(fe))
		}
		h.renderTimePanel(w, r, http.StatusUnprocessableEntity, id, panel)
		return
	}
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderTimePanel(w, r, http.StatusOK, id, timePanel{})
}

func (h *Handler) deleteTime(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	entryID, err := strconv.ParseInt(r.PathValue("entryID"), 10, 64)
	if err != nil || entryID <= 0 {
		http.Error(w, "時間記録が見つかりません", http.StatusNotFound)
		return
	}
	if err := h.api.DeleteTimeEntry(r.Context(), entryID); err != nil {
		h.mutationError(w, r, err)
		return
	}
	h.renderTimePanel(w, r, http.StatusOK, id, timePanel{})
}

// renderTimePanel は時間記録を読み込み、パネルを開いた状態のボードを返す。
func (h *Handler) renderTimePanel(w http.ResponseWriter, r *http.Request, status int, taskID int64, panel timePanel) {
	entries, err := h.api.ListTimeEntries(r.Context(), taskID)
	if err != nil {
		h.mutationError(w, r, err)
		return
	}
	for _, e := range entries {
		panel.Entries = append(panel.Entries, toEntryView(e))
	}
	h.renderBoard(w, r, status, boardView{TimeOpenID: taskID, TimePanel: panel})
}
