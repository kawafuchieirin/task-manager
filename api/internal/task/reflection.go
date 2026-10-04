package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kawafuchieirin/task-manager/api/internal/db"
)

// MaxReflectionLen は振り返りメモの上限（文字数）。insight の抽出テキストの上限と揃える。
const MaxReflectionLen = 5000

// ExtractStatus は抽出の状態。
type ExtractStatus string

// 抽出の状態。insight に接続できなくても振り返りは保存し、failed として後からやり直せるようにする。
const (
	ExtractPending ExtractStatus = "pending"
	ExtractOK      ExtractStatus = "ok"
	ExtractFailed  ExtractStatus = "failed"
)

// Reflection はタスクの振り返り（1タスクに1つ）。
type Reflection struct {
	TaskID     int64
	Body       string
	Learned    []string // 抽出に成功するまでは空
	NotLearned []string
	Status     ExtractStatus
	UpdatedAt  time.Time
}

// ReflectionEntry は振り返りの一覧の1件（タスクの名前とステータスを含む）。
type ReflectionEntry struct {
	Reflection
	TaskTitle  string
	TaskStatus Status
}

// Extractor は振り返りのテキストから「学んだこと」と「できなかったこと」を抽出する（insight サービス）。
type Extractor interface {
	Extract(ctx context.Context, text string) (learned, notLearned []string, err error)
}

// ErrReflectionNotFound はタスクに振り返りが無いことを表す。
var ErrReflectionNotFound = errors.New("振り返りが見つかりません")

// WithExtractor は抽出に使う Extractor を設定する。未設定なら抽出は常に失敗（failed）になる。
func WithExtractor(e Extractor) Option {
	return func(s *Service) { s.extractor = e }
}

// SaveReflection は振り返りメモを保存し、抽出する。
// 抽出に失敗しても保存は成功させ、状態を failed にする（ExtractReflection でやり直せる）。
func (s *Service) SaveReflection(ctx context.Context, taskID int64, body string) (Reflection, error) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	v := &validator{}
	switch n := utf8.RuneCountInString(body); {
	case strings.TrimSpace(body) == "":
		v.add("body", CodeRequired, "振り返りを入力してください")
	case n > MaxReflectionLen:
		v.add("body", CodeTooLong, fmt.Sprintf("振り返りは%d文字以内で入力してください", MaxReflectionLen))
	}
	if err := v.err(); err != nil {
		return Reflection{}, err
	}

	if _, err := s.Get(ctx, taskID); err != nil {
		return Reflection{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO reflections (task_id, body, learned_json, not_learned_json, extract_status, updated_at)
		 VALUES (?, ?, NULL, NULL, 'pending', ?)
		 ON CONFLICT (task_id) DO UPDATE SET
		   body = excluded.body, learned_json = NULL, not_learned_json = NULL,
		   extract_status = 'pending', updated_at = excluded.updated_at`,
		taskID, body, db.FormatTime(s.now()),
	); err != nil {
		return Reflection{}, fmt.Errorf("振り返りの保存に失敗: %w", err)
	}
	return s.extract(ctx, taskID, body)
}

// ExtractReflection は保存済みの振り返りをもう一度抽出する（insight が止まっていて失敗したときなど）。
func (s *Service) ExtractReflection(ctx context.Context, taskID int64) (Reflection, error) {
	r, err := s.GetReflection(ctx, taskID)
	if err != nil {
		return Reflection{}, err
	}
	return s.extract(ctx, taskID, r.Body)
}

// extract は insight で抽出し、結果を保存する。
// 抽出はネットワーク越しで時間がかかりうるため、DB のトランザクションの外で行う（その間ロックを持たない）。
// 抽出中に振り返りが書き換えられていたら、古い本文の結果で上書きしないよう保存しない。
func (s *Service) extract(ctx context.Context, taskID int64, body string) (Reflection, error) {
	status := ExtractFailed
	var learned, notLearned []string
	if s.extractor != nil {
		var err error
		learned, notLearned, err = s.extractor.Extract(ctx, body)
		if err == nil {
			status = ExtractOK
		}
	}

	learnedJSON, notLearnedJSON := sql.NullString{}, sql.NullString{}
	if status == ExtractOK {
		learnedJSON = sql.NullString{String: mustJSON(learned), Valid: true}
		notLearnedJSON = sql.NullString{String: mustJSON(notLearned), Valid: true}
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE reflections SET learned_json = ?, not_learned_json = ?, extract_status = ?, updated_at = ?
		 WHERE task_id = ? AND body = ?`,
		learnedJSON, notLearnedJSON, string(status), db.FormatTime(s.now()), taskID, body,
	); err != nil {
		return Reflection{}, fmt.Errorf("抽出結果の保存に失敗: %w", err)
	}
	return s.GetReflection(ctx, taskID)
}

