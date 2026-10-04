package board

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/client/taskclient"
)

// flashOOB はレスポンスに含まれる、メッセージウィンドウの out-of-band 差し替えを取り出す。
func flashOOB(body string) string {
	const open = `<div id="flash" class="flash flash--info" role="status" title="クリックで とじる" hx-swap-oob="true">`
	i := strings.Index(body, open)
	if i < 0 {
		return ""
	}
	rest := body[i+len(open):]
	return rest[:strings.Index(rest, "</div>")]
}

func TestNotices(t *testing.T) {
	h, api := newTestHandler(t)

	tests := []struct {
		name   string
		method string
		path   string
		form   url.Values
		want   string
	}{
		{"追加", http.MethodPost, "/tasks", url.Values{"title": {"スライム"}}, "「スライム」が あらわれた！"},
		{"とりかかる", http.MethodPost, "/tasks/1/status", url.Values{"status": {"doing"}, "from": {"todo"}}, "「スライム」に とりかかった！"},
		{"タイマー開始", http.MethodPost, "/tasks/1/timer/start", url.Values{}, "「スライム」との たたかいが はじまった！"},
		{"タイマー停止", http.MethodPost, "/tasks/1/timer/stop", url.Values{}, "「スライム」との たたかいを おえた。"},
		{"クリア", http.MethodPost, "/tasks/1/status", url.Values{"status": {"done"}, "from": {"doing"}}, "「スライム」を やっつけた！ ふりかえりを かいておこう。"},
		{"やりなおす", http.MethodPost, "/tasks/1/status", url.Values{"status": {"doing"}, "from": {"done"}}, "「スライム」が ふたたび あらわれた！"},
		{"すてる", http.MethodDelete, "/tasks/1", nil, "タスクを すてた。"},
	}
	for _, tt := range tests {
		rec := send(t, h, tt.method, tt.path, tt.form)
		assertStatus(t, rec, http.StatusOK)
		if got := flashOOB(rec.Body.String()); got != tt.want {
			t.Errorf("%s: メッセージ = %q, want %q", tt.name, got, tt.want)
		}
	}
	_ = api
}

func TestNotices_NotShownForQuietActions(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("t", taskclient.StatusDoing, nil)

	// 進行中から未着手に戻す・パネルを開く・ボードを読み直すときは、メッセージを出さない。
	for _, rec := range []string{
		send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"todo"}, "from": {"doing"}}).Body.String(),
		send(t, h, http.MethodGet, "/tasks/1/time", nil).Body.String(),
		send(t, h, http.MethodGet, "/board", nil).Body.String(),
	} {
		if strings.Contains(rec, "hx-swap-oob") {
			t.Error("メッセージを出さない操作で out-of-band の差し替えが含まれている")
		}
	}
	// ページ全体（index）には #flash が既にあるので、差し替え用の要素を重ねて出さない。
	if strings.Count(send(t, h, http.MethodGet, "/", nil).Body.String(), `id="flash"`) != 1 {
		t.Error("ページ全体に #flash が1つだけあるはず")
	}
}

func TestNotices_EscapeTitle(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := send(t, h, http.MethodPost, "/tasks", url.Values{"title": {"<script>x</script>"}})
	if got := flashOOB(rec.Body.String()); got != "「&lt;script&gt;x&lt;/script&gt;」が あらわれた！" {
		t.Errorf("メッセージのタスク名もエスケープするはず: %q", got)
	}
}

func TestFieldMessage(t *testing.T) {
	tests := []struct {
		fe   taskclient.FieldError
		want string
	}{
		{taskclient.FieldError{Field: "title", Code: "required"}, "タスクの なまえを いれてください。"},
		{taskclient.FieldError{Field: "title", Code: "too_long"}, "なまえは 100もじ いないに してください。"},
		{taskclient.FieldError{Field: "description", Code: "too_long"}, "せつめいは 2000もじ いないに してください。"},
		{taskclient.FieldError{Field: "estimated_min", Code: "out_of_range"}, "もくひょうは 0〜10080ふん で いれてください。"},
		{taskclient.FieldError{Field: "ended_at", Code: "in_future"}, "みらいの じかんは きろく できない。"},
		// 知らない組み合わせは API の文言をそのまま使う（API にコードが増えても表示が空にならない）
		{taskclient.FieldError{Field: "title", Code: "new_rule", Message: "API の文言"}, "API の文言"},
		{taskclient.FieldError{Field: "x", Code: "y"}, "いれた ないようが おかしい ようだ。"},
	}
	for _, tt := range tests {
		if got := fieldMessage(tt.fe); got != tt.want {
			t.Errorf("fieldMessage(%+v) = %q, want %q", tt.fe, got, tt.want)
		}
	}
}

func TestUserMessage_Conflicts(t *testing.T) {
	tests := map[string]string{
		"timer_not_running": "この タスクの タイマーは うごいていない。",
		"task_completed":    "クリアした タスクの タイマーは うごかせない。",
		"entry_running":     "けいそくちゅうの きろくは なおせない。 さきに ていし してください。",
		"unknown_conflict":  "API の文言",
	}
	for code, want := range tests {
		err := &taskclient.APIError{StatusCode: http.StatusConflict, Code: code, Message: "API の文言"}
		if got := userMessage(err); got != want {
			t.Errorf("%s: %q, want %q", code, got, want)
		}
	}
	if got := userMessage(errors.New("boom")); !strings.Contains(got, "おかしな ことが おきた") {
		t.Errorf("想定外のエラー: %q", got)
	}
}

func TestStatusNotice(t *testing.T) {
	tests := []struct {
		from, to taskclient.Status
		want     string
	}{
		{taskclient.StatusTodo, taskclient.StatusDoing, msgStarted},
		{taskclient.StatusTodo, taskclient.StatusDone, msgDone},
		{taskclient.StatusDoing, taskclient.StatusDone, msgDone},
		{taskclient.StatusDone, taskclient.StatusDoing, msgReopened},
		{taskclient.StatusDoing, taskclient.StatusTodo, ""},
		{"", taskclient.StatusDoing, ""}, // from が無い（API を直接使った場合など）
	}
	for _, tt := range tests {
		if got := statusNotice(tt.from, tt.to); got != tt.want {
			t.Errorf("statusNotice(%q, %q) = %q, want %q", tt.from, tt.to, got, tt.want)
		}
	}
}
