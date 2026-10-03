package config

import "testing"

func TestLoad(t *testing.T) {
	got, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if got.Addr != "127.0.0.1:8081" {
		t.Errorf("Addr = %q, want 127.0.0.1:8081", got.Addr)
	}

	// 詳細なアドレスの検証は shared/envconf のテストで行う
	for _, addr := range []string{"bad", "0.0.0.0:8081"} {
		env := func(string) string { return addr }
		if _, err := Load(env); err == nil {
			t.Errorf("INSIGHT_ADDR=%q でエラーを期待したが nil", addr)
		}
	}
}
