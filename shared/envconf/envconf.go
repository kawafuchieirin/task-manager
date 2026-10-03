// Package envconf は各サービスが環境変数から設定を読むときの共通処理を提供する。
package envconf

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Getenv は環境変数の取得関数。テストで差し替えられるよう os.Getenv を直接呼ばない。
type Getenv func(key string) string

// ValueOr は環境変数 key の値を返す。未設定（空文字）なら fallback を返す。
func ValueOr(getenv Getenv, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

// LoopbackAddr は addr がループバックの host:port であることを検証する。
// 画面（web）には認証が無いため、LAN などに公開すると API キーを迂回して操作できてしまう。
// ローカル専用アプリとして、ループバック以外での待ち受けは設定の段階で拒否する。
func LoopbackAddr(key, addr string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s=%q は host:port 形式で指定してください: %w", key, addr, err)
	}
	if !IsLoopbackHost(host) {
		return fmt.Errorf("%s=%q: ローカル専用のため、ホストは 127.0.0.1 / ::1 / localhost のいずれかにしてください", key, addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s=%q のポートは 1〜65535 で指定してください", key, addr)
	}
	return nil
}

// IsLoopbackHost は host が localhost またはループバックの IP アドレスかを返す。
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
