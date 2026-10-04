package task

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kawafuchieirin/task-manager/api/internal/db/dbtest"
)

// fakeClock はテストから時刻を進められる時計。
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestService(t *testing.T) (*Service, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)}
	svc := NewService(dbtest.New(t), WithClock(clock.now))
	return svc, clock
}

func ptr[T any](v T) *T { return &v }

func mustCreate(t *testing.T, svc *Service, in CreateInput) Task {
	t.Helper()
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

func assertValidation(t *testing.T, err error, field string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("ValidationError を期待したが %v", err)
	}
	if ve.Message(field) == "" {
		t.Errorf("項目 %s のエラーを期待したが %+v", field, ve.Errors)
	}
	for _, fe := range ve.Errors {
		if fe.Code == "" {
			t.Errorf("項目 %s のエラーにコードが無い: %+v", fe.Field, fe)
		}
	}
}

func assertCode(t *testing.T, err error, field, code string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("ValidationError を期待したが %v", err)
	}
	for _, fe := range ve.Errors {
		if fe.Field == field && fe.Code == code {
			return
		}
	}
	t.Errorf("項目 %s のコード %s を期待したが %+v", field, code, ve.Errors)
}

func TestCreate(t *testing.T) {
	svc, clock := newTestService(t)

	got := mustCreate(t, svc, CreateInput{Title: "  Go を学ぶ  ", Description: "入門", EstimatedMin: ptr(30)})

	if got.ID == 0 {
		t.Error("ID が採番されていない")
	}
	if got.Title != "Go を学ぶ" {
		t.Errorf("Title = %q, 前後の空白は除去される", got.Title)
	}
	if got.Status != StatusTodo {
		t.Errorf("Status = %q, 未指定なら todo", got.Status)
	}
	if got.EstimatedMin == nil || *got.EstimatedMin != 30 {
		t.Errorf("EstimatedMin = %v, want 30", got.EstimatedMin)
	}
	if got.CompletedAt != nil {
		t.Error("未完了なのに CompletedAt が入っている")
	}
	if !got.CreatedAt.Equal(clock.t) || !got.UpdatedAt.Equal(clock.t) {
		t.Errorf("CreatedAt / UpdatedAt = %v / %v, want %v", got.CreatedAt, got.UpdatedAt, clock.t)
	}
}

func TestCreate_AsDoneRecordsCompletedAt(t *testing.T) {
	svc, clock := newTestService(t)
	got := mustCreate(t, svc, CreateInput{Title: "t", Status: StatusDone})
	if got.CompletedAt == nil || !got.CompletedAt.Equal(clock.t) {
		t.Errorf("CompletedAt = %v, want %v", got.CompletedAt, clock.t)
	}
}

func TestCreate_Validation(t *testing.T) {
	tests := []struct {
		name  string
		in    CreateInput
		field string
		code  string
	}{
		{"タイトルが空", CreateInput{Title: ""}, "title", CodeRequired},
		{"タイトルが空白のみ", CreateInput{Title: "   "}, "title", CodeRequired},
		{"タイトルが101文字", CreateInput{Title: strings.Repeat("あ", 101)}, "title", CodeTooLong},
		{"説明が2001文字", CreateInput{Title: "t", Description: strings.Repeat("a", 2001)}, "description", CodeTooLong},
		{"不正なステータス", CreateInput{Title: "t", Status: "archived"}, "status", CodeInvalid},
		{"目標時間が負", CreateInput{Title: "t", EstimatedMin: ptr(-1)}, "estimated_min", CodeOutOfRange},
		{"目標時間が上限超過", CreateInput{Title: "t", EstimatedMin: ptr(MaxEstimatedMin + 1)}, "estimated_min", CodeOutOfRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t)
			_, err := svc.Create(context.Background(), tt.in)
			assertValidation(t, err, tt.field)
			assertCode(t, err, tt.field, tt.code)
		})
	}
}

