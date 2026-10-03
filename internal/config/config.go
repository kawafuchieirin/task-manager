// Package config は環境変数から各サービスの設定を読み込む。
package config

import (
	"fmt"
	"net"
	"strconv"
)

// Getenv は環境変数の取得関数。テストで差し替えられるよう os.Getenv を直接呼ばない。
type Getenv func(key string) string

// Taskboard は taskboard サービスの設定。
type Taskboard struct {
	Addr   string
	DBPath string
}

// Insight は insight サービスの設定。
type Insight struct {
	Addr string
}

// 既定値はループバックのみで待ち受ける（ローカル専用アプリのため外部公開しない）。
const (
	defaultTaskboardAddr = "127.0.0.1:8080"
	defaultInsightAddr   = "127.0.0.1:8081"
	defaultDBPath        = "data/taskboard.db"
)

// LoadTaskboard は taskboard の設定を読み込み、検証する。
func LoadTaskboard(getenv Getenv) (Taskboard, error) {
	cfg := Taskboard{
		Addr:   valueOr(getenv, "TASKBOARD_ADDR", defaultTaskboardAddr),
		DBPath: valueOr(getenv, "TASKBOARD_DB_PATH", defaultDBPath),
	}
	if err := validateAddr("TASKBOARD_ADDR", cfg.Addr); err != nil {
		return Taskboard{}, err
	}
	return cfg, nil
}

// LoadInsight は insight の設定を読み込み、検証する。
func LoadInsight(getenv Getenv) (Insight, error) {
	cfg := Insight{
		Addr: valueOr(getenv, "INSIGHT_ADDR", defaultInsightAddr),
	}
	if err := validateAddr("INSIGHT_ADDR", cfg.Addr); err != nil {
		return Insight{}, err
	}
	return cfg, nil
}

func valueOr(getenv Getenv, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

func validateAddr(key, addr string) error {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s=%q は host:port 形式で指定してください: %w", key, addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s=%q のポートは 1〜65535 で指定してください", key, addr)
	}
	return nil
}
