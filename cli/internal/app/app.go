// Package app は tm コマンドの本体。引数・環境変数・出力先を受け取るので、テストから直接呼べる。
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kawafuchieirin/task-manager/client/taskclient"
)

// 終了コード。スクリプトから使うときに失敗の種類を区別できるようにする。
const (
	exitOK    = 0
	exitError = 1 // API のエラー・接続できないなど
	exitUsage = 2 // 使い方の誤り
)

const defaultAPIURL = "http://127.0.0.1:8080"

const usage = `tm — ターミナルから タスクを そうさする

つかいかた:
  tm add <なまえ> [-e 分] [-d せつめい]   タスクを ついか（-e は もくひょう じかん）
  tm ls [-a]                             みかんりょうの いちらん（-a で クリアずみも）
  tm start <id>                          タイマーを かいし
  tm stop [id]                           タイマーを ていし（id を はぶくと うごいているもの）
  tm done <id>                           クリアする
  tm help                                この せつめい

かんきょうへんすう:
  TM_API_URL   API の URL（きてい: http://127.0.0.1:8080）
  TM_API_KEY   API に API_KEY を せっていした ときの キー
`

// printer は書き込みの最初のエラーを覚えておく io.Writer のラッパー。
// 出力先が閉じている（tm ls | head など）ときに、書き込みの失敗を終了コードで知らせるために使う。
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

// usageError は使い方の誤り（終了コード 2）。
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// Run は tm を実行し、終了コードを返す。
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	out, errOut := &printer{w: stdout}, &printer{w: stderr}
	code := execute(ctx, args, getenv, out, errOut)
	if code == exitOK && out.err != nil {
		return exitError // 結果を書き出せなかった
	}
	return code
}

// execute はコマンドを実行する。出力は out / errOut に書く。
func execute(ctx context.Context, args []string, getenv func(string) string, out, errOut *printer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		out.printf("%s", usage)
		return exitOK
	}

	apiURL := getenv("TM_API_URL")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	client, err := taskclient.New(apiURL, getenv("TM_API_KEY"))
	if err != nil {
		errOut.printf("＊ %v\n", err)
		return exitUsage
	}
	c := &cli{api: client, apiURL: apiURL, out: out}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		err = c.add(ctx, rest)
	case "ls", "list":
		err = c.list(ctx, rest)
	case "start":
		err = c.start(ctx, rest)
	case "stop":
		err = c.stop(ctx, rest)
	case "done":
		err = c.done(ctx, rest)
	default:
		err = usageError{fmt.Sprintf("しらない コマンド %q です。", cmd)}
	}

	var ue usageError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &ue):
		errOut.printf("＊ %s\n\n%s", ue.msg, usage)
		return exitUsage
	default:
		errOut.printf("＊ %s\n", c.message(err))
		return exitError
	}
}

type cli struct {
	api    *taskclient.Client
	apiURL string
	out    *printer
}

func (c *cli) say(format string, a ...any) {
	c.out.printf("＊ "+format+"\n", a...)
}

func (c *cli) add(ctx context.Context, args []string) error {
	var (
		title     []string
		estimated *int
		desc      string
	)
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-e" || a == "--estimate":
			if i+1 >= len(args) {
				return usageError{"-e には もくひょう じかん（分）を つけてください。"}
			}
			m, err := strconv.Atoi(args[i+1])
			if err != nil {
				return usageError{fmt.Sprintf("-e の %q は 分を すうじで かいてください。", args[i+1])}
			}
			estimated = &m
			i++
		case a == "-d" || a == "--desc":
			if i+1 >= len(args) {
				return usageError{"-d には せつめいを つけてください。"}
			}
			desc = args[i+1]
			i++
		case a == "--":
			title = append(title, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "-") && a != "-":
			return usageError{fmt.Sprintf("しらない オプション %q です。", a)}
		default:
			title = append(title, a)
		}
	}
	if len(title) == 0 {
		return usageError{"タスクの なまえを かいてください。 例: tm add \"Go を学ぶ\" -e 30"}
	}

	t, err := c.api.Create(ctx, taskclient.CreateInput{Title: strings.Join(title, " "), Description: desc, EstimatedMin: estimated})
	if err != nil {
		return err
	}
	c.say("「%s」が あらわれた！ (#%d)", t.Title, t.ID)
	return nil
}

var statusNames = map[taskclient.Status]string{
	taskclient.StatusTodo:  "みちゃくしゅ",
	taskclient.StatusDoing: "しんこうちゅう",
	taskclient.StatusDone:  "クリア",
}

