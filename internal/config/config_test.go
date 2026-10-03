package config

import (
	"slices"
	"testing"
)

func envFrom(m map[string]string) Getenv {
	return func(key string) string { return m[key] }
}

func TestLoadTaskboard(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Taskboard
		wantErr bool
	}{
		{
			name: "未設定なら既定値を使う",
			env:  map[string]string{},
			want: Taskboard{Addr: "127.0.0.1:8080", DBPath: "data/taskboard.db"},
		},
		{
			name: "環境変数で上書きできる",
			env:  map[string]string{"TASKBOARD_ADDR": "127.0.0.1:9000", "TASKBOARD_DB_PATH": "/tmp/x.db"},
			want: Taskboard{Addr: "127.0.0.1:9000", DBPath: "/tmp/x.db"},
		},
		{name: "ポートなしはエラー", env: map[string]string{"TASKBOARD_ADDR": "127.0.0.1"}, wantErr: true},
		{name: "ポートが数値でないとエラー", env: map[string]string{"TASKBOARD_ADDR": "127.0.0.1:http"}, wantErr: true},
		{name: "ポート0はエラー", env: map[string]string{"TASKBOARD_ADDR": "127.0.0.1:0"}, wantErr: true},
		{name: "ポート範囲外はエラー", env: map[string]string{"TASKBOARD_ADDR": "127.0.0.1:65536"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadTaskboard(envFrom(tt.env))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got.Addr != tt.want.Addr || got.DBPath != tt.want.DBPath {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadTaskboard_APIKey(t *testing.T) {
	got, err := LoadTaskboard(envFrom(map[string]string{"TASKBOARD_API_KEY": "0123456789abcdef"}))
	if err != nil {
		t.Fatalf("16文字のキーが拒否された: %v", err)
	}
	if got.APIKey != "0123456789abcdef" {
		t.Errorf("APIKey = %q", got.APIKey)
	}

	if _, err := LoadTaskboard(envFrom(map[string]string{"TASKBOARD_API_KEY": "short"})); err == nil {
		t.Error("短すぎるキーでエラーを期待したが nil")
	}
}

func TestLoadTaskboard_CORSOrigins(t *testing.T) {
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
		{name: "スキームなし", raw: "localhost:3000", wantErr: true},
		{name: "http(s) 以外", raw: "ftp://example.com", wantErr: true},
		{name: "パス付き", raw: "http://example.com/app", wantErr: true},
		{name: "ワイルドカード", raw: "*", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadTaskboard(envFrom(map[string]string{"TASKBOARD_CORS_ORIGINS": tt.raw}))
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

func TestLoadInsight(t *testing.T) {
	got, err := LoadInsight(envFrom(map[string]string{}))
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if got.Addr != "127.0.0.1:8081" {
		t.Errorf("Addr = %q, want 127.0.0.1:8081", got.Addr)
	}

	if _, err := LoadInsight(envFrom(map[string]string{"INSIGHT_ADDR": "bad"})); err == nil {
		t.Error("不正なアドレスでエラーを期待したが nil")
	}
}
