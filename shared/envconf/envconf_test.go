package envconf

import "testing"

func TestValueOr(t *testing.T) {
	env := func(m map[string]string) Getenv { return func(k string) string { return m[k] } }
	if got := ValueOr(env(nil), "X", "def"); got != "def" {
		t.Errorf("未設定: %q", got)
	}
	if got := ValueOr(env(map[string]string{"X": "v"}), "X", "def"); got != "v" {
		t.Errorf("設定あり: %q", got)
	}
}

func TestLoopbackAddr(t *testing.T) {
	tests := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:8080", false},
		{"127.0.0.2:8080", false},
		{"[::1]:8080", false},
		{"localhost:8080", false},
		{"LOCALHOST:8080", false},
		// 画面は認証なしなので、ループバック以外に公開すると API キーを迂回できてしまう
		{"0.0.0.0:8080", true},
		{":8080", true},
		{"192.168.1.10:8080", true},
		{"example.com:8080", true},
		{"127.0.0.1", true},
		{"127.0.0.1:http", true},
		{"127.0.0.1:0", true},
		{"127.0.0.1:65536", true},
	}
	for _, tt := range tests {
		err := LoopbackAddr("ADDR", tt.addr)
		if (err != nil) != tt.wantErr {
			t.Errorf("LoopbackAddr(%q) err = %v, wantErr %v", tt.addr, err, tt.wantErr)
		}
	}
}
