package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireAPIKey は apiKey が設定されているとき、Authorization: Bearer <apiKey> を要求する。
// apiKey が空なら認証しない（ループバックでのみ待ち受ける前提）。
func requireAPIKey(apiKey string, next http.Handler) http.Handler {
	if apiKey == "" {
		return next
	}
	want := []byte(apiKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		// タイミング攻撃でキーを推測されないよう、定数時間で比較する。
		if !ok || subtle.ConstantTimeCompare([]byte(token), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="taskboard"`)
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "API キーが無効です", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cors は許可したオリジンのブラウザアプリから API を呼べるようにする。
// 認証より外側に置き、プリフライト（認証ヘッダーを送らない）と 401 応答にも CORS ヘッダーを付ける。
func cors(origins []string, next http.Handler) http.Handler {
	if len(origins) == 0 {
		return next
	}
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if !allowed[origin] {
			next.ServeHTTP(w, r)
			return
		}
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Expose-Headers", "Location")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
