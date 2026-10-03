package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kawafuchieirin/task-manager/api/internal/db"
)

// TimeEntry はタスクに作業した1区間。EndedAt が nil の区間はタイマーで計測中。
type TimeEntry struct {
	ID        int64
	TaskID    int64
	StartedAt time.Time
	EndedAt   *time.Time
}

// Duration は区間の長さを返す。計測中なら now までの長さ。
func (e TimeEntry) Duration(now time.Time) time.Duration {
	end := now
	if e.EndedAt != nil {
		end = *e.EndedAt
	}
	return max(0, end.Sub(e.StartedAt))
}

// MaxEntryDuration は1区間の上限。日付の打ち間違いなど桁違いの誤入力を弾くため。
const MaxEntryDuration = 24 * time.Hour

// タイマーと時間記録のエラー。
var (
	ErrTimerNotRunning = errors.New("このタスクのタイマーは動いていません")
	ErrTaskCompleted   = errors.New("完了したタスクのタイマーは開始できません。未完了に戻してから開始してください")
	ErrEntryRunning    = errors.New("計測中の区間は編集できません。先にタイマーを停止してください")
	ErrEntryNotFound   = errors.New("時間記録が見つかりません")
)

// TimerRunningError は、すでに別の（または同じ）タスクのタイマーが動いていることを表す。
// 同時に動かせるタイマーは全体で1つだけ（DB の部分ユニークインデックスでも保証している）。
type TimerRunningError struct {
	TaskID int64
	Title  string
}

func (e *TimerRunningError) Error() string {
	return fmt.Sprintf("「%s」のタイマーが動いています。先に停止してください", e.Title)
}

// EntryInput は時間記録を手動で追加するときの入力。
type EntryInput struct {
	StartedAt time.Time
	EndedAt   time.Time
}

// EntryPatch は時間記録の部分更新の入力。nil の項目は変更しない。
type EntryPatch struct {
	StartedAt *time.Time
	EndedAt   *time.Time
}

