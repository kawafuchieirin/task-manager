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
	mu          sync.Mutex
	tasks       []taskclient.Task
	entries     []taskclient.TimeEntry
	nextID      int64
	nextEntryID int64
	now         time.Time
	down        bool // true なら 503 を返す（API 停止中の再現）
	// reflections はタスクごとの振り返り。insightDown なら抽出は失敗（failed）になる。
	reflections map[int64]taskclient.Reflection
	insightDown bool
}

func newFakeAPI(t *testing.T) (*fakeAPI, *taskclient.Client) {
	t.Helper()
	f := &fakeAPI{nextID: 1, nextEntryID: 1, now: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC),
		reflections: map[int64]taskclient.Reflection{}}
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
			return f.withTime(t), true
		}
	}
	return taskclient.Task{}, false
}

// addEntry は終了済みの区間を直接追加する（テストの前提づくり用）。
func (f *fakeAPI) addEntry(taskID int64, start time.Time, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	end := start.Add(d)
	f.entries = append(f.entries, taskclient.TimeEntry{ID: f.nextEntryID, TaskID: taskID, StartedAt: start, EndedAt: &end})
	f.nextEntryID++
}

// withTime は API と同じく、タスクに実績時間・計測中タイマーの開始時刻・振り返りを付ける。
func (f *fakeAPI) withTime(t taskclient.Task) taskclient.Task {
	t.ActualSec, t.RunningSince, t.Reflection = 0, nil, nil
	if r, ok := f.reflections[t.ID]; ok {
		t.Reflection = &r
	}
	for _, e := range f.entries {
		if e.TaskID != t.ID {
			continue
		}
		end := f.now
		if e.EndedAt != nil {
			end = *e.EndedAt
		} else {
			start := e.StartedAt
			t.RunningSince = &start
		}
		t.ActualSec += int64(end.Sub(e.StartedAt) / time.Second)
	}
	return t
}

func (f *fakeAPI) entryJSON(e taskclient.TimeEntry) taskclient.TimeEntry {
	end := f.now
	if e.EndedAt != nil {
		end = *e.EndedAt
	}
	e.DurationSec = int64(end.Sub(e.StartedAt) / time.Second)
	return e
}

