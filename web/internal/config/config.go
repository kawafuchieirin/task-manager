// Package config は環境変数から画面アプリ（web）の設定を読み込む。
package config

import (
	"fmt"
	"net/url"

	"github.com/kawafuchieirin/task-manager/shared/envconf"
)

// Config は画面アプリの設定。
type Config struct {
	Addr string
	// APIURL は呼び出す API サーバーのベース URL（例: http://127.0.0.1:8080）。
	APIURL string
	// APIKey は API サーバーに API_KEY を設定した場合に、同じ値を指定する。
	APIKey string
}

// 既定値はループバックのみで待ち受ける（ローカル専用アプリのため外部公開しない）。
const (
	defaultAddr   = "127.0.0.1:3000"
	defaultAPIURL = "http://127.0.0.1:8080"
)

// Load は画面アプリの設定を読み込み、検証する。
func Load(getenv envconf.Getenv) (Config, error) {
	cfg := Config{
		Addr:   envconf.ValueOr(getenv, "WEB_ADDR", defaultAddr),
		APIURL: envconf.ValueOr(getenv, "WEB_API_URL", defaultAPIURL),
		APIKey: getenv("WEB_API_KEY"),
	}
	if err := envconf.LoopbackAddr("WEB_ADDR", cfg.Addr); err != nil {
		return Config{}, err
	}
	u, err := url.Parse(cfg.APIURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return Config{}, fmt.Errorf("WEB_API_URL=%q は http(s)://host[:port] 形式で指定してください", cfg.APIURL)
	}
	return cfg, nil
}
