package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rfc は JSON に埋め込む RFC3339 の時刻（UTC）。
func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func TestTimer_StartStop(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"t"}`)

	rec := do(t, h, http.MethodPost, "/api/v1/tasks/1/timer/start", "")
	assertStatus(t, rec, http.StatusCreated)
	if loc := rec.Header().Get("Location"); loc != "/api/v1/time-entries/1" {
		t.Errorf("Location = %q", loc)
	}
	started := decode[map[string]any](t, rec)
	if started["ended_at"] != nil || started["task_id"] != float64(1) {
		t.Errorf("開始した区間: %v", started)
	}

	// 計測中はタスクに running_since が入り、ステータスは進行中になる。
	running := decode[taskResponse](t, do(t, h, http.MethodGet, "/api/v1/tasks/1", ""))
	if running.RunningSince == nil || running.Status != "doing" {
		t.Errorf("計測中のタスク: %+v", running)
	}

	rec = do(t, h, http.MethodPost, "/api/v1/tasks/1/timer/stop", "")
	assertStatus(t, rec, http.StatusOK)
	if stopped := decode[entryResponse](t, rec); stopped.EndedAt == nil {
		t.Errorf("停止した区間: %+v", stopped)
	}
	if got := decode[taskResponse](t, do(t, h, http.MethodGet, "/api/v1/tasks/1", "")); got.RunningSince != nil {
		t.Error("停止後は running_since が null になるはず")
	}
}

func TestTimer_Conflicts(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"タスクA"}`)
	createTask(t, h, `{"title":"タスクB"}`)
	createTask(t, h, `{"title":"完了済み","status":"done"}`)

	assertStatus(t, do(t, h, http.MethodPost, "/api/v1/tasks/1/timer/start", ""), http.StatusCreated)

	detail := assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks/2/timer/start", ""),
		http.StatusConflict, codeTimerAlreadyRunning)
	if !strings.Contains(detail.Message, "タスクA") {
		t.Errorf("どのタスクのタイマーが動いているかを伝えるはず: %q", detail.Message)
	}
	assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks/2/timer/stop", ""), http.StatusConflict, codeTimerNotRunning)
	assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks/3/timer/start", ""), http.StatusConflict, codeTaskCompleted)
	assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks/999/timer/start", ""), http.StatusNotFound, codeNotFound)
	assertErrorCode(t, do(t, h, http.MethodPatch, "/api/v1/time-entries/1", `{"started_at":"`+rfc(time.Now().Add(-time.Hour))+`"}`),
		http.StatusConflict, codeEntryRunning)
}