func (f *fakeAPI) serveTime(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if id, ok := strings.CutPrefix(path, "/api/v1/time-entries/"); ok && r.Method == http.MethodDelete {
		entryID, _ := strconv.ParseInt(id, 10, 64)
		for i, e := range f.entries {
			if e.ID == entryID {
				f.entries = append(f.entries[:i], f.entries[i+1:]...)
				w.WriteHeader(http.StatusNoContent)
				return true
			}
		}
		writeAPIError(w, http.StatusNotFound, "not_found", "時間記録が見つかりません", nil)
		return true
	}

	rest, ok := strings.CutPrefix(path, "/api/v1/tasks/")
	if !ok {
		return false
	}
	idStr, action, ok := strings.Cut(rest, "/")
	if !ok {
		return false
	}
	taskID, _ := strconv.ParseInt(idStr, 10, 64)
	i := f.index(taskID)
	if i < 0 {
		writeAPIError(w, http.StatusNotFound, "not_found", "タスクが見つかりません", nil)
		return true
	}

	switch {
	case action == "timer/start" && r.Method == http.MethodPost:
		for _, e := range f.entries {
			if e.EndedAt == nil {
				title := f.tasks[f.index(e.TaskID)].Title
				writeAPIError(w, http.StatusConflict, "timer_already_running", "「"+title+"」のタイマーが動いています。先に停止してください", nil)
				return true
			}
		}
		if f.tasks[i].Status == taskclient.StatusTodo {
			f.tasks[i].Status = taskclient.StatusDoing
		}
		e := taskclient.TimeEntry{ID: f.nextEntryID, TaskID: taskID, StartedAt: f.now}
		f.nextEntryID++
		f.entries = append(f.entries, e)
		writeAPIJSON(w, http.StatusCreated, f.entryJSON(e))
	case action == "timer/stop" && r.Method == http.MethodPost:
		for j, e := range f.entries {
			if e.TaskID == taskID && e.EndedAt == nil {
				now := f.now
				f.entries[j].EndedAt = &now
				writeAPIJSON(w, http.StatusOK, f.entryJSON(f.entries[j]))
				return true
			}
		}
		writeAPIError(w, http.StatusConflict, "timer_not_running", "このタスクのタイマーは動いていません", nil)
	case action == "time-entries" && r.Method == http.MethodGet:
		list := []taskclient.TimeEntry{}
		for _, e := range f.entries {
			if e.TaskID == taskID {
				list = append(list, f.entryJSON(e))
			}
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{"time_entries": list})
	case action == "time-entries" && r.Method == http.MethodPost:
		var in struct {
			StartedAt time.Time `json:"started_at"`
			EndedAt   time.Time `json:"ended_at"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		e := taskclient.TimeEntry{ID: f.nextEntryID, TaskID: taskID, StartedAt: in.StartedAt.UTC(), EndedAt: &in.EndedAt}
		f.nextEntryID++
		f.entries = append(f.entries, e)
		writeAPIJSON(w, http.StatusCreated, f.entryJSON(e))
	default:
		return false
	}
	return true
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "停止中", nil)
		return
	}

	if f.serveTime(w, r) || f.serveReflection(w, r) {
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks":
		list := make([]taskclient.Task, len(f.tasks))
		for i, t := range f.tasks {
			list[i] = f.withTime(t)
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{"tasks": list})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks":
		var in struct {
			Title        string `json:"title"`
			Description  string `json:"description"`
			EstimatedMin *int   `json:"estimated_min"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if strings.TrimSpace(in.Title) == "" {
			writeAPIError(w, http.StatusUnprocessableEntity, "validation_failed", "入力値が不正です",
				[]taskclient.FieldError{{Field: "title", Code: "required", Message: "タイトルを入力してください"}})
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
			writeAPIJSON(w, http.StatusOK, f.withTime(f.tasks[i]))
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
				[]taskclient.FieldError{{Field: "title", Code: "required", Message: "タイトルを入力してください"}})
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
				[]taskclient.FieldError{{Field: "status", Code: "invalid", Message: "ステータスは todo / doing / done のいずれかを指定してください"}})
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

// extract は insight を真似て、「+」で始まる行を学んだこと、「-」で始まる行をできなかったことにする。
func (f *fakeAPI) extract(r taskclient.Reflection) taskclient.Reflection {
	r.Learned, r.NotLearned = []string{}, []string{}
	if f.insightDown {
		r.ExtractStatus = taskclient.ExtractFailed
		return r
	}
	for _, line := range strings.Split(r.Body, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			r.Learned = append(r.Learned, strings.TrimSpace(line[1:]))
		case strings.HasPrefix(line, "-"):
			r.NotLearned = append(r.NotLearned, strings.TrimSpace(line[1:]))
		}
	}
	r.ExtractStatus = taskclient.ExtractOK
	return r
}

// setReflection は振り返りを直接用意する（テストの前提づくり用）。
func (f *fakeAPI) setReflection(taskID int64, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reflections[taskID] = f.extract(taskclient.Reflection{TaskID: taskID, Body: body, UpdatedAt: f.now})
}

func (f *fakeAPI) serveReflection(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/v1/reflections" && r.Method == http.MethodGet {
		from, _ := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		to, _ := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		list := []taskclient.ReflectionEntry{}
		for _, t := range f.tasks {
			ref, ok := f.reflections[t.ID]
			if !ok || (!from.IsZero() && ref.UpdatedAt.Before(from)) || (!to.IsZero() && !ref.UpdatedAt.Before(to)) {
				continue
			}
			list = append(list, taskclient.ReflectionEntry{Reflection: ref, TaskTitle: t.Title, TaskStatus: t.Status})
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{"reflections": list})
		return true
	}

	rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/tasks/")
	if !ok {
		return false
	}
	idStr, action, ok := strings.Cut(rest, "/")
	if !ok || (action != "reflection" && action != "reflection/extract") {
		return false
	}
	taskID, _ := strconv.ParseInt(idStr, 10, 64)
	if f.index(taskID) < 0 {
		writeAPIError(w, http.StatusNotFound, "not_found", "タスクが見つかりません", nil)
		return true
	}
	ref, exists := f.reflections[taskID]

	switch {
	case action == "reflection" && r.Method == http.MethodPut:
		var in struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if strings.TrimSpace(in.Body) == "" {
			writeAPIError(w, http.StatusUnprocessableEntity, "validation_failed", "入力値が不正です",
				[]taskclient.FieldError{{Field: "body", Code: "required", Message: "振り返りを入力してください"}})
			return true
		}
		ref = f.extract(taskclient.Reflection{TaskID: taskID, Body: in.Body, UpdatedAt: f.now})
		f.reflections[taskID] = ref
		writeAPIJSON(w, http.StatusOK, ref)
	case !exists:
		writeAPIError(w, http.StatusNotFound, "not_found", "振り返りが見つかりません", nil)
	case action == "reflection/extract" && r.Method == http.MethodPost:
		ref = f.extract(ref)
		f.reflections[taskID] = ref
		writeAPIJSON(w, http.StatusOK, ref)
	case action == "reflection" && r.Method == http.MethodDelete:
		delete(f.reflections, taskID)
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}
