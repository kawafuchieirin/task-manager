package config

import (
	"slices"
	"testing"
)

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
			want: Config{Addr: "127.0.0.1:8080", DBPath: "data/taskboard.db", InsightURL: "http://127.0.0.1:8081"},
		},
		{
			name: "環境変数で上書きできる",
			env:  map[string]string{"API_ADDR": "localhost:9000", "API_DB_PATH": "/tmp/x.db", "API_INSIGHT_URL": "http://localhost:9001"},
			want: Config{Addr: "localhost:9000", DBPath: "/tmp/x.db", InsightURL: "http://localhost:9001"},
		},
		{name: "insight の URL にスキームなし", env: map[string]string{"API_INSIGHT_URL": "127.0.0.1:8081"}, wantErr: true},
		// 詳細なアドレスの検証は shared/envconf のテストで行う
		{name: "ループバック以外は拒否", env: map[string]string{"API_ADDR": "0.0.0.0:8080"}, wantErr: true},
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
			if got.Addr != tt.want.Addr || got.DBPath != tt.want.DBPath || got.InsightURL != tt.want.InsightURL {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoad_APIKey(t *testing.T) {
	got, err := Load(envFrom(map[string]string{"API_KEY": "0123456789abcdef"}))
	if err != nil {
		t.Fatalf("16文字のキーが拒否された: %v", err)
	}
	if got.APIKey != "0123456789abcdef" {
		t.Errorf("APIKey = %q", got.APIKey)
	}

	if _, err := Load(envFrom(map[string]string{"API_KEY": "short"})); err == nil {
		t.Error("短すぎるキーでエラーを期待したが nil")
	}
}

func TestLoad_CORSOrigins(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "未設定", raw: "", want: nil},
		{name: "複数指定・空白と末尾スラッシュは正規化", raw: " http://localhost:3000/ , https://app.example.com",
			want: []string{"http://localhost:3000", "https://app.example.com"}},
		{name: "空要素は無視", raw: "http://localhost:3000,,", want: []string{"http://localhost:3000"}},
		{name: "ホストとスキームは小文字に揃える", raw: "HTTP://LocalHost:3000", want: []string{"http://localhost:3000"}},
		{name: "既定ポートは省略する", raw: "http://localhost:80,https://app.example.com:443",
			want: []string{"http://localhost", "https://app.example.com"}},
		{name: "IPv6 の既定ポート", raw: "http://[::1]:80", want: []string{"http://[::1]"}},
		{name: "スキームなし", raw: "localhost:3000", wantErr: true},
		{name: "http(s) 以外", raw: "ftp://example.com", wantErr: true},
		{name: "パス付き", raw: "http://example.com/app", wantErr: true},
		{name: "ワイルドカード", raw: "*", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(envFrom(map[string]string{"API_CORS_ORIGINS": tt.raw}))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil: %v", got.CORSOrigins)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if !slices.Equal(got.CORSOrigins, tt.want) {
				t.Errorf("CORSOrigins = %v, want %v", got.CORSOrigins, tt.want)
			}
		})
	}
}
