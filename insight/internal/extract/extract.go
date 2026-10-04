// Package extract は振り返りのテキストから「学んだこと」と「できなかったこと」を抽出する。
//
// 抽出の方式は Extractor インターフェースで差し替えられる（現在はルールベース。将来 LLM の実装を追加できる）。
package extract

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"
)

// Result は抽出結果。どちらも空でなく、重複を除いた入力順の一覧。
type Result struct {
	Learned    []string
	NotLearned []string
}

// Extractor は振り返りのテキストから抽出する。
type Extractor interface {
	Extract(ctx context.Context, text string) (Result, error)
}

// RuleBased はルールに従って抽出する。次の順に判定する。
//
//  1. 見出し（「学んだこと」「できなかったこと」「課題」など）の下の行は、その見出しの分類にする
//  2. 見出しの外で「+」で始まる行は学んだこと、「-」で始まる行はできなかったことにする
//  3. それ以外の文は、キーワード（「理解した」「できなかった」など）で分類する。どちらにも当たらない文は捨てる
type RuleBased struct{}

type kind int

const (
	kindNone kind = iota
	kindLearned
	kindNotLearned
)

// 見出しの語（前後の記号を除き、大文字小文字を区別せずに完全一致で判定する）。
var (
	learnedHeadings = []string{"学んだこと", "学び", "わかったこと", "分かったこと", "できたこと", "よかったこと", "良かったこと", "good", "learned"}
	notHeadings     = []string{"できなかったこと", "わからなかったこと", "分からなかったこと", "課題", "反省", "困ったこと", "次の課題", "bad", "problem", "todo"}
)

// キーワード。否定（できなかったこと）を先に判定する（「理解できなかった」を学んだことにしないため）。
var (
	notKeywords = []string{
		"できなかった", "できていない", "わからなかった", "分からなかった", "わかっていない", "分かっていない",
		"難しかった", "むずかしかった", "苦戦", "つまずいた", "躓いた", "曖昧", "あいまい", "まだ", "忘れた", "失敗", "課題",
	}
	learnedKeywords = []string{
		"学んだ", "学べた", "理解した", "理解できた", "わかった", "分かった", "できた", "覚えた", "知った",
		"身についた", "身に付いた", "習得", "気づいた", "気付いた",
	}
)

// 箇条書きの記号（見出しの下の行の先頭から取り除く）。
const bullets = "-－−*＊・•●○◯+＋"

// Extract はテキストを行ごとに分類する。ctx は将来の実装（外部 API 呼び出し）のためのもの。
func (RuleBased) Extract(_ context.Context, text string) (Result, error) {
	var (
		res     Result
		section = kindNone
	)
	add := func(k kind, item string) {
		item = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(item), "。．."))
		if item == "" {
			return
		}
		switch k {
		case kindLearned:
			if !slices.Contains(res.Learned, item) {
				res.Learned = append(res.Learned, item)
			}
		case kindNotLearned:
			if !slices.Contains(res.NotLearned, item) {
				res.NotLearned = append(res.NotLearned, item)
			}
		}
	}

	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if k := heading(line); k != kindNone {
			section = k
			continue
		}
		if section != kindNone {
			add(section, strings.TrimLeft(line, bullets+" 　"))
			continue
		}
		switch r, size := utf8.DecodeRuneInString(line); r {
		case '+', '＋':
			add(kindLearned, line[size:])
			continue
		case '-', '－', '−':
			add(kindNotLearned, line[size:])
			continue
		}
		for _, sentence := range splitSentences(line) {
			add(classify(sentence), sentence)
		}
	}
	return res, nil
}

// heading は行が見出しならその分類を返す。「## 学んだこと：」「【課題】」なども見出しとみなす。
func heading(line string) kind {
	s := strings.Trim(line, "#＃【】[]「」■□◆◇*＊:：　 ")
	s = strings.ToLower(s)
	switch {
	case slices.Contains(learnedHeadings, s):
		return kindLearned
	case slices.Contains(notHeadings, s):
		return kindNotLearned
	}
	return kindNone
}

func classify(sentence string) kind {
	for _, k := range notKeywords {
		if strings.Contains(sentence, k) {
			return kindNotLearned
		}
	}
	for _, k := range learnedKeywords {
		if strings.Contains(sentence, k) {
			return kindLearned
		}
	}
	return kindNone
}

// splitSentences は行を文に分ける（句点・感嘆符・疑問符の後ろで区切る）。
func splitSentences(line string) []string {
	var (
		sentences []string
		b         strings.Builder
	)
	for _, r := range line {
		b.WriteRune(r)
		if strings.ContainsRune("。！？!?", r) {
			sentences = append(sentences, b.String())
			b.Reset()
		}
	}
	if b.Len() > 0 {
		sentences = append(sentences, b.String())
	}
	return sentences
}
