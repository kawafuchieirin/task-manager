package extract

import (
	"context"
	"slices"
	"testing"
)

func TestRuleBased(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		learned    []string
		notLearned []string
	}{
		{
			name:       "キーワードで分類（SPEC の例）",
			text:       "goroutine の使い方を理解した。テストの書き方はまだ曖昧。",
			learned:    []string{"goroutine の使い方を理解した"},
			notLearned: []string{"テストの書き方はまだ曖昧"},
		},
		{
			name:       "+ / - の行（全角も可）",
			text:       "+ channel の閉じ方\n- select の使い方\n＋ context のキャンセル\n－ race の調べ方",
			learned:    []string{"channel の閉じ方", "context のキャンセル"},
			notLearned: []string{"select の使い方", "race の調べ方"},
		},
		{
			name:       "見出しの下の行（箇条書きの記号は除く。見出しの下では - も記号として扱う）",
			text:       "## 学んだこと：\n- htmx の hx-swap-oob\n・ embed の使い方\n\n【課題】\n* テストの並列化\nCSS のレイアウト",
			learned:    []string{"htmx の hx-swap-oob", "embed の使い方"},
			notLearned: []string{"テストの並列化", "CSS のレイアウト"},
		},
		{
			name:       "否定を先に判定する（理解できなかった は できなかったこと）",
			text:       "インターフェースは理解できなかった。ポインタは理解できた。",
			learned:    []string{"ポインタは理解できた"},
			notLearned: []string{"インターフェースは理解できなかった"},
		},
		{
			name:    "どちらにも当たらない文は捨てる",
			text:    "今日は雨だった。SQL の JOIN がわかった！",
			learned: []string{"SQL の JOIN がわかった！"},
		},
		{
			name:    "重複は1つにまとめる・CRLF・空行",
			text:    "+ embed\r\n\r\n+ embed。\r\n",
			learned: []string{"embed"},
		},
		{name: "空", text: ""},
		{name: "空白だけ", text: "  \n　\n"},
		{
			name:       "見出しは英語・大文字小文字を問わない",
			text:       "Good:\nmise の使い方\nBAD\nlint の設定",
			learned:    []string{"mise の使い方"},
			notLearned: []string{"lint の設定"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RuleBased{}.Extract(context.Background(), tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.Learned, tt.learned) {
				t.Errorf("Learned = %q, want %q", got.Learned, tt.learned)
			}
			if !slices.Equal(got.NotLearned, tt.notLearned) {
				t.Errorf("NotLearned = %q, want %q", got.NotLearned, tt.notLearned)
			}
		})
	}
}

func TestRuleBased_ImplementsExtractor(t *testing.T) {
	var _ Extractor = RuleBased{}
}
