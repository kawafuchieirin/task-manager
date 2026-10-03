package config

import "testing"

func envFrom(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "未設定なら既定値を使う",
			env:  map[string]string{},
			want: Config{Addr: "127.0.0.1:3000", APIURL: "http://127.0.0.1:8080"},
		},
		{
			name: "環境変数で上書きできる",
			env:  map[string]string{"WEB_ADDR": "localhost:3001", "WEB_API_URL": "http://localhost:9000", "WEB_API_KEY": "k"},
			want: Config{Addr: "localhost:3001", APIURL: "http://localhost:9000", APIKey: "k"},
		},
		// 詳細なアドレスの検証は shared/envconf のテストで行う
		{name: "ループバック以外は拒否", env: map[string]string{"WEB_ADDR": "0.0.0.0:3000"}, wantErr: true},
		{name: "API の URL にスキームなし", env: map[string]string{"WEB_API_URL": "127.0.0.1:8080"}, wantErr: true},
		{name: "API の URL が http(s) 以外", env: map[string]string{"WEB_API_URL": "ftp://127.0.0.1"}, wantErr: true},
		{name: "API の URL に認証情報", env: map[string]string{"WEB_API_URL": "http://u:p@127.0.0.1:8080"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(envFrom(tt.env))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
