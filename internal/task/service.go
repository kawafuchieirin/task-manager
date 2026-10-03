package task

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kawafuchieirin/task-manager/internal/db"
)

// CreateInput はタスク作成時の入力。
type CreateInput struct {
	Title        string
	Description  string
	Status       Status // 空なら todo
	EstimatedMin *int
}

// UpdateInput はタスクの部分更新の入力。nil（Set=false）の項目は変更しない。
type UpdateInput struct {
	Title        *string
	Description  *string
	Status       *Status
	EstimatedMin Nullable[int]
}

// Nullable は部分更新で「指定なし」「null で値をクリア」「値を設定」を区別する。
type Nullable[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON はキーが存在したときだけ呼ばれるので、呼ばれた時点で Set=true にする。
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	n.Set = true
	if bytes.Equal(data, []byte("null")) {
		n.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	n.Value = &v
	return nil
}

// Summary はボード全体の進捗。
type Summary struct {
	Total int
	Done  int
}

// ProgressPercent は完了率（%）を返す。切り捨てなので 100 は全件完了のときだけ。
// タスクが0件なら 0 を返す。
func (s Summary) ProgressPercent() int {
	if s.Total == 0 {
		return 0
	}
	return s.Done * 100 / s.Total
}

// Service はタスクの操作を提供する。
type Service struct {
	db  *sql.DB
	now func() time.Time
}

// Option は Service の設定を変更する。
type Option func(*Service)

// WithClock は現在時刻の取得方法を差し替える。テストで時刻を固定するために使う。
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// NewService は Service を返す。
func NewService(database *sql.DB, opts ...Option) *Service {
	s := &Service{db: database, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

const selectColumns = `id, title, description, status, estimated_min, completed_at, created_at, updated_at`

// List はタスクを作成順に返す。status を指定するとそのステータスのみに絞り込む。
func (s *Service) List(ctx context.Context, status *Status) ([]Task, error) {
	query := `SELECT ` + selectColumns + ` FROM tasks`
	var args []any
	if status != nil {
		if !status.Valid() {
			v := &validator{}
			v.status(*status)
			return nil, v.err()
		}
		query += ` WHERE status = ?`
		args = append(args, string(*status))
	}
	query += ` ORDER BY id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("タスク一覧の取得に失敗: %w", err)
	}
	defer func() { _ = rows.Close() }() // 読み取りエラーは rows.Err で検査する

	tasks := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("タスク一覧の読み取りに失敗: %w", err)
	}
	return tasks, nil
}

// Get は ID でタスクを取得する。存在しなければ ErrNotFound を返す。
func (s *Service) Get(ctx context.Context, id int64) (Task, error) {
	return getTask(ctx, s.db, id)
}

// Create はタスクを作成する。
func (s *Service) Create(ctx context.Context, in CreateInput) (Task, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Status == "" {
		in.Status = StatusTodo
	}

	v := &validator{}
	v.title(in.Title)
	v.description(in.Description)
	v.status(in.Status)
	v.estimatedMin(in.EstimatedMin)
	if err := v.err(); err != nil {
		return Task{}, err
	}

	now := s.now()
	var completedAt *time.Time
	if in.Status == StatusDone {
		completedAt = &now
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (title, description, status, estimated_min, completed_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.Title, in.Description, string(in.Status), nullInt(in.EstimatedMin), nullTime(completedAt),
		db.FormatTime(now), db.FormatTime(now),
	)
	if err != nil {
		return Task{}, fmt.Errorf("タスクの作成に失敗: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Task{}, fmt.Errorf("作成したタスクの ID 取得に失敗: %w", err)
	}
	return s.Get(ctx, id)
}

// Update はタスクを部分更新する。
// 完了にしたときは完了日時を記録し、完了から戻したときは完了日時を消す。
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("トランザクション開始に失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Commit 後の Rollback は何もしない

	t, err := getTask(ctx, tx, id)
	if err != nil {
		return Task{}, err
	}

	v := &validator{}
	if in.Title != nil {
		t.Title = strings.TrimSpace(*in.Title)
		v.title(t.Title)
	}
	if in.Description != nil {
		t.Description = *in.Description
		v.description(t.Description)
	}
	if in.EstimatedMin.Set {
		t.EstimatedMin = in.EstimatedMin.Value
		v.estimatedMin(t.EstimatedMin)
	}
	now := s.now()
	if in.Status != nil {
		v.status(*in.Status)
		t.CompletedAt = completedAtAfter(t, *in.Status, now)
		t.Status = *in.Status
	}
	if err := v.err(); err != nil {
		return Task{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET title = ?, description = ?, status = ?, estimated_min = ?, completed_at = ?, updated_at = ?
		 WHERE id = ?`,
		t.Title, t.Description, string(t.Status), nullInt(t.EstimatedMin), nullTime(t.CompletedAt),
		db.FormatTime(now), id,
	); err != nil {
		return Task{}, fmt.Errorf("タスクの更新に失敗: %w", err)
	}
	updated, err := getTask(ctx, tx, id)
	if err != nil {
		return Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("タスク更新のコミットに失敗: %w", err)
	}
	return updated, nil
}

// completedAtAfter はステータス変更後の完了日時を返す。
// 完了済みのタスクを再度「完了」にしても、最初の完了日時を保つ。
func completedAtAfter(t Task, next Status, now time.Time) *time.Time {
	switch {
	case next != StatusDone:
		return nil
	case t.Status == StatusDone:
		return t.CompletedAt
	default:
		return &now
	}
}

// Delete はタスクを削除する。関連する時間記録・振り返りも DB の制約で削除される。
func (s *Service) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("タスクの削除に失敗: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("削除件数の取得に失敗: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Summary はボード全体の進捗を返す。
func (s *Service) Summary(ctx context.Context) (Summary, error) {
	var sum Summary
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*), coalesce(sum(status = 'done'), 0) FROM tasks`,
	).Scan(&sum.Total, &sum.Done)
	if err != nil {
		return Summary{}, fmt.Errorf("進捗の集計に失敗: %w", err)
	}
	return sum, nil
}

// queryer は *sql.DB と *sql.Tx の共通部分。
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getTask(ctx context.Context, q queryer, id int64) (Task, error) {
	row := q.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(sc scanner) (Task, error) {
	var (
		t                    Task
		status               string
		estimated            sql.NullInt64
		completed            sql.NullString
		createdAt, updatedAt string
	)
	if err := sc.Scan(&t.ID, &t.Title, &t.Description, &status, &estimated, &completed, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, err
		}
		return Task{}, fmt.Errorf("タスクの読み取りに失敗: %w", err)
	}
	t.Status = Status(status)
	if estimated.Valid {
		m := int(estimated.Int64)
		t.EstimatedMin = &m
	}

	var err error
	if completed.Valid {
		c, perr := db.ParseTime(completed.String)
		if perr != nil {
			return Task{}, perr
		}
		t.CompletedAt = &c
	}
	if t.CreatedAt, err = db.ParseTime(createdAt); err != nil {
		return Task{}, err
	}
	if t.UpdatedAt, err = db.ParseTime(updatedAt); err != nil {
		return Task{}, err
	}
	return t, nil
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullTime(p *time.Time) any {
	if p == nil {
		return nil
	}
	return db.FormatTime(*p)
}
