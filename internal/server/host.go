package server

import (
	"net"
	"net/http"
	"slices"
	"strings"
)

// LocalHosts は待ち受けアドレス addr でアクセスされうるホスト名の一覧を返す。
// ループバックの別名に加え、127.0.0.2 のような別のループバックアドレスで待ち受けている場合はそれも含める。
// ループバック以外のホストは、Host ヘッダーとして許可しない。
func LocalHosts(addr string) []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return hosts
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() && !slices.Contains(hosts, ip.String()) {
		hosts = append(hosts, ip.String())
	}
	return hosts
}

// RequireHost は Host ヘッダーが allowed のいずれかであるリクエストだけを通す。
// DNS リバインディング（悪意あるサイトのドメインを 127.0.0.1 に向けて、
// ブラウザから同一オリジンとしてローカルのサーバーを操作する攻撃）を防ぐ。
func RequireHost(allowed []string, next http.Handler) http.Handler {
	set := make(map[string]bool, len(allowed))
	for _, h := range allowed {
		set[strings.ToLower(h)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(strings.ToLower(host), "[]")
		if !set[host] {
			http.Error(w, "許可されていないホストです", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}
