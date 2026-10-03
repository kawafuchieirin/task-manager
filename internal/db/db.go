// Package db は SQLite への接続とスキーマのマイグレーションを扱う。
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql に "sqlite" ドライバを登録する
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Open は SQLite ファイルを開く。親ディレクトリが無ければ作成する。
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("DB ディレクトリの作成に失敗: %w", err)
	}

	// PRAGMA は接続単位の設定なので DSN で指定し、プール内の全接続に適用させる。
	dsn := "file:" + path +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("DB のオープンに失敗: %w", err)
	}
	// SQLite は書き込みが直列なので、シングルユーザー用途では接続を1本に絞り SQLITE_BUSY を避ける。
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("DB への接続確認に失敗: %w", err)
	}
	return db, nil
}

// Migrate は未適用のマイグレーションをファイル名順に適用する。
// 各ファイルは1トランザクションで適用し、途中で失敗したファイルは記録しない。
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("schema_migrations の作成に失敗: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("マイグレーションファイルの列挙に失敗: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(filepath.Base(file), ".sql")
		if applied[version] {
			continue
		}
		if err := applyMigration(ctx, db, file, version); err != nil {
			return err
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("適用済みマイグレーションの取得に失敗: %w", err)
	}
	defer func() { _ = rows.Close() }() // 読み取りエラーは rows.Err で検査する

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("適用済みマイグレーションの読み取りに失敗: %w", err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func applyMigration(ctx context.Context, db *sql.DB, file, version string) error {
	script, err := migrationFS.ReadFile(file)
	if err != nil {
		return fmt.Errorf("%s の読み込みに失敗: %w", file, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s のトランザクション開始に失敗: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }() // Commit 後の Rollback は何もしない

	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		return fmt.Errorf("%s の適用に失敗: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("%s の適用記録に失敗: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s のコミットに失敗: %w", version, err)
	}
	return nil
}
