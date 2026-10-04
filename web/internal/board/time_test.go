package board

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kawafuchieirin/task-manager/client/taskclient"
)

func TestCard_ShowsEstimateActualAndDiff(t *testing.T) {
	h, api := newTestHandler(t)
	within := api.add("目標内", taskclient.StatusDoing, new(60))
	over := api.add("目標超過", taskclient.StatusDoing, new(30))
	api.add("目標なし", taskclient.StatusTodo, nil)
	api.addEntry(within.ID, api.now.Add(-time.Hour), 45*time.Minute)
	api.addEntry(over.ID, api.now.Add(-time.Hour), 50*time.Minute)

	body := send(t, h, http.MethodGet, "/board", nil).Body.String()
	assertContains(t, body,
		"もくひょう 1時間", "じっせき 45分", "のこり 15分",
		"もくひょう 30分", "じっせき 50分", "20分 オーバー！",
		"もくひょう ごうけい 1時間30分 ／ じっせき ごうけい 1時間35分")
	if strings.Count(body, "card__time--over") != 1 {
		t.Error("目標を超えたタスクだけを強調するはず")
	}
}

func TestTimer_StartShowsRunningAndStop(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("t", taskclient.StatusTodo, nil)

	rec := send(t, h, http.MethodPost, "/tasks/1/timer/start", url.Values{})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, "card--running", "けいそくちゅう", `data-elapsed="0"`, `hx-post="/tasks/1/timer/stop"`)
	if strings.Contains(body, `hx-post="/tasks/1/timer/start"`) {
		t.Error("計測中は開始ボタンを出さないはず")
	}

	api.now = api.now.Add(90 * time.Second)
	rec = send(t, h, http.MethodPost, "/tasks/1/timer/stop", url.Values{})
	assertStatus(t, rec, http.StatusOK)
	body = rec.Body.String()
	if strings.Contains(body, "card--running") {
		t.Error("停止後は計測中の表示を消すはず")
	}
	assertContains(t, body, "じっせき 1分", `hx-post="/tasks/1/timer/start"`)
}

func TestTimer_RunningElsewhereShowsMessage(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("動いているタスク", taskclient.StatusDoing, nil)
	api.add("別のタスク", taskclient.StatusTodo, nil)
	assertStatus(t, send(t, h, http.MethodPost, "/tasks/1/timer/start", url.Values{}), http.StatusOK)

	rec := send(t, h, http.MethodPost, "/tasks/2/timer/start", url.Values{})
	assertStatus(t, rec, http.StatusConflict)
	assertContains(t, rec.Body.String(), "「動いているタスク」の タイマーが うごいている！")
}

func TestTimer_NoStartButtonForDoneTask(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("完了", taskclient.StatusDone, nil)
	if strings.Contains(send(t, h, http.MethodGet, "/board", nil).Body.String(), "timer/start") {
		t.Error("完了したタスクには開始ボタンを出さないはず（API が拒否する）")
	}
}

func TestTimePanel_ShowAddDelete(t *testing.T) {
	h, api := newTestHandler(t)
	task := api.add("t", taskclient.StatusDoing, nil)
	api.add("他のタスク", taskclient.StatusTodo, nil)
	// 2026-10-03 09:00 UTC = 18:00 JST
	api.addEntry(task.ID, api.now.Add(-time.Hour), 30*time.Minute)

	rec := send(t, h, http.MethodGet, "/tasks/1/time", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, "10/3 17:00 – 17:30", "30分", `hx-delete="/tasks/1/time-entries/1"`, `hx-post="/tasks/1/time-entries"`)
	if strings.Count(body, `class="time-panel"`) != 1 {
		t.Error("パネルは開いたタスクにだけ表示するはず")
	}

	rec = send(t, h, http.MethodPost, "/tasks/1/time-entries", url.Values{"minutes": {"25"}})
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "25分", "じっせき 55分", `class="time-panel"`)

	rec = send(t, h, http.MethodDelete, "/tasks/1/time-entries/1", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "じっせき 25分", `class="time-panel"`)
	assertStatus(t, send(t, h, http.MethodDelete, "/tasks/1/time-entries/1", nil), http.StatusNotFound)
}

func TestTimePanel_AddValidation(t *testing.T) {
	for _, minutes := range []string{"", "0", "-5", "abc", "1441"} {
		t.Run(minutes, func(t *testing.T) {
			h, api := newTestHandler(t)
			api.add("t", taskclient.StatusDoing, nil)

			rec := send(t, h, http.MethodPost, "/tasks/1/time-entries", url.Values{"minutes": {minutes}})
			assertStatus(t, rec, http.StatusUnprocessableEntity)
			assertContains(t, rec.Body.String(), "さぎょうじかんは 1〜1440ふん で いれてください。", `class="time-panel"`, `aria-invalid="true"`)
			if got, _ := api.get(1); got.ActualSec != 0 {
				t.Error("検証エラーなのに記録された")
			}
		})
	}
}

func TestTimePanel_EmptyAndRunning(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("t", taskclient.StatusTodo, nil)

	assertContains(t, send(t, h, http.MethodGet, "/tasks/1/time", nil).Body.String(), "まだ きろくが ない。")

	assertStatus(t, send(t, h, http.MethodPost, "/tasks/1/timer/start", url.Values{}), http.StatusOK)
	body := send(t, h, http.MethodGet, "/tasks/1/time", nil).Body.String()
	assertContains(t, body, "– けいそくちゅう", "けいそくちゅうの タイマーを とりけしますか？")
}

