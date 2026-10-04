package board

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/web/internal/taskclient"
)

func TestCharacter_Status(t *testing.T) {
	h, api := newTestHandler(t)
	for range 6 {
		api.add("クリア済み", taskclient.StatusDone, nil)
	}
	api.add("みちゃくしゅ", taskclient.StatusTodo, nil)

	body := send(t, h, http.MethodGet, "/", nil).Body.String()
	assertContains(t, body, `class="character character--walk"`, `id="character-name" class="character__name">ピコ<`,
		`class="character__level">2<`, "ヒヨコ", "あと 9こ クリア", "がんばっている！",
		`role="img" aria-label="ピコ（ヒヨコ）の ドットえ"`, `class="character__frame character__frame--1"`)
	if strings.Count(body, "<svg class=\"sprite\"") != 2 {
		t.Error("あるくときは2コマの SVG を出すはず")
	}
}

func TestCharacter_Moods(t *testing.T) {
	t.Run("タスクが無いとねむる", func(t *testing.T) {
		h, _ := newTestHandler(t)
		body := send(t, h, http.MethodGet, "/board", nil).Body.String()
		assertContains(t, body, "character--sleep", "ねむっている…", `class="character__zzz"`)
	})
	t.Run("全部クリアでよろこぶ", func(t *testing.T) {
		h, api := newTestHandler(t)
		api.add("a", taskclient.StatusDone, nil)
		assertContains(t, send(t, h, http.MethodGet, "/board", nil).Body.String(), "character--happy", "ぜんぶ クリアして よろこんでいる！")
	})
	t.Run("タイマー計測中はたたかう", func(t *testing.T) {
		h, api := newTestHandler(t)
		api.add("スライムを たおす", taskclient.StatusDoing, nil)
		body := send(t, h, http.MethodPost, "/tasks/1/timer/start", url.Values{}).Body.String()
		assertContains(t, body, "character--battle", "「スライムを たおす」と たたかっている！", `class="character__bang"`)
	})
}

func TestCharacter_CelebrateAndLevelUp(t *testing.T) {
	h, api := newTestHandler(t)
	for range 4 {
		api.add("クリア済み", taskclient.StatusDone, nil)
	}
	api.add("5こめ", taskclient.StatusDoing, nil)
	api.add("6こめ", taskclient.StatusDoing, nil)

	// 5こめのクリアで Lv1 → Lv2
	rec := send(t, h, http.MethodPost, "/tasks/5/status", url.Values{"status": {"done"}, "from": {"doing"}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, "character--celebrate", "character--levelup", `class="character__sparkle"`, `class="character__level">2<`)
	if got := flashOOB(body); got != "「5こめ」を やっつけた！ ふりかえりを かいておこう。 ピコは レベル2に あがった！" {
		t.Errorf("メッセージ: %q", got)
	}

	// 6こめはレベルが上がらない（ジャンプだけ）
	body = send(t, h, http.MethodPost, "/tasks/6/status", url.Values{"status": {"done"}, "from": {"doing"}}).Body.String()
	assertContains(t, body, "character--celebrate")
	if strings.Contains(body, "character--levelup") || strings.Contains(flashOOB(body), "あがった") {
		t.Error("レベルが変わらないクリアでは、レベルアップを出さないはず")
	}

	// 読み直しやほかの操作ではジャンプしない
	if strings.Contains(send(t, h, http.MethodGet, "/board", nil).Body.String(), "character--celebrate") {
		t.Error("クリア以外ではジャンプしないはず")
	}
}

func TestCharacter_MaxLevel(t *testing.T) {
	h, api := newTestHandler(t)
	for range 50 {
		api.add("t", taskclient.StatusDone, nil)
	}
	assertContains(t, send(t, h, http.MethodGet, "/board", nil).Body.String(), "キング", "さいだい レベル！", `class="character__level">5<`)
}
