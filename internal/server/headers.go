package server

import "net/http"

// SecurityHeaders はブラウザ向けの防御ヘッダーを全レスポンスに付ける。
//   - frame-ancestors / X-Frame-Options: 他サイトの iframe に埋め込んで操作させるクリックジャッキングを防ぐ。
//     iframe 内からの送信は同一オリジン扱いになり、CSRF 対策では防げないため。
//   - nosniff: Content-Type と異なる種類として解釈されるのを防ぐ。
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
