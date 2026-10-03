package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const now = "2026-10-03T00:00:00Z"

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	// 親ディレクトリの自動作成も確かめるため、存在しないサブディレクトリを指定する。
	path := filepath.Join(t.TempDir(), "nested", "test.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func insertTask(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO tasks (title, created_at, updated_at) VALUES ('t', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("タスクの挿入: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

func TestMigrate_CreatesTables(t *testing.T) {
	db := openTestDB(t)
	for _, table := range []string{"tasks", "time_entries", "reflections"} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("テーブル %s が存在しない: %v", table, err)
		}
	}
}

func TestMigrate_IsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("2回目の Migrate が失敗: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("schema_migrations の件数 = %d, want 1", count)
	}
}

func TestSchema_TaskConstraints(t *testing.T) {
	db := openTestDB(t)
	tests := []struct {
		name string
		sql  string
		args []any
	}{
		{"空のタイトル", `INSERT INTO tasks (title, created_at, updated_at) VALUES ('', ?, ?)`, []any{now, now}},
		{"101文字のタイトル", `INSERT INTO tasks (title, created_at, updated_at) VALUES (?, ?, ?)`,
			[]any{strings.Repeat("あ", 101), now, now}},
		{"不正なステータス", `INSERT INTO tasks (title, status, created_at, updated_at) VALUES ('t', 'archived', ?, ?)`, []any{now, now}},
		{"負の目標時間", `INSERT INTO tasks (title, estimated_min, created_at, updated_at) VALUES ('t', -1, ?, ?)`, []any{now, now}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := db.Exec(tt.sql, tt.args...); err == nil {
				t.Error("制約違反のエラーを期待したが nil")
			}
		})
	}
}

func TestSchema_TitleBoundary(t *testing.T) {
	db := openTestDB(t)
	// length() は文字数で数えるため、マルチバイト文字100文字は許可される。
	_, err := db.Exec(`INSERT INTO tasks (title, created_at, updated_at) VALUES (?, ?, ?)`,
		strings.Repeat("あ", 100), now, now)
	if err != nil {
		t.Errorf("100文字のタイトルが拒否された: %v", err)
	}
}

func TestSchema_OnlyOneRunningTimer(t *testing.T) {
	db := openTestDB(t)
	a, b := insertTask(t, db), insertTask(t, db)

	if _, err := db.Exec(`INSERT INTO time_entries (task_id, started_at) VALUES (?, ?)`, a, now); err != nil {
		t.Fatalf("1つ目のタイマー: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO time_entries (task_id, started_at) VALUES (?, ?)`, b, now); err == nil {
		t.Error("計測中タイマーの2つ目はエラーを期待したが nil")
	}
	// 停止済みの区間は何件でも登録できる。
	for range 2 {
		if _, err := db.Exec(`INSERT INTO time_entries (task_id, started_at, ended_at) VALUES (?, ?, ?)`, b, now, now); err != nil {
			t.Errorf("停止済み区間の登録: %v", err)
		}
	}
}

func TestSchema_TimeEntryEndBeforeStart(t *testing.T) {
	db := openTestDB(t)
	id := insertTask(t, db)
	_, err := db.Exec(`INSERT INTO time_entries (task_id, started_at, ended_at) VALUES (?, ?, ?)`,
		id, "2026-10-03T10:00:00Z", "2026-10-03T09:00:00Z")
	if err == nil {
		t.Error("終了が開始より前の区間はエラーを期待したが nil")
	}
}

func TestSchema_DeleteTaskCascades(t *testing.T) {
	db := openTestDB(t)
	id := insertTask(t, db)
	if _, err := db.Exec(`INSERT INTO time_entries (task_id, started_at, ended_at) VALUES (?, ?, ?)`, id, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO reflections (task_id, body, updated_at) VALUES (?, 'memo', ?)`, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM tasks WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"time_entries", "reflections"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s に %d 件残っている（foreign_keys が無効の可能性）", table, count)
		}
	}
}

// 読み取ってから書き込むトランザクション（task.Service.Update と同じ形）の途中で、
// 別の接続（sqlite3 CLI など別プロセス相当）が書き込んでも失敗しないことを確かめる。
// deferred トランザクションだと、後から書き込もうとした時点で SQLITE_BUSY_SNAPSHOT になり
// busy_timeout も効かない。immediate なら開始時点でロックを取るので、相手が待たされる。
func TestOpen_ReadThenWriteTxSurvivesOtherWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shared.db")
	a, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := Migrate(ctx, a); err != nil {
		t.Fatal(err)
	}
	b, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })

	tx, err := a.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := tx.QueryRow(`SELECT count(*) FROM tasks`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	otherDone := make(chan error, 1)
	go func() {
		_, err := b.Exec(`INSERT INTO tasks (title, created_at, updated_at) VALUES ('other', ?, ?)`, now, now)
		otherDone <- err
	}()
	// 別接続の書き込みが（deferred なら）先に完了してしまう猶予を与える。
	time.Sleep(100 * time.Millisecond)

	if _, err := tx.Exec(`INSERT INTO tasks (title, created_at, updated_at) VALUES ('mine', ?, ?)`, now, now); err != nil {
		t.Fatalf("読み取り後の書き込みが失敗した（deferred の BUSY_SNAPSHOT）: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-otherDone; err != nil {
		t.Fatalf("別接続の書き込みはロック解放後に成功するはず: %v", err)
	}

	var count int
	if err := a.QueryRow(`SELECT count(*) FROM tasks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}
