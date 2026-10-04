// Package character はボードに表示するドット絵のキャラクター「ピコ」の成長と表情を決める。
//
// レベルはクリアしたタスクの数で上がり、表情はボードの進捗とタイマーの状態で変わる。
// 見た目は sprites.go のドット絵を SVG にして表示する（拡大してもドットがにじまない）。
package character

import (
	"fmt"
	"html/template"
	"strings"
)

// Name はキャラクターの名前。
const Name = "ピコ"

// levelThresholds は各レベルになるのに必要なクリア数。levelThresholds[0] が Lv1。
var levelThresholds = []int{0, 5, 15, 30, 50}

type stage struct {
	Name   string
	Pixels []string
}

// Level はクリア数からレベル（1 始まり）を返す。
func Level(done int) int {
	lv := 1
	for i, need := range levelThresholds {
		if done >= need {
			lv = i + 1
		}
	}
	return lv
}

// ToNextLevel は次のレベルまでに必要なクリア数を返す。最大レベルなら 0。
func ToNextLevel(done int) int {
	lv := Level(done)
	if lv >= len(levelThresholds) {
		return 0
	}
	return levelThresholds[lv] - done
}

// Mood はキャラクターの状態。
type Mood string

// キャラクターの状態。
const (
	MoodSleep  Mood = "sleep"  // タスクが無い・まだ1つもクリアしていない
	MoodWalk   Mood = "walk"   // 途中
	MoodHappy  Mood = "happy"  // 全部クリア
	MoodBattle Mood = "battle" // タイマーで計測中（ほかより優先）
)

// MoodFor はボードの状態から表情を決める。
func MoodFor(done, total int, timerRunning bool) Mood {
	switch {
	case timerRunning:
		return MoodBattle
	case total == 0 || done == 0:
		return MoodSleep
	case done == total:
		return MoodHappy
	default:
		return MoodWalk
	}
}

// Status は画面に表示するキャラクターの状態。
type Status struct {
	Name      string
	Level     int
	StageName string
	MaxLevel  bool
	ToNext    int
	Mood      Mood
	// Frames はアニメーションのコマ（1〜2枚）。CSS で切り替える。
	Frames []template.HTML
}

// StatusFor はクリア数とボードの状態から、表示するキャラクターの状態を作る。
func StatusFor(done, total int, timerRunning bool) Status {
	lv := Level(done)
	st := stages[lv-1]
	mood := MoodFor(done, total, timerRunning)
	return Status{
		Name:      Name,
		Level:     lv,
		StageName: st.Name,
		MaxLevel:  lv == len(levelThresholds),
		ToNext:    ToNextLevel(done),
		Mood:      mood,
		Frames:    frames[lv-1][mood],
	}
}

// frames はレベル・表情ごとの SVG を起動時に1度だけ作ったもの。
var frames = buildFrames()

func buildFrames() []map[Mood][]template.HTML {
	out := make([]map[Mood][]template.HTML, len(stages))
	for i, st := range stages {
		if err := validate(st.Pixels); err != nil {
			panic(fmt.Sprintf("ドット絵 %q が不正: %v", st.Name, err)) // 定義の誤りは起動時に気づけるようにする
		}
		open := st.Pixels
		bob := shiftDown(open)
		out[i] = map[Mood][]template.HTML{
			MoodWalk:   {render(open), render(bob)},
			MoodBattle: {render(open), render(bob)},
			MoodSleep:  {render(closeEyes(open))},
			MoodHappy:  {render(smile(open)), render(shiftDown(smile(open)))},
		}
	}
	return out
}

const size = 16

func validate(pixels []string) error {
	if len(pixels) != size {
		return fmt.Errorf("%d 行（%d 行のはず）", len(pixels), size)
	}
	eyes := 0
	for y, row := range pixels {
		if len(row) != size {
			return fmt.Errorf("%d 行目が %d 文字（%d 文字のはず）", y+1, len(row), size)
		}
		for x := range len(row) {
			c := row[x]
			if c == '.' {
				continue
			}
			if _, ok := palette[c]; !ok {
				return fmt.Errorf("%d 行 %d 列目の %q は palette に無い色", y+1, x+1, c)
			}
			if c == 'E' {
				eyes++
			}
		}
	}
	if eyes == 0 {
		return fmt.Errorf("目（E）が無い")
	}
	return nil
}

// shiftDown は全体を1ドット下げる（歩くときの上下の揺れ）。
func shiftDown(pixels []string) []string {
	return append([]string{strings.Repeat(".", size)}, pixels[:size-1]...)
}

// closeEyes は目を閉じた顔にする（縦2ドットの目の上側を体の色にし、下側だけを線として残す）。
func closeEyes(pixels []string) []string {
	return mapEyes(pixels, func(upper bool) bool { return !upper })
}

// smile は笑った顔にする（目の上側だけを残し、下側を体の色にする）。
func smile(pixels []string) []string {
	return mapEyes(pixels, func(upper bool) bool { return upper })
}

// mapEyes は目のドットのうち keep が true のものだけを残し、ほかは左隣の色（体の色）で塗る。
func mapEyes(pixels []string, keep func(upper bool) bool) []string {
	out := make([][]byte, size)
	for y := range size {
		out[y] = []byte(pixels[y])
	}
	for y := range size {
		for x := range size {
			if pixels[y][x] != 'E' {
				continue
			}
			upper := y+1 < size && pixels[y+1][x] == 'E'
			if !keep(upper) && x > 0 {
				out[y][x] = pixels[y][x-1]
			}
		}
	}
	res := make([]string, size)
	for y := range size {
		res[y] = string(out[y])
	}
	return res
}

// render はドット絵を SVG にする。横に同じ色が続くドットは1つの rect にまとめて小さくする。
func render(pixels []string) template.HTML {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="sprite" viewBox="0 0 %d %d" shape-rendering="crispEdges" aria-hidden="true" focusable="false">`, size, size)
	for y, row := range pixels {
		for x := 0; x < size; {
			c := row[x]
			run := 1
			for x+run < size && row[x+run] == c {
				run++
			}
			if c != '.' {
				fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="1" fill="%s"/>`, x, y, run, palette[c])
			}
			x += run
		}
	}
	b.WriteString(`</svg>`)
	// palette と座標だけから作る固定の SVG で、利用者の入力は含まないので安全。
	return template.HTML(b.String()) //nolint:gosec // 上記のとおり、利用者の入力を含まない
}
