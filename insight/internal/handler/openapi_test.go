package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kawafuchieirin/task-manager/insight/internal/extract"
)

// TestOpenAPI_MatchesRoutes は、仕様書（insight/openapi.yaml）の /api/v1 のパスとメソッドが、実装と一致することを確かめる。
func TestOpenAPI_MatchesRoutes(t *testing.T) {
	spec, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// YAML のライブラリを依存に加えないよう、paths の書き方（2文字下げのパス、4文字下げのメソッド）を前提に読む。
	pathRe := regexp.MustCompile(`(?m)^  (/api/v1/[^:\s]*):\s*$`)
	methodRe := regexp.MustCompile(`(?m)^    (get|put|post|patch|delete):\s*$`)

	h := New(extract.RuleBased{}, discard)
	paths := pathRe.FindAllStringSubmatchIndex(string(spec), -1)
	if len(paths) == 0 {
		t.Fatal("openapi.yaml から /api/v1 のパスを読み取れない")
	}
	for i, loc := range paths {
		path := string(spec[loc[2]:loc[3]])
		end := len(spec)
		if i+1 < len(paths) {
			end = paths[i+1][0]
		}
		// 次のトップレベルのキー（components など）までをこのパスの範囲とする。
		block := string(spec[loc[1]:end])
		if j := regexp.MustCompile(`(?m)^\S`).FindStringIndex(block); j != nil {
			block = block[:j[0]]
		}
		var methods []string
		for _, m := range methodRe.FindAllStringSubmatch(block, -1) {
			methods = append(methods, strings.ToUpper(m[1]))
		}

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodTrace, path, nil))
		allow := strings.Split(rec.Header().Get("Allow"), ", ")
		slices.Sort(allow)
		slices.Sort(methods)
		if rec.Code != http.StatusMethodNotAllowed || !slices.Equal(allow, methods) {
			t.Errorf("%s: 仕様書 %v / 実装 %v（status %d）", path, methods, allow, rec.Code)
		}
	}
}
