// Package config は環境変数から各サービスの設定を読み込む。
package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Getenv は環境変数の取得関数。テストで差し替えられるよう os.Getenv を直接呼ばない。
type Getenv func(key string) string

// Taskboard は taskboard サービスの設定。
type Taskboard struct {
	Addr   string
	DBPath string
	// APIKey が空でなければ、/api/v1 へのリクエストに Bearer トークンとして要求する。
	APIKey string
	// CORSOrigins はブラウザから API を呼べる他のアプリのオリジン（例: http://localhost:3000）。
	CORSOrigins []string
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

	// minAPIKeyLen は推測されにくいキーを強制するための最小長。
	minAPIKeyLen = 16
)

// LoadTaskboard は taskboard の設定を読み込み、検証する。
func LoadTaskboard(getenv Getenv) (Taskboard, error) {
	cfg := Taskboard{
		Addr:   valueOr(getenv, "TASKBOARD_ADDR", defaultTaskboardAddr),
		DBPath: valueOr(getenv, "TASKBOARD_DB_PATH", defaultDBPath),
		APIKey: getenv("TASKBOARD_API_KEY"),
	}
	if err := validateAddr("TASKBOARD_ADDR", cfg.Addr); err != nil {
		return Taskboard{}, err
	}
	if cfg.APIKey != "" && len(cfg.APIKey) < minAPIKeyLen {
		return Taskboard{}, fmt.Errorf("TASKBOARD_API_KEY は%d文字以上にしてください", minAPIKeyLen)
	}
	origins, err := parseOrigins("TASKBOARD_CORS_ORIGINS", getenv("TASKBOARD_CORS_ORIGINS"))
	if err != nil {
		return Taskboard{}, err
	}
	cfg.CORSOrigins = origins
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

// validateAddr は addr がループバックの host:port であることを検証する。
// 画面には認証が無いため、LAN などに公開すると API キーを迂回して操作できてしまう。
// ローカル専用アプリとして、ループバック以外での待ち受けは設定の段階で拒否する。
func validateAddr(key, addr string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s=%q は host:port 形式で指定してください: %w", key, addr, err)
	}
	if !isLoopback(host) {
		return fmt.Errorf("%s=%q: ローカル専用のため、ホストは 127.0.0.1 / ::1 / localhost のいずれかにしてください", key, addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s=%q のポートは 1〜65535 で指定してください", key, addr)
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// parseOrigins はカンマ区切りのオリジン一覧を検証し、scheme://host[:port] の形に揃えて返す。
func parseOrigins(key, raw string) ([]string, error) {
	var origins []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		u, err := url.Parse(item)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, fmt.Errorf("%s の %q は http(s)://host[:port] 形式で指定してください", key, item)
		}
		origins = append(origins, u.Scheme+"://"+u.Host)
	}
	return origins, nil
}
