// Package config は環境変数から insight サービスの設定を読み込む。
package config

import "github.com/kawafuchieirin/task-manager/shared/envconf"

// Config は insight サービスの設定。
type Config struct {
	Addr string
}

// 既定値はループバックのみで待ち受ける（ローカル専用アプリのため外部公開しない）。
const defaultAddr = "127.0.0.1:8081"

// Load は insight の設定を読み込み、検証する。
func Load(getenv envconf.Getenv) (Config, error) {
	cfg := Config{Addr: envconf.ValueOr(getenv, "INSIGHT_ADDR", defaultAddr)}
	if err := envconf.LoopbackAddr("INSIGHT_ADDR", cfg.Addr); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
