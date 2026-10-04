package jsonapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatalf("JSON のデコード: %v", err)
	}
	return m
}

func TestWriteError_OmitsEmptyDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError[string](rec, http.StatusNotFound, CodeNotFound, "x", nil)
	errObj := decodeBody(t, rec)["error"].(map[string]any)
	if _, ok := errObj["details"]; ok {
		t.Error("details が空なら省略するはず")
	}

	rec = httptest.NewRecorder()
	WriteError(rec, http.StatusUnprocessableEntity, CodeValidationFailed, "x", []string{"a"})
	if got := decodeBody(t, rec)["error"].(map[string]any)["details"]; got == nil {
		t.Error("details があれば出すはず")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestDecodeJSON(t *testing.T) {
	type in struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name, contentType, body string
		wantOK                  bool
		wantStatus              int
		wantCode                string
	}{
		{"正常", "application/json", `{"name":"a"}`, true, 0, ""},
		{"charset 付き", "application/json; charset=utf-8", `{"name":"a"}`, true, 0, ""},
		{"Content-Type 違い", "text/plain", `{"name":"a"}`, false, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType},
		{"空", "application/json", ``, false, http.StatusBadRequest, CodeInvalidJSON},
		{"未知のフィールド", "application/json", `{"nam":"a"}`, false, http.StatusBadRequest, CodeInvalidJSON},
		{"型違い", "application/json", `{"name":1}`, false, http.StatusBadRequest, CodeInvalidJSON},
		{"JSON が2つ", "application/json", `{"name":"a"}{}`, false, http.StatusBadRequest, CodeInvalidJSON},
		{"大きすぎる", "application/json", `{"name":"` + strings.Repeat("a", 100) + `"}`, false, http.StatusRequestEntityTooLarge, CodePayloadTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()
			var dst in
			ok := DecodeJSON(rec, req, &dst, 64)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v（%s）", ok, tt.wantOK, rec.Body.String())
			}
			if ok {
				return
			}
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := decodeBody(t, rec)["error"].(map[string]any)["code"]; got != tt.wantCode {
				t.Errorf("code = %v, want %s", got, tt.wantCode)
			}
		})
	}
}

func TestMethodNotAllowedAndNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	MethodNotAllowed(http.MethodGet, http.MethodPost).ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, POST" {
		t.Errorf("405: status = %d, Allow = %q", rec.Code, rec.Header().Get("Allow"))
	}

	rec = httptest.NewRecorder()
	NotFound("ない").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound || decodeBody(t, rec)["error"].(map[string]any)["code"] != CodeNotFound {
		t.Errorf("404: status = %d", rec.Code)
	}
}
