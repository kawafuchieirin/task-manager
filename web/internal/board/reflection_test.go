package board

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kawafuchieirin/task-manager/client/taskclient"
)

func TestClear_OpensReflectPanel(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("スライム", taskclient.StatusDoing, nil)

	rec := send(t, h, http.MethodPost, "/tasks/1/status", url.Values{"status": {"done"}, "from": {"doing"}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `class="reflect-panel"`, `hx-put="/tasks/1/reflection"`, "「＋」で はじめた ぎょうは まなんだこと")
	if strings.Contains(body, "けす</button>") {
		t.Error("まだ振り返りが無いので「けす」は出さないはず")
	}
	if got := flashOOB(body); !strings.Contains(got, "ふりかえりを かいておこう") {
		t.Errorf("クリアのメッセージで振り返りを促すはず: %q", got)
	}
}

func TestReflect_SaveShowsExtractedItems(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("スライム", taskclient.StatusDone, nil)

	rec := send(t, h, http.MethodPut, "/tasks/1/reflection", url.Values{"body": {"+ goroutine\r\n- テスト"}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if strings.Contains(body, `class="reflect-panel"`) {
		t.Error("保存したらパネルを閉じるはず")
	}
	assertContains(t, body, "まなんだこと", "<li>goroutine</li>", "できなかったこと", "<li>テスト</li>")
	if got := flashOOB(body); got != "ふりかえりを きろくした！ まなんだこと 1こ、 できなかったこと 1こ。" {
		t.Errorf("メッセージ: %q", got)
	}
	if got, _ := api.get(1); got.Reflection == nil || got.Reflection.Body != "+ goroutine\n- テスト" {
		t.Errorf("CRLF を LF に揃えて保存するはず: %+v", got.Reflection)
	}
}

func TestReflect_ValidationKeepsPanel(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("スライム", taskclient.StatusDone, nil)

	rec := send(t, h, http.MethodPut, "/tasks/1/reflection", url.Values{"body": {"  "}})
	assertStatus(t, rec, http.StatusUnprocessableEntity)
	assertContains(t, rec.Body.String(), `class="reflect-panel"`, "ふりかえりを いれてください。", `aria-invalid="true"`)
}

func TestReflect_ExtractFailureAndRetry(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("スライム", taskclient.StatusDone, nil)
	api.insightDown = true

	rec := send(t, h, http.MethodPut, "/tasks/1/reflection", url.Values{"body": {"+ a"}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, "ちゅうしゅつに しっぱいした！", `hx-post="/tasks/1/reflection/extract"`)
	if got := flashOOB(body); !strings.Contains(got, "insight が うごいているか") {
		t.Errorf("失敗のメッセージ: %q", got)
	}

	api.insightDown = false
	rec = send(t, h, http.MethodPost, "/tasks/1/reflection/extract", url.Values{})
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "<li>a</li>")
	if strings.Contains(rec.Body.String(), "しっぱいした！") {
		t.Error("やり直しに成功したら失敗の表示を消すはず")
	}
}

func TestReflect_EditAndDelete(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("スライム", taskclient.StatusDone, nil)
	api.setReflection(1, "+ まえの ないよう")

	rec := send(t, h, http.MethodGet, "/tasks/1/reflection", nil)
	assertStatus(t, rec, http.StatusOK)
	// html/template は「+」を &#43; にエスケープする（ブラウザでは「+」と表示される）。
	assertContains(t, rec.Body.String(), "&#43; まえの ないよう</textarea>", "けす</button>")

	rec = send(t, h, http.MethodDelete, "/tasks/1/reflection", nil)
	assertStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "card__reflection") {
		t.Error("けしたら振り返りの表示が消えるはず")
	}
	if got := flashOOB(rec.Body.String()); got != "ふりかえりを けした。" {
		t.Errorf("メッセージ: %q", got)
	}
	assertStatus(t, send(t, h, http.MethodDelete, "/tasks/1/reflection", nil), http.StatusNotFound)
}

func TestReflect_ButtonOnlyForDoneOrReflected(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("みちゃくしゅ", taskclient.StatusTodo, nil)
	api.add("クリア済み", taskclient.StatusDone, nil)
	api.add("ふりかえり済みの進行中", taskclient.StatusDoing, nil)
	api.setReflection(3, "+ a")

	body := send(t, h, http.MethodGet, "/board", nil).Body.String()
	if strings.Contains(body, `hx-get="/tasks/1/reflection"`) {
		t.Error("未完了で振り返りも無いタスクには「ふりかえる」を出さないはず")
	}
	assertContains(t, body, `hx-get="/tasks/2/reflection"`, `hx-get="/tasks/3/reflection"`)
}

func TestReflect_EmptyExtraction(t *testing.T) {
	h, api := newTestHandler(t)
	api.add("スライム", taskclient.StatusDone, nil)
	api.setReflection(1, "きょうは あめだった")
	assertContains(t, send(t, h, http.MethodGet, "/board", nil).Body.String(), "ふりかえりから なにも みつからなかった。")
}

func TestReflectionsPage(t *testing.T) {
	h, api := newTestHandler(t)
	api.now = time.Now() // 期間の絞り込みは実際の現在時刻を基準にするため
	api.add("スライム", taskclient.StatusDone, nil)
	api.add("ドラキー", taskclient.StatusDone, nil)
	api.add("ゴーレム", taskclient.StatusDone, nil)
	api.setReflection(1, "+ goroutine\n- テスト")
	api.setReflection(2, "+ htmx")
	api.insightDown = true
	api.setReflection(3, "+ x")

	rec := send(t, h, http.MethodGet, "/reflections", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, "<!doctype html>", `aria-current="page">ふりかえり`,
		"goroutine", "「スライム」", "htmx", "「ドラキー」", "テスト",
		"ちゅうしゅつに しっぱいした ふりかえりが 1こ ある", `href="/reflections?range=week" aria-current="true"`)
}

func TestReflectionsPage_EmptyAndRanges(t *testing.T) {
	h, api := newTestHandler(t)
	api.now = time.Now().AddDate(0, 0, -10) // 10日前の振り返り
	api.add("スライム", taskclient.StatusDone, nil)
	api.setReflection(1, "+ むかしの まなび")

	assertContains(t, send(t, h, http.MethodGet, "/reflections?range=week", nil).Body.String(), "この きかんの ふりかえりは ない ようだ。")
	assertContains(t, send(t, h, http.MethodGet, "/reflections?range=month", nil).Body.String(), "むかしの まなび")
	assertContains(t, send(t, h, http.MethodGet, "/reflections?range=all", nil).Body.String(), "むかしの まなび")
	// 不正な期間は既定（この1しゅうかん）として扱う。
	assertContains(t, send(t, h, http.MethodGet, "/reflections?range=evil", nil).Body.String(), `href="/reflections?range=week" aria-current="true"`)
}

func TestReflectionsPage_APIUnavailable(t *testing.T) {
	h, api := newTestHandler(t)
	api.down = true
	rec := send(t, h, http.MethodGet, "/reflections", nil)
	assertStatus(t, rec, http.StatusBadGateway)
	assertContains(t, rec.Body.String(), "<!doctype html>", "API サーバーと つうしん できない！")
}

func TestRangeBounds(t *testing.T) {
	// 2026-10-03 23:30 JST（= 14:30 UTC）
	now := time.Date(2026, 10, 3, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		key      string
		from, to string
	}{
		{"today", "2026-10-03T00:00:00+09:00", "2026-10-04T00:00:00+09:00"},
		{"week", "2026-09-27T00:00:00+09:00", "2026-10-04T00:00:00+09:00"},
		{"month", "2026-09-04T00:00:00+09:00", "2026-10-04T00:00:00+09:00"},
	}
	for _, tt := range tests {
		from, to := rangeBounds(tt.key, now)
		if from.Format(time.RFC3339) != tt.from || to.Format(time.RFC3339) != tt.to {
			t.Errorf("%s: [%s, %s), want [%s, %s)", tt.key, from.Format(time.RFC3339), to.Format(time.RFC3339), tt.from, tt.to)
		}
	}
	if from, to := rangeBounds("all", now); from != nil || to != nil {
		t.Error("all は制限なし")
	}
}
