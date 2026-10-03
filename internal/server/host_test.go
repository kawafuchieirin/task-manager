package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestLocalHosts(t *testing.T) {
	loopback := []string{"localhost", "127.0.0.1", "::1"}
	tests := []struct {
		addr string
		want []string
	}{
		{"127.0.0.1:8080", loopback},
		{"[::1]:8080", loopback},
		{"localhost:8080", loopback},
		{"127.0.0.2:8080", append(slices.Clone(loopback), "127.0.0.2")},
		// ループバック以外は Host として許可しない（設定でも拒否するが、二重に防ぐ）
		{"0.0.0.0:8080", loopback},
		{"192.168.1.10:8080", loopback},
	}
	for _, tt := range tests {
		if got := LocalHosts(tt.addr); !slices.Equal(got, tt.want) {
			t.Errorf("LocalHosts(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestRequireHost(t *testing.T) {
	h := RequireHost(LocalHosts("127.0.0.1:8080"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	tests := []struct {
		host string
		want int
	}{
		{"127.0.0.1:8080", http.StatusOK},
		{"localhost:8080", http.StatusOK},
		{"LOCALHOST:8080", http.StatusOK},
		{"[::1]:8080", http.StatusOK},
		{"localhost", http.StatusOK},
		{"evil.example.com:8080", http.StatusMisdirectedRequest},
		{"127.0.0.1.evil.example.com", http.StatusMisdirectedRequest},
		{"", http.StatusMisdirectedRequest},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = tt.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("Host %q: status = %d, want %d", tt.host, rec.Code, tt.want)
		}
	}
}
