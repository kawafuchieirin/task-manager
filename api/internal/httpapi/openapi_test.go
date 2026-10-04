package httpapi

import (
	"bufio"
	"bytes"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	apispec "github.com/kawafuchieirin/task-manager/api"
)

// specOperations は openapi.yaml の paths から「パス → メソッド（大文字）」を読み取る。
// YAML のライブラリを依存に加えないよう、paths の書き方（2文字下げのパス、4文字下げのメソッド）を前提に読む。
func specOperations(t *testing.T, spec []byte) map[string][]string {
	t.Helper()
	pathRe := regexp.MustCompile(`^  (/[^:\s]*):\s*$`)
	methodRe := regexp.MustCompile(`^    (get|put|post|patch|delete):\s*$`)
	ops := map[string][]string{}
	inPaths, current := false, ""
	sc := bufio.NewScanner(bytes.NewReader(spec))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && !strings.HasPrefix(line, " "):
			inPaths = false // 次のトップレベルのキー（components など）
		case inPaths:
			if m := pathRe.FindStringSubmatch(line); m != nil {
				current = m[1]
			} else if m := methodRe.FindStringSubmatch(line); m != nil && current != "" {
				ops[current] = append(ops[current], strings.ToUpper(m[1]))
			}
		}
	}
	if len(ops) == 0 {
		t.Fatal("openapi.yaml から paths を読み取れない")
	}
	return ops
}

// TestOpenAPI_MatchesRoutes は、仕様書のパスごとのメソッドが、実装の受け付けるメソッド（405 の Allow）と一致することを確かめる。
func TestOpenAPI_MatchesRoutes(t *testing.T) {
	h := newTestHandler(t, Options{})
	for path, methods := range specOperations(t, apispec.OpenAPI) {
		if !strings.HasPrefix(path, "/api/v1/") {
			continue // /healthz は cmd/api のルーティングで確かめる
		}
		// どのメソッドとも合わない TRACE を送ると、実装が受け付けるメソッドが Allow に入る。
		rec := do(t, h, http.MethodTrace, strings.ReplaceAll(path, "{id}", "1"), "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: 実装にルートが無い（status %d）", path, rec.Code)
			continue
		}
		allow := strings.Split(rec.Header().Get("Allow"), ", ")
		slices.Sort(allow)
		slices.Sort(methods)
		if !slices.Equal(allow, methods) {
			t.Errorf("%s: 仕様書のメソッド %v と実装 %v が違う", path, methods, allow)
		}
	}
}

// TestOpenAPI_CoversAllRoutes は、実装に登録したルートがすべて仕様書に書かれていることを確かめる。
func TestOpenAPI_CoversAllRoutes(t *testing.T) {
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	ops := specOperations(t, apispec.OpenAPI)
	routeRe := regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) (/[^"]+)"`)
	routes := routeRe.FindAllStringSubmatch(string(src), -1)
	if len(routes) == 0 {
		t.Fatal("api.go からルートを読み取れない")
	}
	for _, m := range routes {
		method, path := m[1], m[2]
		if !slices.Contains(ops[path], method) {
			t.Errorf("%s %s が仕様書（openapi.yaml）に無い", method, path)
		}
	}
}

func TestServeOpenAPI(t *testing.T) {
	h := newTestHandler(t, Options{})
	rec := do(t, h, http.MethodGet, "/api/v1/openapi.yaml", "")
	assertStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "openapi: 3.1.0") {
		t.Errorf("仕様書を返すはず: %.40s", rec.Body.String())
	}
	// API キーを設定していれば、仕様書の取得にもキーが要る（/api/v1 は一律で守る）。
	keyed := newTestHandler(t, Options{APIKey: "0123456789abcdef"})
	assertStatus(t, do(t, keyed, http.MethodGet, "/api/v1/openapi.yaml", ""), http.StatusUnauthorized)
}