func TestTimePanel_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	assertStatus(t, send(t, h, http.MethodGet, "/tasks/999/time", nil), http.StatusNotFound)
	assertStatus(t, send(t, h, http.MethodPost, "/tasks/999/timer/start", url.Values{}), http.StatusNotFound)
	assertStatus(t, send(t, h, http.MethodDelete, "/tasks/1/time-entries/abc", nil), http.StatusNotFound)
}

func TestFormatDuration(t *testing.T) {
	tests := map[int64]string{0: "0分", 30: "1分 みまん", 59: "1分 みまん", 60: "1分", 3599: "59分", 3600: "1時間", 5430: "1時間30分"}
	for in, want := range tests {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatClock(t *testing.T) {
	tests := map[int64]string{0: "0:00:00", 59: "0:00:59", 61: "0:01:01", 3661: "1:01:01", 36000: "10:00:00", -5: "0:00:00"}
	for in, want := range tests {
		if got := formatClock(in); got != want {
			t.Errorf("formatClock(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestToEntryView_SpansMidnight(t *testing.T) {
	// 2026-10-03 14:30 UTC = 23:30 JST、1時間後は翌日 00:30 JST
	start := time.Date(2026, 10, 3, 14, 30, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	v := toEntryView(taskclient.TimeEntry{ID: 1, StartedAt: start, EndedAt: &end, DurationSec: 3600})
	if v.Range != "10/3 23:30 – 10/4 00:30" {
		t.Errorf("Range = %q（日付をまたぐときは終了側にも日付を出す）", v.Range)
	}
}

func TestTimer_CountdownWithEstimate(t *testing.T) {
	h, api := newTestHandler(t)
	task := api.add("目標あり", taskclient.StatusDoing, new(30))
	api.add("目標なし", taskclient.StatusDoing, nil)
	api.addEntry(task.ID, api.now.Add(-2*time.Hour), 10*time.Minute)

	assertStatus(t, send(t, h, http.MethodPost, "/tasks/1/timer/start", url.Values{}), http.StatusOK)
	api.now = api.now.Add(5 * time.Minute)
	body := send(t, h, http.MethodGet, "/board", nil).Body.String()
	// 実績 15 分（記録 10 分 + 計測中 5 分）、目標 30 分 → のこり 15 分。JS が使う属性も出す。
	assertContains(t, body, `class="card__countdown">のこり 0:15:00</span>`,
		`data-estimated="1800"`, `data-task-id="1"`, `data-task-title="目標あり"`, `data-running-since="`)

	// 目標なしのタスクを計測するときはカウントダウンを出さない。
	assertStatus(t, send(t, h, http.MethodPost, "/tasks/1/timer/stop", url.Values{}), http.StatusOK)
	body = send(t, h, http.MethodPost, "/tasks/2/timer/start", url.Values{}).Body.String()
	if strings.Contains(body, "card__countdown") || strings.Contains(body, "data-estimated=") {
		t.Error("目標が無いタスクではカウントダウンを出さないはず")
	}
}

func TestTimer_CountdownOvertime(t *testing.T) {
	h, api := newTestHandler(t)
	task := api.add("超過", taskclient.StatusDoing, new(10))
	api.addEntry(task.ID, api.now.Add(-2*time.Hour), 9*time.Minute)
	assertStatus(t, send(t, h, http.MethodPost, "/tasks/1/timer/start", url.Values{}), http.StatusOK)
	api.now = api.now.Add(2*time.Minute + 30*time.Second)

	body := send(t, h, http.MethodGet, "/board", nil).Body.String()
	// html/template は「+」を &#43; にエスケープする（ブラウザでは「+」と表示される）。
	assertContains(t, body, `class="card__countdown card__countdown--over">オーバー &#43;0:01:30</span>`)
}

func TestCountdown(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		card cardView
		want string
	}{
		{"計測中でない", cardView{Task: taskclient.Task{EstimatedMin: new(30), ActualSec: 60}}, ""},
		{"目標なし", cardView{Task: taskclient.Task{RunningSince: &now, ActualSec: 60}}, ""},
		{"目標 0 分", cardView{Task: taskclient.Task{RunningSince: &now, EstimatedMin: new(0), ActualSec: 60}}, ""},
		{"残りあり", cardView{Task: taskclient.Task{RunningSince: &now, EstimatedMin: new(30), ActualSec: 61}}, "のこり 0:28:59"},
		{"ちょうど", cardView{Task: taskclient.Task{RunningSince: &now, EstimatedMin: new(1), ActualSec: 60}}, "オーバー +0:00:00"},
		{"超過", cardView{Task: taskclient.Task{RunningSince: &now, EstimatedMin: new(1), ActualSec: 3725}}, "オーバー +1:01:05"},
	}
	for _, tt := range tests {
		if got := tt.card.Countdown(); got != tt.want {
			t.Errorf("%s: Countdown() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestNotifyToggleInHeader(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, path := range []string{"/", "/reflections"} {
		assertContains(t, send(t, h, http.MethodGet, path, nil).Body.String(),
			`id="notify-toggle" aria-pressed="true">つうち: ON</button>`)
	}
}