func TestTimeEntries_CRUD(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"t"}`)
	now := time.Now()
	start, end := now.Add(-2*time.Hour), now.Add(-90*time.Minute)

	rec := do(t, h, http.MethodPost, "/api/v1/tasks/1/time-entries",
		fmt.Sprintf(`{"started_at":%q,"ended_at":%q}`, rfc(start), rfc(end)))
	assertStatus(t, rec, http.StatusCreated)
	if loc := rec.Header().Get("Location"); loc != "/api/v1/time-entries/1" {
		t.Errorf("Location = %q", loc)
	}
	if got := decode[entryResponse](t, rec); got.DurationSec != 1800 {
		t.Errorf("duration_sec = %d, want 1800", got.DurationSec)
	}

	// タイムゾーン付きの時刻も受け付ける（他のアプリが JST で送る場合）。
	jst := time.FixedZone("JST", 9*60*60)
	rec = do(t, h, http.MethodPost, "/api/v1/tasks/1/time-entries",
		fmt.Sprintf(`{"started_at":%q,"ended_at":%q}`, now.Add(-time.Hour).In(jst).Format(time.RFC3339),
			now.Add(-50*time.Minute).In(jst).Format(time.RFC3339)))
	assertStatus(t, rec, http.StatusCreated)
	if got := decode[entryResponse](t, rec); got.StartedAt.Location() != time.UTC {
		t.Errorf("UTC で返すはず: %v", got.StartedAt)
	}

	rec = do(t, h, http.MethodGet, "/api/v1/tasks/1/time-entries", "")
	assertStatus(t, rec, http.StatusOK)
	list := decode[struct {
		TimeEntries []entryResponse `json:"time_entries"`
	}](t, rec)
	if len(list.TimeEntries) != 2 {
		t.Fatalf("2件のはず: %+v", list)
	}

	if got := decode[taskResponse](t, do(t, h, http.MethodGet, "/api/v1/tasks/1", "")); got.ActualSec != 1800+600 {
		t.Errorf("actual_sec = %d, want 2400", got.ActualSec)
	}

	rec = do(t, h, http.MethodPatch, "/api/v1/time-entries/1", fmt.Sprintf(`{"ended_at":%q}`, rfc(now.Add(-60*time.Minute))))
	assertStatus(t, rec, http.StatusOK)
	if got := decode[entryResponse](t, rec); got.DurationSec != 3600 {
		t.Errorf("修正後 duration_sec = %d, want 3600", got.DurationSec)
	}

	assertStatus(t, do(t, h, http.MethodGet, "/api/v1/time-entries/1", ""), http.StatusOK)
	assertStatus(t, do(t, h, http.MethodDelete, "/api/v1/time-entries/1", ""), http.StatusNoContent)
	assertErrorCode(t, do(t, h, http.MethodGet, "/api/v1/time-entries/1", ""), http.StatusNotFound, codeNotFound)
	assertErrorCode(t, do(t, h, http.MethodDelete, "/api/v1/time-entries/1", ""), http.StatusNotFound, codeNotFound)
	assertErrorCode(t, do(t, h, http.MethodGet, "/api/v1/time-entries/abc", ""), http.StatusNotFound, codeNotFound)
}

func TestTimeEntries_RequestErrors(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"t"}`)
	now := time.Now()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"時刻の形式が不正", `{"started_at":"2026/10/03 09:00","ended_at":"2026/10/03 10:00"}`, http.StatusBadRequest, codeInvalidJSON},
		{"開始・終了なし", `{}`, http.StatusUnprocessableEntity, codeValidationFailed},
		{"終了が開始より前", fmt.Sprintf(`{"started_at":%q,"ended_at":%q}`, rfc(now.Add(-time.Hour)), rfc(now.Add(-2*time.Hour))),
			http.StatusUnprocessableEntity, codeValidationFailed},
		{"未来", fmt.Sprintf(`{"started_at":%q,"ended_at":%q}`, rfc(now), rfc(now.Add(time.Hour))),
			http.StatusUnprocessableEntity, codeValidationFailed},
		{"未知のフィールド", `{"minutes":30}`, http.StatusBadRequest, codeInvalidJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks/1/time-entries", tt.body), tt.wantStatus, tt.wantCode)
		})
	}
	assertErrorCode(t, do(t, h, http.MethodPost, "/api/v1/tasks/999/time-entries",
		fmt.Sprintf(`{"started_at":%q,"ended_at":%q}`, rfc(now.Add(-time.Hour)), rfc(now))), http.StatusNotFound, codeNotFound)
}

func TestTimer_MethodNotAllowed(t *testing.T) {
	h := newTestHandler(t, Options{})
	tests := []struct{ method, path, allow string }{
		{http.MethodGet, "/api/v1/tasks/1/timer/start", "POST"},
		{http.MethodGet, "/api/v1/tasks/1/timer/stop", "POST"},
		{http.MethodDelete, "/api/v1/tasks/1/time-entries", "GET, POST"},
		{http.MethodPost, "/api/v1/time-entries/1", "GET, PATCH, DELETE"},
	}
	for _, tt := range tests {
		rec := do(t, h, tt.method, tt.path, "")
		assertErrorCode(t, rec, http.StatusMethodNotAllowed, codeMethodNotAllowed)
		if got := rec.Header().Get("Allow"); got != tt.allow {
			t.Errorf("%s %s: Allow = %q, want %q", tt.method, tt.path, got, tt.allow)
		}
	}
}

func TestSummary_IncludesTime(t *testing.T) {
	h := newTestHandler(t, Options{})
	createTask(t, h, `{"title":"a","estimated_min":30}`)
	now := time.Now()
	assertStatus(t, do(t, h, http.MethodPost, "/api/v1/tasks/1/time-entries",
		fmt.Sprintf(`{"started_at":%q,"ended_at":%q}`, rfc(now.Add(-time.Hour)), rfc(now.Add(-40*time.Minute)))), http.StatusCreated)

	got := decode[summaryResponse](t, do(t, h, http.MethodGet, "/api/v1/stats/summary", ""))
	if got.EstimatedMin != 30 || got.ActualSec != 1200 {
		t.Errorf("summary = %+v", got)
	}
}
