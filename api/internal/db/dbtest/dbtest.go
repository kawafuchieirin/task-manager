// Package dbtest はテスト用に、マイグレーション適用済みの一時 SQLite DB を用意する。
package dbtest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/kawafuchieirin/task-manager/api/internal/db"
)

// New はテストごとに独立した DB を返す。テスト終了時に自動でクローズされる。
func New(t testing.TB) *sql.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("テスト DB のオープン: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("テスト DB のマイグレーション: %v", err)
	}
	return database
}