// StartTimer はタスクのタイマーを開始する。
// 未着手のタスクは進行中にする（タイマーを動かす＝作業を始めた、とみなす）。
func (s *Service) StartTimer(ctx context.Context, taskID int64) (TimeEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TimeEntry{}, fmt.Errorf("トランザクション開始に失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Commit 後の Rollback は何もしない

	now := s.now()
	t, err := getTask(ctx, tx, taskID, now)
	if err != nil {
		return TimeEntry{}, err
	}
	if t.Status == StatusDone {
		return TimeEntry{}, ErrTaskCompleted
	}
	var running TimerRunningError
	err = tx.QueryRowContext(ctx,
		`SELECT t.id, t.title FROM time_entries e JOIN tasks t ON t.id = e.task_id WHERE e.ended_at IS NULL`,
	).Scan(&running.TaskID, &running.Title)
	switch {
	case err == nil:
		return TimeEntry{}, &running
	case !errors.Is(err, sql.ErrNoRows):
		return TimeEntry{}, fmt.Errorf("計測中タイマーの確認に失敗: %w", err)
	}

	if t.Status == StatusTodo {
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = 'doing', updated_at = ? WHERE id = ?`,
			db.FormatTime(now), taskID); err != nil {
			return TimeEntry{}, fmt.Errorf("ステータスの更新に失敗: %w", err)
		}
	}
	entry, err := insertEntry(ctx, tx, taskID, now, nil)
	if err != nil {
		return TimeEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return TimeEntry{}, fmt.Errorf("タイマー開始のコミットに失敗: %w", err)
	}
	return entry, nil
}

// StopTimer はタスクの計測中タイマーを止め、確定した区間を返す。
func (s *Service) StopTimer(ctx context.Context, taskID int64) (TimeEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TimeEntry{}, fmt.Errorf("トランザクション開始に失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Commit 後の Rollback は何もしない

	now := s.now()
	if _, err := getTask(ctx, tx, taskID, now); err != nil {
		return TimeEntry{}, err
	}
	var entryID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM time_entries WHERE task_id = ? AND ended_at IS NULL`, taskID).Scan(&entryID)
	if errors.Is(err, sql.ErrNoRows) {
		return TimeEntry{}, ErrTimerNotRunning
	}
	if err != nil {
		return TimeEntry{}, fmt.Errorf("計測中タイマーの確認に失敗: %w", err)
	}
	if err := stopRunning(ctx, tx, taskID, now); err != nil {
		return TimeEntry{}, err
	}
	entry, err := getEntry(ctx, tx, entryID)
	if err != nil {
		return TimeEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return TimeEntry{}, fmt.Errorf("タイマー停止のコミットに失敗: %w", err)
	}
	return entry, nil
}

// stopRunning はタスクの計測中の区間を now で終了させる。計測中でなければ ErrTimerNotRunning。
// 時計が戻っていても終了が開始より前にならないよう、開始時刻で下限を取る。
func stopRunning(ctx context.Context, q queryer, taskID int64, now time.Time) error {
	res, err := q.ExecContext(ctx,
		`UPDATE time_entries SET ended_at = max(started_at, ?) WHERE task_id = ? AND ended_at IS NULL`,
		db.FormatTime(now), taskID)
	if err != nil {
		return fmt.Errorf("タイマーの停止に失敗: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("停止件数の取得に失敗: %w", err)
	}
	if n == 0 {
		return ErrTimerNotRunning
	}
	return nil
}

// ListTimeEntries はタスクの時間記録を開始時刻の順に返す。
func (s *Service) ListTimeEntries(ctx context.Context, taskID int64) ([]TimeEntry, error) {
	if _, err := s.Get(ctx, taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, started_at, ended_at FROM time_entries WHERE task_id = ? ORDER BY started_at, id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("時間記録の取得に失敗: %w", err)
	}
	defer func() { _ = rows.Close() }() // 読み取りエラーは rows.Err で検査する

	entries := []TimeEntry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("時間記録の読み取りに失敗: %w", err)
	}
	return entries, nil
}

// GetTimeEntry は ID で時間記録を取得する。
func (s *Service) GetTimeEntry(ctx context.Context, id int64) (TimeEntry, error) {
	return getEntry(ctx, s.db, id)
}

// AddTimeEntry は終了済みの区間を手動で追加する（タイマーを使わずに記録したいとき）。
func (s *Service) AddTimeEntry(ctx context.Context, taskID int64, in EntryInput) (TimeEntry, error) {
	start, end := in.StartedAt.UTC().Truncate(time.Second), in.EndedAt.UTC().Truncate(time.Second)
	if err := validateEntry(start, end, s.now()); err != nil {
		return TimeEntry{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TimeEntry{}, fmt.Errorf("トランザクション開始に失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Commit 後の Rollback は何もしない

	if _, err := getTask(ctx, tx, taskID, s.now()); err != nil {
		return TimeEntry{}, err
	}
	entry, err := insertEntry(ctx, tx, taskID, start, &end)
	if err != nil {
		return TimeEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return TimeEntry{}, fmt.Errorf("時間記録の追加のコミットに失敗: %w", err)
	}
	return entry, nil
}

// UpdateTimeEntry は終了済みの区間の開始・終了時刻を修正する。計測中の区間は修正できない。
func (s *Service) UpdateTimeEntry(ctx context.Context, id int64, in EntryPatch) (TimeEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TimeEntry{}, fmt.Errorf("トランザクション開始に失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Commit 後の Rollback は何もしない

	e, err := getEntry(ctx, tx, id)
	if err != nil {
		return TimeEntry{}, err
	}
	if e.EndedAt == nil {
		return TimeEntry{}, ErrEntryRunning
	}
	start, end := e.StartedAt, *e.EndedAt
	if in.StartedAt != nil {
		start = in.StartedAt.UTC().Truncate(time.Second)
	}
	if in.EndedAt != nil {
		end = in.EndedAt.UTC().Truncate(time.Second)
	}
	if err := validateEntry(start, end, s.now()); err != nil {
		return TimeEntry{}, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE time_entries SET started_at = ?, ended_at = ? WHERE id = ?`,
		db.FormatTime(start), db.FormatTime(end), id); err != nil {
		return TimeEntry{}, fmt.Errorf("時間記録の更新に失敗: %w", err)
	}
	updated, err := getEntry(ctx, tx, id)
	if err != nil {
		return TimeEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return TimeEntry{}, fmt.Errorf("時間記録の更新のコミットに失敗: %w", err)
	}
	return updated, nil
}

// DeleteTimeEntry は時間記録を削除する。計測中の区間を削除するとタイマーを取り消したことになる。
func (s *Service) DeleteTimeEntry(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM time_entries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("時間記録の削除に失敗: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("削除件数の取得に失敗: %w", err)
	}
	if n == 0 {
		return ErrEntryNotFound
	}
	return nil
}

func validateEntry(start, end, now time.Time) error {
	v := &validator{}
	if start.IsZero() {
		v.add("started_at", CodeRequired, "開始時刻を指定してください")
	}
	if end.IsZero() {
		v.add("ended_at", CodeRequired, "終了時刻を指定してください")
	}
	if !start.IsZero() && !end.IsZero() {
		switch {
		case !end.After(start):
			v.add("ended_at", CodeNotAfterStart, "終了時刻は開始時刻より後にしてください")
		case end.Sub(start) > MaxEntryDuration:
			v.add("ended_at", CodeTooLong, "1つの区間は24時間以内にしてください")
		}
	}
	if end.After(now) {
		v.add("ended_at", CodeInFuture, "未来の時刻は記録できません")
	}
	return v.err()
}

func insertEntry(ctx context.Context, q queryer, taskID int64, start time.Time, end *time.Time) (TimeEntry, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO time_entries (task_id, started_at, ended_at) VALUES (?, ?, ?)`,
		taskID, db.FormatTime(start), nullTime(end))
	if err != nil {
		return TimeEntry{}, fmt.Errorf("時間記録の追加に失敗: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return TimeEntry{}, fmt.Errorf("時間記録の ID 取得に失敗: %w", err)
	}
	return getEntry(ctx, q, id)
}

func getEntry(ctx context.Context, q queryer, id int64) (TimeEntry, error) {
	row := q.QueryRowContext(ctx, `SELECT id, task_id, started_at, ended_at FROM time_entries WHERE id = ?`, id)
	e, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TimeEntry{}, ErrEntryNotFound
	}
	return e, err
}

func scanEntry(sc scanner) (TimeEntry, error) {
	var (
		e     TimeEntry
		start string
		end   sql.NullString
	)
	if err := sc.Scan(&e.ID, &e.TaskID, &start, &end); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TimeEntry{}, err
		}
		return TimeEntry{}, fmt.Errorf("時間記録の読み取りに失敗: %w", err)
	}
	var err error
	if e.StartedAt, err = db.ParseTime(start); err != nil {
		return TimeEntry{}, err
	}
	if end.Valid {
		t, err := db.ParseTime(end.String)
		if err != nil {
			return TimeEntry{}, err
		}
		e.EndedAt = &t
	}
	return e, nil
}
