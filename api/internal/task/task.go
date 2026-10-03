// Package task はタスクのドメインモデルと永続化を扱う。
package task

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Status はタスクの進行状態。
type Status string

// タスクのステータス。ボードの列に対応する。
const (
	StatusTodo  Status = "todo"
	StatusDoing Status = "doing"
	StatusDone  Status = "done"
)

// Statuses はボードの列の表示順。
var Statuses = []Status{StatusTodo, StatusDoing, StatusDone}

// Valid はステータスが定義済みの値かを返す。
func (s Status) Valid() bool {
	switch s {
	case StatusTodo, StatusDoing, StatusDone:
		return true
	}
	return false
}

// 入力値の上限。DB の CHECK 制約と揃える。
const (
	MaxTitleLen       = 100
	MaxDescriptionLen = 2000
	// MaxEstimatedMin は目標時間の上限（1週間）。桁違いの誤入力を弾くため。
	MaxEstimatedMin = 7 * 24 * 60
)

// Task はボード上の1件のタスク。
type Task struct {
	ID           int64
	Title        string
	Description  string
	Status       Status
	EstimatedMin *int // nil は目標時間が未設定
	CompletedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// ActualSec は記録した全区間の合計（秒）。計測中の区間は取得時点までを含める。
	ActualSec int64
	// RunningSince は計測中のタイマーの開始時刻。タイマーが動いていなければ nil。
	RunningSince *time.Time
}

// ErrNotFound は指定した ID のタスクが存在しないことを表す。
var ErrNotFound = errors.New("タスクが見つかりません")

// FieldError は1項目分の入力エラー。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError は入力値の検証エラー。複数項目のエラーをまとめて返す。
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, fe := range e.Errors {
		msgs[i] = fe.Field + ": " + fe.Message
	}
	return "入力値が不正です: " + strings.Join(msgs, ", ")
}

// Message は指定した項目のエラーメッセージを返す。エラーが無ければ空文字。
func (e *ValidationError) Message(field string) string {
	for _, fe := range e.Errors {
		if fe.Field == field {
			return fe.Message
		}
	}
	return ""
}

type validator struct{ errs []FieldError }

func (v *validator) add(field, msg string) {
	v.errs = append(v.errs, FieldError{Field: field, Message: msg})
}

func (v *validator) err() error {
	if len(v.errs) == 0 {
		return nil
	}
	return &ValidationError{Errors: v.errs}
}

func (v *validator) title(title string) {
	switch n := utf8.RuneCountInString(title); {
	case n == 0:
		v.add("title", "タイトルを入力してください")
	case n > MaxTitleLen:
		v.add("title", fmt.Sprintf("タイトルは%d文字以内で入力してください", MaxTitleLen))
	}
}

func (v *validator) description(desc string) {
	if utf8.RuneCountInString(desc) > MaxDescriptionLen {
		v.add("description", fmt.Sprintf("説明は%d文字以内で入力してください", MaxDescriptionLen))
	}
}

func (v *validator) status(s Status) {
	if !s.Valid() {
		v.add("status", "ステータスは todo / doing / done のいずれかを指定してください")
	}
}

func (v *validator) estimatedMin(m *int) {
	if m != nil && (*m < 0 || *m > MaxEstimatedMin) {
		v.add("estimated_min", fmt.Sprintf("目標時間は0〜%d分で指定してください", MaxEstimatedMin))
	}
}
