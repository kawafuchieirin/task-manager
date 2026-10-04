package board

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kawafuchieirin/task-manager/web/internal/taskclient"
)

// 画面に出す文言はこのファイルに集める。
// 画面はレトロ RPG 風のデザインなので、言葉づかいも合わせる（ひらがな多め・文節で区切る・「！」で締める）。
// API のメッセージは他のアプリ向けの普通の日本語なので、画面ではエラーのコードを見て言い換える。

var statusLabels = map[taskclient.Status]string{
	taskclient.StatusTodo:  "みちゃくしゅ",
	taskclient.StatusDoing: "しんこうちゅう",
	taskclient.StatusDone:  "クリア",
}

// action はカードに表示するステータス変更ボタン。
type action struct {
	Label string
	To    taskclient.Status
}

// actionsFor はステータスごとに表示する遷移ボタンを返す。クリアからは進行中に戻せる。
func actionsFor(s taskclient.Status) []action {
	switch s {
	case taskclient.StatusTodo:
		return []action{{"とりかかる", taskclient.StatusDoing}, {"クリアする", taskclient.StatusDone}}
	case taskclient.StatusDoing:
		return []action{{"みちゃくしゅに もどす", taskclient.StatusTodo}, {"クリアする", taskclient.StatusDone}}
	case taskclient.StatusDone:
		return []action{{"やりなおす", taskclient.StatusDoing}}
	}
	return nil
}

// 操作が成功したときにメッセージウィンドウに出す文言。%s はタスク名。
const (
	msgCreated      = "「%s」が あらわれた！"
	msgStarted      = "「%s」に とりかかった！"
	msgDone         = "「%s」を やっつけた！ ふりかえりを かいておこう。"
	msgReopened     = "「%s」が ふたたび あらわれた！"
	msgDeleted      = "タスクを すてた。"
	msgTimerStarted = "「%s」との たたかいが はじまった！"
	msgTimerStopped = "「%s」との たたかいを おえた。"

	msgReflectionSaved   = "ふりかえりを きろくした！ まなんだこと %dこ、 できなかったこと %dこ。"
	msgReflectionFailed  = "ふりかえりは きろくしたが、 ちゅうしゅつに しっぱいした！ make status で insight が うごいているか たしかめてください。"
	msgReflectionDeleted = "ふりかえりを けした。"
)

// タイマーの二重起動など、操作できなかったときの文言。
const (
	msgTimerRunningElsewhere        = "「%s」の タイマーが うごいている！ さきに ていし してください。"
	msgTimerRunningElsewhereUnknown = "べつの タイマーが うごいている！ さきに ていし してください。"
)

// statusNotice はステータスを変えたときの文言を返す。出さない場合は空文字。
func statusNotice(from, to taskclient.Status) string {
	switch {
	case to == taskclient.StatusDone:
		return msgDone
	case from == taskclient.StatusDone:
		return msgReopened
	case from == taskclient.StatusTodo && to == taskclient.StatusDoing:
		return msgStarted
	}
	return ""
}

// 画面だけで使う入力エラーのコード（API のコードは taskclient.FieldError.Code を参照）。
const (
	codeNotInteger = "not_integer"
	codeOutOfRange = "out_of_range"
)

// 入力値の上限。API の検証と同じ値（文言に出すため、ここにも持つ）。
const (
	maxTitleLen       = 100
	maxDescriptionLen = 2000
	maxEstimatedMin   = 7 * 24 * 60
	maxReflectionLen  = 5000
)

// fieldMessage は入力エラーを画面の言葉づかいで返す。知らない組み合わせは API の文言をそのまま使う。
func fieldMessage(fe taskclient.FieldError) string {
	switch fe.Field + "/" + fe.Code {
	case "title/required":
		return "タスクの なまえを いれてください。"
	case "title/too_long":
		return fmt.Sprintf("なまえは %dもじ いないに してください。", maxTitleLen)
	case "description/too_long":
		return fmt.Sprintf("せつめいは %dもじ いないに してください。", maxDescriptionLen)
	case "estimated_min/out_of_range":
		return fmt.Sprintf("もくひょうは 0〜%dふん で いれてください。", maxEstimatedMin)
	case "estimated_min/not_integer":
		return "もくひょうは ふんを すうじで いれてください。"
	case "status/invalid":
		return "その じょうたいには できない。"
	case "body/required":
		return "ふりかえりを いれてください。"
	case "body/too_long":
		return fmt.Sprintf("ふりかえりは %dもじ いないに してください。", maxReflectionLen)
	case "minutes/out_of_range":
		return fmt.Sprintf("さぎょうじかんは 1〜%dふん で いれてください。", maxManualMinutes)
	case "ended_at/in_future":
		return "みらいの じかんは きろく できない。"
	case "ended_at/too_long":
		return "1かいの きろくは 24じかん までです。"
	case "ended_at/not_after_start":
		return "おわりの じかんは はじめより あとに してください。"
	}
	if fe.Message != "" {
		return fe.Message
	}
	return "いれた ないようが おかしい ようだ。"
}

// userMessage は操作できなかったときに画面に表示するメッセージを返す。内部の詳細はログにだけ出す。
func userMessage(err error) string {
	var (
		apiErr *taskclient.APIError
		ve     *taskclient.ValidationError
	)
	switch {
	case errors.Is(err, taskclient.ErrNotFound):
		return "さがしている ものが みつからない！ がめんを よみこみなおしてください。"
	case errors.Is(err, taskclient.ErrUnavailable):
		return "API サーバーと つうしん できない！ make status で api が うごいているか たしかめてください。"
	case errors.As(err, &ve):
		msgs := make([]string, len(ve.Errors))
		for i, fe := range ve.Errors {
			msgs[i] = fieldMessage(fe)
		}
		return strings.Join(msgs, " ")
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized:
		return "API キーが あわない！ WEB_API_KEY と API_KEY を たしかめてください。"
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict:
		return conflictMessage(apiErr)
	default:
		return "なにか おかしな ことが おきた！ しばらく してから ためしてください。"
	}
}

// conflictMessage は 409（状態の衝突）を画面の言葉づかいで返す。知らないコードは API の文言を使う。
func conflictMessage(apiErr *taskclient.APIError) string {
	switch apiErr.Code {
	case "timer_already_running":
		return msgTimerRunningElsewhereUnknown
	case "timer_not_running":
		return "この タスクの タイマーは うごいていない。"
	case "task_completed":
		return "クリアした タスクの タイマーは うごかせない。"
	case "entry_running":
		return "けいそくちゅうの きろくは なおせない。 さきに ていし してください。"
	}
	return apiErr.Message
}