func TestCreate_Boundaries(t *testing.T) {
	svc, _ := newTestService(t)
	in := CreateInput{
		Title:        strings.Repeat("あ", MaxTitleLen),
		Description:  strings.Repeat("a", MaxDescriptionLen),
		EstimatedMin: ptr(MaxEstimatedMin),
	}
	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Errorf("上限ちょうどの値が拒否された: %v", err)
	}
	if _, err := svc.Create(context.Background(), CreateInput{Title: "t", EstimatedMin: ptr(0)}); err != nil {
		t.Errorf("目標時間0分が拒否された: %v", err)
	}
}

func TestCreate_ReportsAllErrors(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Create(context.Background(), CreateInput{Title: "", EstimatedMin: ptr(-1)})
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Errors) != 2 {
		t.Fatalf("2件のエラーを期待したが %v", err)
	}
}

func TestUpdate_StatusTransitions(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	clock.advance(time.Hour)
	done, err := svc.Update(ctx, created.ID, UpdateInput{Status: ptr(StatusDone)})
	if err != nil {
		t.Fatal(err)
	}
	firstCompleted := clock.t
	if done.CompletedAt == nil || !done.CompletedAt.Equal(firstCompleted) {
		t.Fatalf("完了時に CompletedAt = %v, want %v", done.CompletedAt, firstCompleted)
	}
	if !done.UpdatedAt.Equal(clock.t) {
		t.Errorf("UpdatedAt が更新されていない: %v", done.UpdatedAt)
	}

	clock.advance(time.Hour)
	again, err := svc.Update(ctx, created.ID, UpdateInput{Status: ptr(StatusDone)})
	if err != nil {
		t.Fatal(err)
	}
	if again.CompletedAt == nil || !again.CompletedAt.Equal(firstCompleted) {
		t.Errorf("再度完了にしたら CompletedAt が変わった: %v", again.CompletedAt)
	}

	reopened, err := svc.Update(ctx, created.ID, UpdateInput{Status: ptr(StatusDoing)})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != StatusDoing || reopened.CompletedAt != nil {
		t.Errorf("完了を取り消したら doing・CompletedAt=nil になるはず: %+v", reopened)
	}
}

func TestUpdate_PartialFields(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "before", Description: "desc", EstimatedMin: ptr(30)})

	got, err := svc.Update(ctx, created.ID, UpdateInput{Title: ptr(" after ")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "after" || got.Description != "desc" || got.EstimatedMin == nil || *got.EstimatedMin != 30 {
		t.Errorf("指定した項目だけ変わるはず: %+v", got)
	}

	got, err = svc.Update(ctx, created.ID, UpdateInput{EstimatedMin: Nullable[int]{Set: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got.EstimatedMin != nil {
		t.Errorf("null 指定で目標時間がクリアされるはず: %v", *got.EstimatedMin)
	}
}

func TestUpdate_Validation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	_, err := svc.Update(ctx, created.ID, UpdateInput{Title: ptr(""), Status: ptr(Status("x"))})
	assertValidation(t, err, "title")
	assertValidation(t, err, "status")

	unchanged, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Title != "t" {
		t.Errorf("検証エラー時は更新されないはず: %q", unchanged.Title)
	}
}

func TestNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Get(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: %v", err)
	}
	if _, err := svc.Update(ctx, 999, UpdateInput{Title: ptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update: %v", err)
	}
	if err := svc.Delete(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete: %v", err)
	}
}

func TestDelete(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created := mustCreate(t, svc, CreateInput{Title: "t"})

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("削除後の Get: %v", err)
	}
}

func TestList(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	empty, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("0件のときは空スライス（nil ではない）を返すはず: %#v", empty)
	}

	a := mustCreate(t, svc, CreateInput{Title: "a"})
	b := mustCreate(t, svc, CreateInput{Title: "b", Status: StatusDone})
	c := mustCreate(t, svc, CreateInput{Title: "c"})

	all, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].ID != a.ID || all[1].ID != b.ID || all[2].ID != c.ID {
		t.Errorf("作成順に全件返すはず: %+v", all)
	}

	todo, err := svc.List(ctx, ptr(StatusTodo))
	if err != nil {
		t.Fatal(err)
	}
	if len(todo) != 2 || todo[0].ID != a.ID || todo[1].ID != c.ID {
		t.Errorf("todo のみに絞り込むはず: %+v", todo)
	}

	_, err = svc.List(ctx, ptr(Status("x")))
	assertValidation(t, err, "status")
}

