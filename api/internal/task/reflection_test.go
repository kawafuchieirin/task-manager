package task

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kawafuchieirin/task-manager/api/internal/db/dbtest"
)

// fakeExtractor は抽出結果や失敗をテストから指定できる Extractor。
type fakeExtractor struct {
	mu         sync.Mutex
	learned    []string
	notLearned []string
	err        error
	calls      []string
	// during は抽出の途中で呼ばれる（抽出中に振り返りが書き換えられる状況の再現用）。
	during func()
}

func (f *fakeExtractor) Extract(_ context.Context, text string) ([]string, []string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, text)
	during := f.during
	f.during = nil
	f.mu.Unlock()
	if during != nil {
		during()
	}
	return f.learned, f.notLearned, f.err
}

func newReflectionService(t *testing.T, ex Extractor) (*Service, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)}
	opts := []Option{WithClock(clock.now)}
	if ex != nil {
		opts = append(opts, WithExtractor(ex))
	}
	return NewService(dbtest.New(t), opts...), clock
}

func TestSaveReflection(t *testing.T) {
	ex := &fakeExtractor{learned: []string{"goroutine"}, notLearned: []string{"テスト"}}
	svc, clock := newReflectionService(t, ex)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	r, err := svc.SaveReflection(ctx, created.ID, "goroutine を理解した。\r\nテストはまだ曖昧。")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ExtractOK || !slices.Equal(r.Learned, []string{"goroutine"}) || !slices.Equal(r.NotLearned, []string{"テスト"}) {
		t.Errorf("保存結果: %+v", r)
	}
	if r.Body != "goroutine を理解した。\nテストはまだ曖昧。" {
		t.Errorf("CRLF は LF に揃えて保存するはず: %q", r.Body)
	}
	if !r.UpdatedAt.Equal(clock.t) {
		t.Errorf("UpdatedAt = %v", r.UpdatedAt)
	}
	if len(ex.calls) != 1 || ex.calls[0] != r.Body {
		t.Errorf("保存した本文で抽出するはず: %q", ex.calls)
	}

	// タスクの取得に振り返りが含まれる（一覧の JOIN）。
	got, _ := svc.Get(ctx, created.ID)
	if got.Reflection == nil || got.Reflection.Status != ExtractOK || !slices.Equal(got.Reflection.Learned, []string{"goroutine"}) {
		t.Errorf("タスクに振り返りが含まれるはず: %+v", got.Reflection)
	}
}

func TestSaveReflection_OverwritesAndClearsOldResult(t *testing.T) {
	ex := &fakeExtractor{learned: []string{"古い"}}
	svc, _ := newReflectionService(t, ex)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	if _, err := svc.SaveReflection(ctx, created.ID, "1回目"); err != nil {
		t.Fatal(err)
	}

	ex.learned, ex.err = nil, errors.New("insight 停止中")
	r, err := svc.SaveReflection(ctx, created.ID, "2回目")
	if err != nil {
		t.Fatal(err)
	}
	if r.Body != "2回目" || r.Status != ExtractFailed || len(r.Learned) != 0 {
		t.Errorf("上書き後は前回の抽出結果を残さないはず: %+v", r)
	}
}

