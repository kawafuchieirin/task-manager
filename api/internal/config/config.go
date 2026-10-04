// Package config は環境変数から API サーバーの設定を読み込む。
package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/kawafuchieirin/task-manager/shared/envconf"
)

// Config は API サーバーの設定。
type Config struct {
	Addr   string
	DBPath string
	// APIKey が空でなければ、/api/v1 へのリクエストに Bearer トークンとして要求する。
	APIKey string
	// CORSOrigins はブラウザから API を直接呼べる他のアプリのオリジン（例: http://localhost:5173）。
	// 画面アプリ（web）はサーバー間で呼ぶため、ここに登録する必要はない。
	CORSOrigins []string
	// InsightURL は振り返りの抽出を頼む insight サービスの URL。
	InsightURL string
}

// 既定値はループバックのみで待ち受ける（ローカル専用アプリのため外部公開しない）。
const (
	defaultAddr       = "127.0.0.1:8080"
	defaultDBPath     = "data/taskboard.db"
	defaultInsightURL = "http://127.0.0.1:8081"

	// minAPIKeyLen は推測されにくいキーを強制するための最小長。
	minAPIKeyLen = 16
)

// Load は API サーバーの設定を読み込み、検証する。
func Load(getenv envconf.Getenv) (Config, error) {
	cfg := Config{
		Addr:       envconf.ValueOr(getenv, "API_ADDR", defaultAddr),
		DBPath:     envconf.ValueOr(getenv, "API_DB_PATH", defaultDBPath),
		APIKey:     getenv("API_KEY"),
		InsightURL: envconf.ValueOr(getenv, "API_INSIGHT_URL", defaultInsightURL),
	}
	if err := envconf.LoopbackAddr("API_ADDR", cfg.Addr); err != nil {
		return Config{}, err
	}
	if cfg.APIKey != "" && len(cfg.APIKey) < minAPIKeyLen {
		return Config{}, fmt.Errorf("API_KEY は%d文字以上にしてください", minAPIKeyLen)
	}
	origins, err := parseOrigins("API_CORS_ORIGINS", getenv("API_CORS_ORIGINS"))
	if err != nil {
		return Config{}, err
	}
	cfg.CORSOrigins = origins
	if u, err := url.Parse(cfg.InsightURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return Config{}, fmt.Errorf("API_INSIGHT_URL=%q は http(s)://host[:port] 形式で指定してください", cfg.InsightURL)
	}
	return cfg, nil
}

// parseOrigins はカンマ区切りのオリジン一覧を検証し、ブラウザが送る Origin ヘッダーと同じ形
// （小文字の scheme://host、既定ポートは省略）に揃えて返す。揃えないと一致せず CORS が黙って効かない。
func parseOrigins(key, raw string) ([]string, error) {
	var origins []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		u, err := url.Parse(item) // スキームは url.Parse が小文字にする
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, fmt.Errorf("%s の %q は http(s)://host[:port] 形式で指定してください", key, item)
		}
		host := strings.ToLower(u.Host)
		if port := u.Port(); (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
			host = strings.ToLower(u.Hostname())
			if strings.Contains(host, ":") { // IPv6 は角括弧を戻す
				host = "[" + host + "]"
			}
		}
		origins = append(origins, u.Scheme+"://"+host)
	}
	return origins, nil
}