func TestSummary(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	sum, err := svc.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 0 || sum.Done != 0 || sum.ProgressPercent() != 0 {
		t.Errorf("0件のとき: %+v, %d%%", sum, sum.ProgressPercent())
	}

	mustCreate(t, svc, CreateInput{Title: "a", Status: StatusDone})
	mustCreate(t, svc, CreateInput{Title: "b", Status: StatusDone})
	mustCreate(t, svc, CreateInput{Title: "c", Status: StatusDoing})

	sum, err = svc.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 3 || sum.Done != 2 {
		t.Errorf("Summary = %+v, want Total=3 Done=2", sum)
	}
	if got := sum.ProgressPercent(); got != 66 {
		t.Errorf("ProgressPercent = %d, want 66（切り捨て）", got)
	}
}

func TestProgressPercent(t *testing.T) {
	tests := []struct {
		sum  Summary
		want int
	}{
		{Summary{Total: 0, Done: 0}, 0},
		{Summary{Total: 1, Done: 1}, 100},
		{Summary{Total: 3, Done: 1}, 33},
		{Summary{Total: 1000, Done: 999}, 99}, // 全件完了でなければ 100 にならない
	}
	for _, tt := range tests {
		if got := tt.sum.ProgressPercent(); got != tt.want {
			t.Errorf("%+v.ProgressPercent() = %d, want %d", tt.sum, got, tt.want)
		}
	}
}

func TestNullable_UnmarshalJSON(t *testing.T) {
	var body struct {
		A Nullable[int] `json:"a"`
		B Nullable[int] `json:"b"`
		C Nullable[int] `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"a": 5, "b": null}`), &body); err != nil {
		t.Fatal(err)
	}
	if !body.A.Set || body.A.Value == nil || *body.A.Value != 5 {
		t.Errorf("値あり: %+v", body.A)
	}
	if !body.B.Set || body.B.Value != nil {
		t.Errorf("null: %+v", body.B)
	}
	if body.C.Set {
		t.Errorf("キーなし: %+v", body.C)
	}
}

func TestGoal(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	created := mustCreate(t, svc, CreateInput{Title: "Go を学ぶ", Goal: "  A Tour of Go を最後まで終える  "})
	if created.Goal != "A Tour of Go を最後まで終える" {
		t.Errorf("Goal = %q（前後の空白は除く）", created.Goal)
	}
	if noGoal := mustCreate(t, svc, CreateInput{Title: "ゴールなし"}); noGoal.Goal != "" {
		t.Errorf("未指定なら空文字: %q", noGoal.Goal)
	}

	updated, err := svc.Update(ctx, created.ID, UpdateInput{Goal: ptr("練習問題を3問解く")})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Goal != "練習問題を3問解く" || updated.Title != "Go を学ぶ" {
		t.Errorf("ゴールだけ変わるはず: %+v", updated)
	}
	cleared, err := svc.Update(ctx, created.ID, UpdateInput{Goal: ptr("")})
	if err != nil || cleared.Goal != "" {
		t.Errorf("空文字でゴールを消せるはず: %q, %v", cleared.Goal, err)
	}
	kept, err := svc.Update(ctx, created.ID, UpdateInput{Title: ptr("Go の基礎")})
	if err != nil || kept.Goal != "" {
		t.Errorf("指定しなければゴールは変わらない: %q, %v", kept.Goal, err)
	}
}

func TestGoal_Validation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, CreateInput{Title: "t", Goal: strings.Repeat("あ", MaxGoalLen+1)})
	assertCode(t, err, "goal", CodeTooLong)
	if _, err := svc.Create(ctx, CreateInput{Title: "t", Goal: strings.Repeat("あ", MaxGoalLen)}); err != nil {
		t.Errorf("上限ちょうどが拒否された: %v", err)
	}
	created := mustCreate(t, svc, CreateInput{Title: "t"})
	_, err = svc.Update(ctx, created.ID, UpdateInput{Goal: ptr(strings.Repeat("a", MaxGoalLen+1))})
	assertCode(t, err, "goal", CodeTooLong)
}
