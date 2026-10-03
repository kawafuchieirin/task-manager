package task

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartStopTimer(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	started, err := svc.StartTimer(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.EndedAt != nil || !started.StartedAt.Equal(clock.t) || started.TaskID != created.ID {
		t.Errorf("開始した区間: %+v", started)
	}

	// 計測中は、取得時点までの経過時間が実績に含まれ、開始時刻が分かる。
	clock.advance(90 * time.Second)
	running, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.ActualSec != 90 || running.RunningSince == nil || !running.RunningSince.Equal(started.StartedAt) {
		t.Errorf("計測中: ActualSec=%d RunningSince=%v", running.ActualSec, running.RunningSince)
	}

	clock.advance(30 * time.Second)
	stopped, err := svc.StopTimer(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.EndedAt == nil || stopped.Duration(clock.t) != 2*time.Minute {
		t.Errorf("停止した区間: %+v", stopped)
	}

	// 停止後は時間が進んでも実績は増えない。
	clock.advance(time.Hour)
	after, _ := svc.Get(ctx, created.ID)
	if after.ActualSec != 120 || after.RunningSince != nil {
		t.Errorf("停止後: ActualSec=%d RunningSince=%v", after.ActualSec, after.RunningSince)
	}
}

func TestStartTimer_MovesTodoToDoing(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	todo := mustCreate(t, svc, CreateInput{Title: "todo"})

	if _, err := svc.StartTimer(ctx, todo.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, todo.ID); got.Status != StatusDoing {
		t.Errorf("Status = %q, タイマーを開始したら進行中になるはず", got.Status)
	}
}

func TestStartTimer_OnlyOneRunning(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	a := mustCreate(t, svc, CreateInput{Title: "タスクA"})
	b := mustCreate(t, svc, CreateInput{Title: "タスクB"})

	if _, err := svc.StartTimer(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{b.ID, a.ID} {
		_, err := svc.StartTimer(ctx, id)
		var running *TimerRunningError
		if !errors.As(err, &running) || running.TaskID != a.ID || running.Title != "タスクA" {
			t.Errorf("タスク %d の開始: err = %v（動いているタスクを知らせるはず）", id, err)
		}
	}
	if got, _ := svc.Get(ctx, b.ID); got.Status != StatusTodo {
		t.Errorf("開始に失敗したタスクのステータスは変わらないはず: %q", got.Status)
	}

	// 止めれば別のタスクで開始できる。
	if _, err := svc.StopTimer(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartTimer(ctx, b.ID); err != nil {
		t.Errorf("停止後に別タスクで開始: %v", err)
	}
}

func TestTimerErrors(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	todo := mustCreate(t, svc, CreateInput{Title: "t"})
	done := mustCreate(t, svc, CreateInput{Title: "d", Status: StatusDone})

	if _, err := svc.StartTimer(ctx, done.ID); !errors.Is(err, ErrTaskCompleted) {
		t.Errorf("完了タスクで開始: %v", err)
	}
	if _, err := svc.StopTimer(ctx, todo.ID); !errors.Is(err, ErrTimerNotRunning) {
		t.Errorf("動いていないタイマーの停止: %v", err)
	}
	if _, err := svc.StartTimer(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("存在しないタスクで開始: %v", err)
	}
	if _, err := svc.StopTimer(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("存在しないタスクで停止: %v", err)
	}
}

func TestCompletingTaskStopsTimer(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	if _, err := svc.StartTimer(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	clock.advance(10 * time.Minute)
	done := StatusDone
	got, err := svc.Update(ctx, created.ID, UpdateInput{Status: &done})
	if err != nil {
		t.Fatal(err)
	}
	if got.RunningSince != nil || got.ActualSec != 600 {
		t.Errorf("完了したらタイマーが止まるはず: RunningSince=%v ActualSec=%d", got.RunningSince, got.ActualSec)
	}

	// 別のタスクでタイマーを開始できる（計測中の区間が残っていない）。
	other := mustCreate(t, svc, CreateInput{Title: "o"})
	if _, err := svc.StartTimer(ctx, other.ID); err != nil {
		t.Errorf("完了後に別タスクで開始: %v", err)
	}
}

func TestEditingDoneTaskWithoutTimerSucceeds(t *testing.T) {
	svc, _ := newTestService(t)
	created := mustCreate(t, svc, CreateInput{Title: "t", Status: StatusDone})
	done := StatusDone
	if _, err := svc.Update(context.Background(), created.ID, UpdateInput{Status: &done, Title: ptr("x")}); err != nil {
		t.Errorf("タイマーの無い完了タスクの更新: %v", err)
	}
}

func TestAddTimeEntry(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	start := clock.t.Add(-2 * time.Hour)
	end := clock.t.Add(-90 * time.Minute)
	entry, err := svc.AddTimeEntry(ctx, created.ID, EntryInput{StartedAt: start, EndedAt: end})
	if err != nil {
		t.Fatal(err)
	}
	if !entry.StartedAt.Equal(start) || entry.EndedAt == nil || !entry.EndedAt.Equal(end) {
		t.Errorf("追加した区間: %+v", entry)
	}
	if _, err := svc.AddTimeEntry(ctx, created.ID, EntryInput{StartedAt: start, EndedAt: start.Add(10 * time.Minute)}); err != nil {
		t.Fatal(err)
	}

	got, _ := svc.Get(ctx, created.ID)
	if got.ActualSec != 40*60 {
		t.Errorf("ActualSec = %d, want 2400（30分 + 10分）", got.ActualSec)
	}
	// 手動の区間はタイマーではないので、計測中にはならない。
	if got.RunningSince != nil {
		t.Error("手動で追加した区間でタイマーが動いている扱いになっている")
	}
}

func TestAddTimeEntry_AcceptsOtherTimezones(t *testing.T) {
	svc, clock := newTestService(t)
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	jst := time.FixedZone("JST", 9*60*60)

	start := clock.t.Add(-time.Hour).In(jst)
	entry, err := svc.AddTimeEntry(context.Background(), created.ID,
		EntryInput{StartedAt: start, EndedAt: start.Add(30 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if entry.StartedAt.Location() != time.UTC || !entry.StartedAt.Equal(start) {
		t.Errorf("UTC で保存されるはず: %v", entry.StartedAt)
	}
}

func TestAddTimeEntry_Validation(t *testing.T) {
	base := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC) // newTestService の時計と同じ
	tests := []struct {
		name  string
		in    EntryInput
		field string
	}{
		{"開始なし", EntryInput{EndedAt: base}, "started_at"},
		{"終了なし", EntryInput{StartedAt: base.Add(-time.Hour)}, "ended_at"},
		{"終了が開始より前", EntryInput{StartedAt: base.Add(-time.Hour), EndedAt: base.Add(-2 * time.Hour)}, "ended_at"},
		{"長さ0", EntryInput{StartedAt: base.Add(-time.Hour), EndedAt: base.Add(-time.Hour)}, "ended_at"},
		{"24時間超", EntryInput{StartedAt: base.Add(-25 * time.Hour), EndedAt: base}, "ended_at"},
		{"未来", EntryInput{StartedAt: base, EndedAt: base.Add(time.Minute)}, "ended_at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t)
			created := mustCreate(t, svc, CreateInput{Title: "t"})
			_, err := svc.AddTimeEntry(context.Background(), created.ID, tt.in)
			assertValidation(t, err, tt.field)
		})
	}
}

func TestAddTimeEntry_Boundaries(t *testing.T) {
	svc, clock := newTestService(t)
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	// ちょうど24時間・終了がちょうど現在は許可する。
	in := EntryInput{StartedAt: clock.t.Add(-MaxEntryDuration), EndedAt: clock.t}
	if _, err := svc.AddTimeEntry(context.Background(), created.ID, in); err != nil {
		t.Errorf("境界値が拒否された: %v", err)
	}
	if _, err := svc.AddTimeEntry(context.Background(), 999, in); !errors.Is(err, ErrNotFound) {
		t.Errorf("存在しないタスク: %v", err)
	}
}

func TestUpdateTimeEntry(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	entry, err := svc.AddTimeEntry(ctx, created.ID,
		EntryInput{StartedAt: clock.t.Add(-time.Hour), EndedAt: clock.t.Add(-30 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}

	newEnd := clock.t.Add(-15 * time.Minute)
	updated, err := svc.UpdateTimeEntry(ctx, entry.ID, EntryPatch{EndedAt: &newEnd})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.StartedAt.Equal(entry.StartedAt) || !updated.EndedAt.Equal(newEnd) {
		t.Errorf("終了だけ変わるはず: %+v", updated)
	}
	if got, _ := svc.Get(ctx, created.ID); got.ActualSec != 45*60 {
		t.Errorf("ActualSec = %d, want 2700", got.ActualSec)
	}

	badStart := newEnd.Add(time.Minute)
	_, err = svc.UpdateTimeEntry(ctx, entry.ID, EntryPatch{StartedAt: &badStart})
	assertValidation(t, err, "ended_at")
	if _, err := svc.UpdateTimeEntry(ctx, 999, EntryPatch{}); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("存在しない区間: %v", err)
	}
}

func TestUpdateTimeEntry_RunningIsRejected(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	running, err := svc.StartTimer(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := clock.t.Add(-time.Hour)
	if _, err := svc.UpdateTimeEntry(ctx, running.ID, EntryPatch{StartedAt: &start}); !errors.Is(err, ErrEntryRunning) {
		t.Errorf("計測中の区間の修正: %v", err)
	}
}

func TestDeleteTimeEntry(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	running, err := svc.StartTimer(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 計測中の区間を削除するとタイマーの取り消しになる。
	if err := svc.DeleteTimeEntry(ctx, running.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, created.ID); got.RunningSince != nil || got.ActualSec != 0 {
		t.Errorf("削除後: %+v", got)
	}
	if err := svc.DeleteTimeEntry(ctx, running.ID); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("2回目の削除: %v", err)
	}
}

func TestListTimeEntries(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	other := mustCreate(t, svc, CreateInput{Title: "o"})

	empty, err := svc.ListTimeEntries(ctx, created.ID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("0件は空スライス: %#v, %v", empty, err)
	}

	late := EntryInput{StartedAt: clock.t.Add(-time.Hour), EndedAt: clock.t.Add(-50 * time.Minute)}
	early := EntryInput{StartedAt: clock.t.Add(-3 * time.Hour), EndedAt: clock.t.Add(-2 * time.Hour)}
	for _, in := range []EntryInput{late, early} {
		if _, err := svc.AddTimeEntry(ctx, created.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.AddTimeEntry(ctx, other.ID, late); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.ListTimeEntries(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].StartedAt.Equal(early.StartedAt) {
		t.Errorf("このタスクの区間だけを開始順に返すはず: %+v", entries)
	}
	if _, err := svc.ListTimeEntries(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("存在しないタスク: %v", err)
	}
}

func TestList_AggregatesTimePerTask(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	a := mustCreate(t, svc, CreateInput{Title: "a"})
	b := mustCreate(t, svc, CreateInput{Title: "b"})
	mustCreate(t, svc, CreateInput{Title: "記録なし"})

	if _, err := svc.AddTimeEntry(ctx, a.ID, EntryInput{StartedAt: clock.t.Add(-time.Hour), EndedAt: clock.t}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartTimer(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	clock.advance(5 * time.Minute)

	tasks, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("時間記録の JOIN でタスクが増減してはいけない: %d 件", len(tasks))
	}
	got := map[string]int64{}
	for _, tk := range tasks {
		got[tk.Title] = tk.ActualSec
	}
	if got["a"] != 3600 || got["b"] != 300 || got["記録なし"] != 0 {
		t.Errorf("実績時間: %v", got)
	}

	sum, err := svc.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ActualSec != 3900 {
		t.Errorf("Summary.ActualSec = %d, want 3900", sum.ActualSec)
	}
}

func TestSummary_EstimatedTotal(t *testing.T) {
	svc, _ := newTestService(t)
	mustCreate(t, svc, CreateInput{Title: "a", EstimatedMin: ptr(30)})
	mustCreate(t, svc, CreateInput{Title: "b", EstimatedMin: ptr(45)})
	mustCreate(t, svc, CreateInput{Title: "目標なし"})

	sum, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.EstimatedMin != 75 {
		t.Errorf("EstimatedMin = %d, want 75（目標なしは数えない）", sum.EstimatedMin)
	}
}

func TestDeleteTask_RemovesTimeEntries(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	entry, err := svc.StartTimer(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetTimeEntry(ctx, entry.ID); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("タスク削除で時間記録も消えるはず: %v", err)
	}
	// 計測中だった区間が消えたので、別タスクで開始できる。
	other := mustCreate(t, svc, CreateInput{Title: "o"})
	if _, err := svc.StartTimer(ctx, other.ID); err != nil {
		t.Errorf("削除後に別タスクで開始: %v", err)
	}
}