// GetReflection はタスクの振り返りを取得する。
func (s *Service) GetReflection(ctx context.Context, taskID int64) (Reflection, error) {
	if _, err := s.Get(ctx, taskID); err != nil {
		return Reflection{}, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT task_id, body, learned_json, not_learned_json, extract_status, updated_at FROM reflections WHERE task_id = ?`, taskID)
	r, err := scanReflection(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Reflection{}, ErrReflectionNotFound
	}
	return r, err
}

// DeleteReflection はタスクの振り返りを削除する。
func (s *Service) DeleteReflection(ctx context.Context, taskID int64) error {
	if _, err := s.Get(ctx, taskID); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM reflections WHERE task_id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("振り返りの削除に失敗: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("削除件数の取得に失敗: %w", err)
	} else if n == 0 {
		return ErrReflectionNotFound
	}
	return nil
}

// ListReflections は期間内（更新日時が from 以上 to 未満）の振り返りを新しい順に返す。
// from / to が nil ならその側の制限なし。
func (s *Service) ListReflections(ctx context.Context, from, to *time.Time) ([]ReflectionEntry, error) {
	query := `SELECT r.task_id, r.body, r.learned_json, r.not_learned_json, r.extract_status, r.updated_at, t.title, t.status
		FROM reflections r JOIN tasks t ON t.id = r.task_id WHERE 1 = 1`
	var args []any
	if from != nil {
		query += ` AND r.updated_at >= ?`
		args = append(args, db.FormatTime(*from))
	}
	if to != nil {
		query += ` AND r.updated_at < ?`
		args = append(args, db.FormatTime(*to))
	}
	query += ` ORDER BY r.updated_at DESC, r.task_id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("振り返りの一覧の取得に失敗: %w", err)
	}
	defer func() { _ = rows.Close() }() // 読み取りエラーは rows.Err で検査する

	entries := []ReflectionEntry{}
	for rows.Next() {
		var (
			e      ReflectionEntry
			status string
		)
		r, err := scanReflectionWith(rows, &e.TaskTitle, &status)
		if err != nil {
			return nil, err
		}
		e.Reflection, e.TaskStatus = r, Status(status)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("振り返りの一覧の読み取りに失敗: %w", err)
	}
	return entries, nil
}

func scanReflection(sc scanner) (Reflection, error) {
	return scanReflectionWith(sc)
}

// scanReflectionWith は振り返りの列に続けて、extra の列も読み取る。
func scanReflectionWith(sc scanner, extra ...any) (Reflection, error) {
	var (
		r                 Reflection
		learned, notLearn sql.NullString
		status, updated   string
	)
	dest := append([]any{&r.TaskID, &r.Body, &learned, &notLearn, &status, &updated}, extra...)
	if err := sc.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Reflection{}, err
		}
		return Reflection{}, fmt.Errorf("振り返りの読み取りに失敗: %w", err)
	}
	return fillReflection(r, learned, notLearn, status, updated)
}

func fillReflection(r Reflection, learned, notLearned sql.NullString, status, updated string) (Reflection, error) {
	r.Status = ExtractStatus(status)
	var err error
	if r.Learned, err = parseList(learned); err != nil {
		return Reflection{}, err
	}
	if r.NotLearned, err = parseList(notLearned); err != nil {
		return Reflection{}, err
	}
	if r.UpdatedAt, err = db.ParseTime(updated); err != nil {
		return Reflection{}, err
	}
	return r, nil
}

func parseList(s sql.NullString) ([]string, error) {
	list := []string{}
	if !s.Valid {
		return list, nil
	}
	if err := json.Unmarshal([]byte(s.String), &list); err != nil {
		return nil, fmt.Errorf("抽出結果の読み取りに失敗: %w", err)
	}
	return list, nil
}

func mustJSON(list []string) string {
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		panic(fmt.Sprintf("文字列の配列は必ず JSON にできる: %v", err)) // 起こりえない
	}
	return string(b)
}
