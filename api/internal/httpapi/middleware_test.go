package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testKey = "0123456789abcdef"

func TestRequireAPIKey(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"ヘッダーなし", "", http.StatusUnauthorized},
		{"キー違い", "Bearer wrong-key-wrong-key", http.StatusUnauthorized},
		{"Bearer なし", testKey, http.StatusUnauthorized},
		{"Bearer のみ", "Bearer ", http.StatusUnauthorized},
		{"別スキーム", "Basic " + testKey, http.StatusUnauthorized},
		{"小文字の bearer も許可", "bearer " + testKey, http.StatusOK},
		{"正しいキー", "Bearer " + testKey, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t, Options{APIKey: testKey})
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if tt.want == http.StatusUnauthorized {
				if rec.Header().Get("WWW-Authenticate") == "" {
					t.Error("401 には WWW-Authenticate を付けるはず")
				}
				assertErrorCode(t, rec, http.StatusUnauthorized, codeUnauthorized)
			}
		})
	}
}

func TestRequireAPIKey_DisabledWhenEmpty(t *testing.T) {
	h := newTestHandler(t, Options{})
	assertStatus(t, do(t, h, http.MethodGet, "/api/v1/tasks", ""), http.StatusOK)
}

func TestCORS(t *testing.T) {
	const allowed = "http://localhost:3000"
	h := newTestHandler(t, Options{APIKey: testKey, CORSOrigins: []string{allowed}})

	t.Run("許可オリジンのプリフライトは認証なしで 204", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/tasks", nil)
		req.Header.Set("Origin", allowed)
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assertStatus(t, rec, http.StatusNoContent)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != allowed {
			t.Errorf("Allow-Origin = %q", got)
		}
		if rec.Header().Get("Access-Control-Allow-Headers") == "" || rec.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("Allow-Headers / Allow-Methods がない")
		}
	})

	t.Run("許可オリジンの 401 にも CORS ヘッダーを付ける", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
		req.Header.Set("Origin", allowed)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assertStatus(t, rec, http.StatusUnauthorized)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != allowed {
			t.Errorf("Allow-Origin = %q", got)
		}
	})

	t.Run("許可していないオリジンには CORS ヘッダーを付けない", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/tasks", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		req.Header.Set("Access-Control-Request-Method", "DELETE")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q, want 空", got)
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Error("キャッシュ汚染を防ぐため Vary: Origin を付けるはず")
		}
	})
}