func TestSaveReflection_ExtractorFailureStillSaves(t *testing.T) {
	ex := &fakeExtractor{err: errors.New("接続できない")}
	svc, _ := newReflectionService(t, ex)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	r, err := svc.SaveReflection(ctx, created.ID, "+ a")
	if err != nil {
		t.Fatalf("抽出に失敗しても保存は成功するはず: %v", err)
	}
	if r.Status != ExtractFailed || r.Body != "+ a" || r.Learned == nil || len(r.Learned) != 0 {
		t.Errorf("失敗時: %+v（一覧は nil ではなく空）", r)
	}

	// insight が復旧したらやり直せる。
	ex.err, ex.learned = nil, []string{"a"}
	r, err = svc.ExtractReflection(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ExtractOK || !slices.Equal(r.Learned, []string{"a"}) {
		t.Errorf("やり直し後: %+v", r)
	}
}

func TestSaveReflection_NoExtractorConfigured(t *testing.T) {
	svc, _ := newReflectionService(t, nil)
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	r, err := svc.SaveReflection(context.Background(), created.ID, "+ a")
	if err != nil || r.Status != ExtractFailed {
		t.Errorf("抽出先が無ければ failed: %+v, %v", r, err)
	}
}

func TestSaveReflection_DoesNotOverwriteNewerBody(t *testing.T) {
	ex := &fakeExtractor{learned: []string{"古い本文の結果"}}
	svc, _ := newReflectionService(t, ex)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	// 1回目の抽出中に、別の操作で本文が書き換えられた状況を再現する。
	ex.during = func() {
		if _, err := svc.db.ExecContext(ctx, `UPDATE reflections SET body = '新しい本文' WHERE task_id = ?`, created.ID); err != nil {
			t.Error(err)
		}
	}
	r, err := svc.SaveReflection(ctx, created.ID, "古い本文")
	if err != nil {
		t.Fatal(err)
	}
	if r.Body != "新しい本文" || r.Status != ExtractPending || len(r.Learned) != 0 {
		t.Errorf("古い本文の抽出結果で上書きしないはず: %+v", r)
	}
}

func TestSaveReflection_Validation(t *testing.T) {
	svc, _ := newReflectionService(t, &fakeExtractor{})
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	ctx := context.Background()

	_, err := svc.SaveReflection(ctx, created.ID, "  \n ")
	assertCode(t, err, "body", CodeRequired)
	_, err = svc.SaveReflection(ctx, created.ID, strings.Repeat("あ", MaxReflectionLen+1))
	assertCode(t, err, "body", CodeTooLong)
	if _, err := svc.SaveReflection(ctx, created.ID, strings.Repeat("あ", MaxReflectionLen)); err != nil {
		t.Errorf("上限ちょうどが拒否された: %v", err)
	}
	if _, err := svc.SaveReflection(ctx, 999, "a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("存在しないタスク: %v", err)
	}
}

func TestReflection_NotFoundAndDelete(t *testing.T) {
	svc, _ := newReflectionService(t, &fakeExtractor{})
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	if _, err := svc.GetReflection(ctx, created.ID); !errors.Is(err, ErrReflectionNotFound) {
		t.Errorf("未作成: %v", err)
	}
	if _, err := svc.ExtractReflection(ctx, created.ID); !errors.Is(err, ErrReflectionNotFound) {
		t.Errorf("未作成のやり直し: %v", err)
	}
	if err := svc.DeleteReflection(ctx, created.ID); !errors.Is(err, ErrReflectionNotFound) {
		t.Errorf("未作成の削除: %v", err)
	}
	if _, err := svc.GetReflection(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("存在しないタスク: %v", err)
	}

	if _, err := svc.SaveReflection(ctx, created.ID, "a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteReflection(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, created.ID); got.Reflection != nil {
		t.Error("削除後はタスクに振り返りが含まれないはず")
	}
}

func TestReflection_DeletedWithTask(t *testing.T) {
	svc, _ := newReflectionService(t, &fakeExtractor{})
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	if _, err := svc.SaveReflection(ctx, created.ID, "a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListReflections(ctx, nil, nil)
	if err != nil || len(list) != 0 {
		t.Errorf("タスクを削除したら振り返りも消えるはず: %+v, %v", list, err)
	}
}

func TestListReflections(t *testing.T) {
	ex := &fakeExtractor{learned: []string{"x"}}
	svc, clock := newReflectionService(t, ex)
	ctx := context.Background()
	a := mustCreate(t, svc, CreateInput{Title: "a", Status: StatusDone})
	b := mustCreate(t, svc, CreateInput{Title: "b"})
	mustCreate(t, svc, CreateInput{Title: "振り返りなし"})

	if _, err := svc.SaveReflection(ctx, a.ID, "a の振り返り"); err != nil { // 09:00
		t.Fatal(err)
	}
	clock.advance(24 * time.Hour)
	if _, err := svc.SaveReflection(ctx, b.ID, "b の振り返り"); err != nil { // 翌日 09:00
		t.Fatal(err)
	}

	all, err := svc.ListReflections(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].TaskTitle != "b" || all[1].TaskTitle != "a" || all[1].TaskStatus != StatusDone {
		t.Errorf("新しい順・タスク名とステータス付きで返すはず: %+v", all)
	}

	day1 := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)
	only, err := svc.ListReflections(ctx, &day1, &day2)
	if err != nil || len(only) != 1 || only[0].TaskTitle != "a" {
		t.Errorf("from 以上 to 未満で絞り込むはず: %+v, %v", only, err)
	}
	since, err := svc.ListReflections(ctx, &day2, nil)
	if err != nil || len(since) != 1 || since[0].TaskTitle != "b" {
		t.Errorf("from だけ: %+v, %v", since, err)
	}
}

func TestList_IncludesReflectionWithoutDuplicates(t *testing.T) {
	svc, clock := newReflectionService(t, &fakeExtractor{})
	ctx := context.Background()
	a := mustCreate(t, svc, CreateInput{Title: "a"})
	mustCreate(t, svc, CreateInput{Title: "b"})
	if _, err := svc.SaveReflection(ctx, a.ID, "x"); err != nil {
		t.Fatal(err)
	}
	// 時間記録が複数あっても、振り返りの JOIN でタスクが増えたり実績が倍になったりしない。
	for range 2 {
		if _, err := svc.AddTimeEntry(ctx, a.ID, EntryInput{StartedAt: clock.t.Add(-time.Hour), EndedAt: clock.t.Add(-30 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].ActualSec != 3600 || tasks[0].Reflection == nil || tasks[1].Reflection != nil {
		t.Errorf("一覧: %d 件, a の実績 %d 秒, 振り返り %v / %v", len(tasks), tasks[0].ActualSec, tasks[0].Reflection, tasks[1].Reflection)
	}
}