func (c *cli) list(ctx context.Context, args []string) error {
	all := false
	for _, a := range args {
		switch a {
		case "-a", "--all":
			all = true
		default:
			return usageError{fmt.Sprintf("ls では %q は つかえません。", a)}
		}
	}
	tasks, err := c.api.List(ctx)
	if err != nil {
		return err
	}

	done := 0
	var shown []taskclient.Task
	for _, t := range tasks {
		if t.Status == taskclient.StatusDone {
			done++
			if !all {
				continue
			}
		}
		shown = append(shown, t)
	}
	percent := 0
	if len(tasks) > 0 {
		percent = done * 100 / len(tasks)
	}
	c.out.printf("しんちょく %d%%（%dこ のうち %dこ クリア）\n", percent, len(tasks), done)
	if len(shown) == 0 {
		c.out.printf("タスクは ない ようだ。\n")
		return nil
	}
	for _, t := range shown {
		line := fmt.Sprintf("#%-4d [%s] %s", t.ID, statusNames[t.Status], t.Title)
		if t.EstimatedMin != nil {
			line += "  もくひょう " + minutes(int64(*t.EstimatedMin)*60)
		}
		if t.ActualSec > 0 {
			line += "  じっせき " + minutes(t.ActualSec)
		}
		if t.RunningSince != nil {
			line += "  ⏱ けいそくちゅう"
		}
		c.out.printf("%s\n", line)
	}
	return nil
}

func (c *cli) start(ctx context.Context, args []string) error {
	id, err := oneID(args, "start")
	if err != nil {
		return err
	}
	if _, err := c.api.StartTimer(ctx, id); err != nil {
		return err
	}
	t, err := c.api.Get(ctx, id)
	if err != nil {
		return err
	}
	c.say("「%s」との たたかいが はじまった！ (#%d)", t.Title, t.ID)
	return nil
}

func (c *cli) stop(ctx context.Context, args []string) error {
	var id int64
	switch len(args) {
	case 0:
		// id を省いたら、動いているタイマー（全体で1つ）を止める。
		tasks, err := c.api.List(ctx)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if t.RunningSince != nil {
				id = t.ID
			}
		}
		if id == 0 {
			return errors.New("うごいている タイマーは ない。")
		}
	default:
		var err error
		if id, err = oneID(args, "stop"); err != nil {
			return err
		}
	}
	entry, err := c.api.StopTimer(ctx, id)
	if err != nil {
		return err
	}
	t, err := c.api.Get(ctx, id)
	if err != nil {
		return err
	}
	c.say("「%s」との たたかいを おえた。 こんかい %s、 じっせき ごうけい %s。", t.Title, minutes(entry.DurationSec), minutes(t.ActualSec))
	return nil
}

func (c *cli) done(ctx context.Context, args []string) error {
	id, err := oneID(args, "done")
	if err != nil {
		return err
	}
	status := taskclient.StatusDone
	t, err := c.api.Update(ctx, id, taskclient.UpdateInput{Status: &status})
	if err != nil {
		return err
	}
	c.say("「%s」を やっつけた！ (#%d)", t.Title, t.ID)
	return nil
}

func oneID(args []string, cmd string) (int64, error) {
	if len(args) != 1 {
		return 0, usageError{fmt.Sprintf("tm %s には タスクの id を 1つ つけてください。 例: tm %s 12", cmd, cmd)}
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil || id <= 0 {
		return 0, usageError{fmt.Sprintf("%q は タスクの id では ありません（tm ls で たしかめられます）。", args[0])}
	}
	return id, nil
}

// minutes は秒数を「1時間5分」の形にする（1分未満は「1分 みまん」）。
func minutes(sec int64) string {
	if sec > 0 && sec < 60 {
		return "1分 みまん"
	}
	m := sec / 60
	switch h, min := m/60, m%60; {
	case h == 0:
		return fmt.Sprintf("%d分", min)
	case min == 0:
		return fmt.Sprintf("%d時間", h)
	default:
		return fmt.Sprintf("%d時間%d分", h, min)
	}
}

// message はエラーを利用者向けの文にする。
func (c *cli) message(err error) string {
	var (
		apiErr *taskclient.APIError
		ve     *taskclient.ValidationError
	)
	switch {
	case errors.Is(err, taskclient.ErrUnavailable):
		return fmt.Sprintf("API サーバーと つうしん できない！ make start で きどう してください。（%s）", c.apiURL)
	case errors.Is(err, taskclient.ErrNotFound):
		return "その タスクは みつからない！ tm ls で id を たしかめてください。"
	case errors.As(err, &ve):
		msgs := make([]string, len(ve.Errors))
		for i, fe := range ve.Errors {
			msgs[i] = fe.Message
		}
		return strings.Join(msgs, " ")
	case errors.As(err, &apiErr) && apiErr.StatusCode == 401:
		return "API キーが あわない！ TM_API_KEY を たしかめてください。"
	case errors.As(err, &apiErr) && apiErr.Code == "timer_already_running":
		return apiErr.Message + "（tm stop で とめられます）"
	case errors.As(err, &apiErr):
		return apiErr.Message
	default:
		return err.Error()
	}
}
