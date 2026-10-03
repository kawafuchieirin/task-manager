package board

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kawafuchieirin/task-manager/web/internal/taskclient"
)

// fakeAPI は API サーバーの /api/v1/tasks を真似る、テスト用のインメモリ実装。
// 画面が API の契約（JSON の形・エラー形式・完了日時の扱い）どおりに動くかを確かめるために使う。
// 検証は画面のテストに必要な分（タイトル必須・ステータスの値）だけを再現する。
type fakeAPI struct {
	mu     sync.Mutex
	tasks  []taskclient.Task
	nextID int64
	now    time.Time
	down   bool // true なら 503 を返す（API 停止中の再現）
}

func newFakeAPI(t *testing.T) (*fakeAPI, *taskclient.Client) {
	t.Helper()
	f := &fakeAPI{nextID: 1, now: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := taskclient.New(srv.URL, "", taskclient.WithRetry(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

func (f *fakeAPI) add(title string, status taskclient.Status, estimated *int) taskclient.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := taskclient.Task{ID: f.nextID, Title: title, Status: status, EstimatedMin: estimated, CreatedAt: f.now, UpdatedAt: f.now}
	if status == taskclient.StatusDone {
		now := f.now
		t.CompletedAt = &now
	}
	f.nextID++
	f.tasks = append(f.tasks, t)
	return t
}

func (f *fakeAPI) get(id int64) (taskclient.Task, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.ID == id {
			return t, true
		}
	}
	return taskclient.Task{}, false
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "停止中", nil)
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks":
		writeAPIJSON(w, http.StatusOK, map[string]any{"tasks": f.tasks})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks":
		var in struct {
			Title        string `json:"title"`
			Description  string `json:"description"`
			EstimatedMin *int   `json:"estimated_min"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if strings.TrimSpace(in.Title) == "" {
			writeAPIError(w, http.StatusUnprocessableEntity, "validation_failed", "入力値が不正です",
				[]taskclient.FieldError{{Field: "title", Message: "タイトルを入力してください"}})
			return
		}
		t := taskclient.Task{ID: f.nextID, Title: strings.TrimSpace(in.Title), Description: in.Description,
			Status: taskclient.StatusTodo, EstimatedMin: in.EstimatedMin, CreatedAt: f.now, UpdatedAt: f.now}
		f.nextID++
		f.tasks = append(f.tasks, t)
		writeAPIJSON(w, http.StatusCreated, t)
	case strings.HasPrefix(r.URL.Path, "/api/v1/tasks/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/"), 10, 64)
		i := f.index(id)
		if i < 0 {
			writeAPIError(w, http.StatusNotFound, "not_found", "タスクが見つかりません", nil)
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeAPIJSON(w, http.StatusOK, f.tasks[i])
		case http.MethodDelete:
			f.tasks = append(f.tasks[:i], f.tasks[i+1:]...)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPatch:
			f.patch(w, r, i)
		}
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "API のパスが存在しません", nil)
	}
}

func (f *fakeAPI) index(id int64) int {
	for i, t := range f.tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeAPI) patch(w http.ResponseWriter, r *http.Request, i int) {
	var in map[string]json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&in)
	t := f.tasks[i]
	if raw, ok := in["title"]; ok {
		_ = json.Unmarshal(raw, &t.Title)
		t.Title = strings.TrimSpace(t.Title)
		if t.Title == "" {
			writeAPIError(w, http.StatusUnprocessableEntity, "validation_failed", "入力値が不正です",
				[]taskclient.FieldError{{Field: "title", Message: "タイトルを入力してください"}})
			return
		}
	}
	if raw, ok := in["description"]; ok {
		_ = json.Unmarshal(raw, &t.Description)
	}
	if raw, ok := in["estimated_min"]; ok {
		t.EstimatedMin = nil
		_ = json.Unmarshal(raw, &t.EstimatedMin)
	}
	if raw, ok := in["status"]; ok {
		var s taskclient.Status
		_ = json.Unmarshal(raw, &s)
		if s != taskclient.StatusTodo && s != taskclient.StatusDoing && s != taskclient.StatusDone {
			writeAPIError(w, http.StatusUnprocessableEntity, "validation_failed", "入力値が不正です",
				[]taskclient.FieldError{{Field: "status", Message: "ステータスは todo / doing / done のいずれかを指定してください"}})
			return
		}
		switch {
		case s != taskclient.StatusDone:
			t.CompletedAt = nil
		case t.Status != taskclient.StatusDone:
			now := f.now
			t.CompletedAt = &now
		}
		t.Status = s
	}
	t.UpdatedAt = f.now
	f.tasks[i] = t
	writeAPIJSON(w, http.StatusOK, t)
}

func writeAPIJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string, details []taskclient.FieldError) {
	writeAPIJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg, "details": details}})
}
