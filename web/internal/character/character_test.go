package character

import (
	"strings"
	"testing"
)

func TestSpritesAreValid(t *testing.T) {
	// buildFrames は不正な定義で panic するので、ここまで来れば全ステージが正しい。
	for i, st := range stages {
		if err := validate(st.Pixels); err != nil {
			t.Errorf("Lv%d %s: %v", i+1, st.Name, err)
		}
	}
	if len(stages) != len(levelThresholds) {
		t.Errorf("姿の数 %d とレベルの数 %d が合わない", len(stages), len(levelThresholds))
	}
}

func TestValidate_RejectsMalformed(t *testing.T) {
	ok := stages[0].Pixels
	short := append([]string{}, ok...)
	short[3] = short[3][:15]
	unknown := append([]string{}, ok...)
	unknown[3] = "Z" + unknown[3][1:]
	noEyes := make([]string, size)
	for i := range noEyes {
		noEyes[i] = strings.Repeat("W", size)
	}
	for name, px := range map[string][]string{"行が短い": short, "知らない色": unknown, "目が無い": noEyes, "行数違い": ok[:15]} {
		if err := validate(px); err == nil {
			t.Errorf("%s: エラーを期待したが nil", name)
		}
	}
}

func TestLevel(t *testing.T) {
	tests := []struct{ done, level, toNext int }{
		{0, 1, 5}, {4, 1, 1}, {5, 2, 10}, {14, 2, 1}, {15, 3, 15},
		{29, 3, 1}, {30, 4, 20}, {49, 4, 1}, {50, 5, 0}, {999, 5, 0},
	}
	for _, tt := range tests {
		if got := Level(tt.done); got != tt.level {
			t.Errorf("Level(%d) = %d, want %d", tt.done, got, tt.level)
		}
		if got := ToNextLevel(tt.done); got != tt.toNext {
			t.Errorf("ToNextLevel(%d) = %d, want %d", tt.done, got, tt.toNext)
		}
	}
}

func TestMoodFor(t *testing.T) {
	tests := []struct {
		done, total int
		running     bool
		want        Mood
	}{
		{0, 0, false, MoodSleep},
		{0, 3, false, MoodSleep},
		{1, 3, false, MoodWalk},
		{3, 3, false, MoodHappy},
		{3, 3, true, MoodBattle}, // 計測中はほかより優先
		{0, 1, true, MoodBattle},
	}
	for _, tt := range tests {
		if got := MoodFor(tt.done, tt.total, tt.running); got != tt.want {
			t.Errorf("MoodFor(%d, %d, %v) = %q, want %q", tt.done, tt.total, tt.running, got, tt.want)
		}
	}
}

func TestStatusFor(t *testing.T) {
	st := StatusFor(15, 20, false)
	if st.Name != "ピコ" || st.Level != 3 || st.StageName != "コトリ" || st.ToNext != 15 || st.MaxLevel || st.Mood != MoodWalk {
		t.Errorf("StatusFor: %+v", st)
	}
	if len(st.Frames) != 2 {
		t.Errorf("あるくときは2コマ: %d", len(st.Frames))
	}
	if max := StatusFor(50, 50, false); !max.MaxLevel || max.StageName != "キング" || max.Mood != MoodHappy {
		t.Errorf("最大レベル: %+v", max)
	}
	if sleep := StatusFor(0, 0, false); len(sleep.Frames) != 1 {
		t.Errorf("ねむるときは1コマ: %d", len(sleep.Frames))
	}
}

func TestExpressionsChangeOnlyEyes(t *testing.T) {
	base := stages[2].Pixels
	closed, smiled := closeEyes(base), smile(base)
	if strings.Join(closed, "") == strings.Join(base, "") || strings.Join(smiled, "") == strings.Join(base, "") {
		t.Fatal("表情で目が変わるはず")
	}
	for y := range size {
		for x := range size {
			if base[y][x] != 'E' && (closed[y][x] != base[y][x] || smiled[y][x] != base[y][x]) {
				t.Fatalf("目以外のドット (%d,%d) が変わった", x, y)
			}
		}
	}
	// 閉じた目と笑った目は、目のドットが半分ずつ残る（縦2ドットの上下どちらか）。
	count := func(px []string) int { return strings.Count(strings.Join(px, ""), "E") }
	if count(closed) != count(base)/2 || count(smiled) != count(base)/2 {
		t.Errorf("目のドット: 元 %d / 閉じ %d / 笑い %d", count(base), count(closed), count(smiled))
	}
}

func TestRender(t *testing.T) {
	svg := string(render(stages[1].Pixels))
	if !strings.HasPrefix(svg, `<svg class="sprite" viewBox="0 0 16 16"`) || !strings.Contains(svg, `shape-rendering="crispEdges"`) ||
		!strings.Contains(svg, `aria-hidden="true"`) || !strings.HasSuffix(svg, "</svg>") {
		t.Errorf("SVG の形: %.120s…", svg)
	}
	// 横に続く同じ色はまとめるので、ドットの数より rect は少ない。
	dots := strings.Count(strings.Join(stages[1].Pixels, ""), "") - 1 - strings.Count(strings.Join(stages[1].Pixels, ""), ".")
	if rects := strings.Count(svg, "<rect"); rects == 0 || rects >= dots {
		t.Errorf("rect %d 個（ドット %d 個より少ないはず）", rects, dots)
	}
}

func TestShiftDown(t *testing.T) {
	got := shiftDown(stages[0].Pixels)
	if got[0] != strings.Repeat(".", size) || got[1] != stages[0].Pixels[0] || len(got) != size {
		t.Error("1ドット下にずらすはず")
	}
}
