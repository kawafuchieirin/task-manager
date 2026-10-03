// Package taskclient は API サーバー（/api/v1）を呼び出すクライアント。
// 画面アプリは DB を持たず、タスクの読み書きはすべてこのクライアント経由で行う。
package taskclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Status はタスクの進行状態。値は API と同じ。
type Status string

// タスクのステータス。ボードの列に対応する。
const (
	StatusTodo  Status = "todo"
	StatusDoing Status = "doing"
	StatusDone  Status = "done"
)

// Statuses はボードの列の表示順。
var Statuses = []Status{StatusTodo, StatusDoing, StatusDone}

// Task は API が返すタスク。
type Task struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Status       Status     `json:"status"`
	EstimatedMin *int       `json:"estimated_min"`
	CompletedAt  *time.Time `json:"completed_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// ActualSec は実績時間の合計（秒）。計測中の区間は API が応答した時点までを含む。
	ActualSec int64 `json:"actual_sec"`
	// RunningSince は計測中のタイマーの開始時刻。タイマーが動いていなければ nil。
	RunningSince *time.Time `json:"running_since"`
}

// TimeEntry はタスクに作業した1区間。EndedAt が nil の区間は計測中。
type TimeEntry struct {
	ID          int64      `json:"id"`
	TaskID      int64      `json:"task_id"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	DurationSec int64      `json:"duration_sec"`
}

// CreateInput はタスク作成の入力。
type CreateInput struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	EstimatedMin *int   `json:"estimated_min,omitempty"`
}

// UpdateInput はタスクの部分更新の入力。nil の項目は送らない（変更しない）。
type UpdateInput struct {
	Title       *string
	Description *string
	Status      *Status
	// SetEstimatedMin が true なら EstimatedMin を送る。EstimatedMin が nil なら目標時間を未設定に戻す。
	SetEstimatedMin bool
	EstimatedMin    *int
}

// MarshalJSON は「送らない」と「null を送る」を区別して JSON にする。
func (in UpdateInput) MarshalJSON() ([]byte, error) {
	body := map[string]any{}
	if in.Title != nil {
		body["title"] = *in.Title
	}
	if in.Description != nil {
		body["description"] = *in.Description
	}
	if in.Status != nil {
		body["status"] = *in.Status
	}
	if in.SetEstimatedMin {
		body["estimated_min"] = in.EstimatedMin // nil は null になる
	}
	return json.Marshal(body)
}

// エラー。呼び出し側は errors.Is / errors.As で判定する。
var (
	// ErrNotFound は指定したタスクが存在しないことを表す。
	ErrNotFound = errors.New("タスクが見つかりません")
	// ErrUnavailable は API サーバーに接続できない、または API が一時的に応答できないことを表す。
	ErrUnavailable = errors.New("API サーバーに接続できません")
)

// FieldError は1項目分の入力エラー。
// Code は違反の種類（required / too_long など）。画面はこれを見て独自の文言を出す。
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidationError は API が返した入力値の検証エラー（422）。
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, fe := range e.Errors {
		msgs[i] = fe.Field + ": " + fe.Message
	}
	return "入力値が不正です: " + strings.Join(msgs, ", ")
}

// Message は指定した項目のエラーメッセージを返す。エラーが無ければ空文字。
func (e *ValidationError) Message(field string) string {
	for _, fe := range e.Errors {
		if fe.Field == field {
			return fe.Message
		}
	}
	return ""
}

// APIError は上記以外の API のエラー応答。
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API エラー（%d %s）: %s", e.StatusCode, e.Code, e.Message)
}

const (
	defaultTimeout = 5 * time.Second
	// 読み取り（GET）だけを再試行する。POST などは二重登録になりうるため再試行しない。
	defaultRetries = 2
	defaultBackoff = 100 * time.Millisecond
	// maxResponseBytes は応答ボディの上限。壊れた応答でメモリを使い切らないようにする。
	maxResponseBytes = 10 << 20
)

// Client は API サーバーのクライアント。
type Client struct {
	baseURL *url.URL
	apiKey  string
	http    *http.Client
	retries int
	backoff time.Duration
}

// Option は Client の設定を変更する。
type Option func(*Client)

// WithHTTPClient は内部の http.Client を差し替える。
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.http = hc } }

// WithRetry は読み取りの再試行回数と初回の待ち時間（以降は倍々）を変更する。
func WithRetry(retries int, backoff time.Duration) Option {
	return func(c *Client) { c.retries, c.backoff = retries, backoff }
}

// New は baseURL（例: http://127.0.0.1:8080）の API を呼ぶクライアントを返す。
// apiKey が空でなければ Authorization: Bearer で送る。
func New(baseURL, apiKey string, opts ...Option) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("API の URL %q は http(s)://host[:port] 形式で指定してください", baseURL)
	}
	c := &Client{
		baseURL: u,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: defaultTimeout},
		retries: defaultRetries,
		backoff: defaultBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// List はすべてのタスクを作成順に返す。
func (c *Client) List(ctx context.Context) ([]Task, error) {
	var resp struct {
		Tasks []Task `json:"tasks"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/tasks", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// Get は ID でタスクを取得する。
func (c *Client) Get(ctx context.Context, id int64) (Task, error) {
	var t Task
	err := c.do(ctx, http.MethodGet, taskPath(id), nil, &t)
	return t, err
}

// Create はタスクを作成する。
func (c *Client) Create(ctx context.Context, in CreateInput) (Task, error) {
	var t Task
	err := c.do(ctx, http.MethodPost, "/api/v1/tasks", in, &t)
	return t, err
}

// Update はタスクを部分更新する。
func (c *Client) Update(ctx context.Context, id int64, in UpdateInput) (Task, error) {
	var t Task
	err := c.do(ctx, http.MethodPatch, taskPath(id), in, &t)
	return t, err
}

// Delete はタスクを削除する。
func (c *Client) Delete(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, taskPath(id), nil, nil)
}

// StartTimer はタスクのタイマーを開始する。別のタスクのタイマーが動いていれば 409 の APIError を返す。
func (c *Client) StartTimer(ctx context.Context, taskID int64) (TimeEntry, error) {
	var e TimeEntry
	err := c.do(ctx, http.MethodPost, taskPath(taskID)+"/timer/start", nil, &e)
	return e, err
}

// StopTimer はタスクのタイマーを止める。
func (c *Client) StopTimer(ctx context.Context, taskID int64) (TimeEntry, error) {
	var e TimeEntry
	err := c.do(ctx, http.MethodPost, taskPath(taskID)+"/timer/stop", nil, &e)
	return e, err
}

// ListTimeEntries はタスクの時間記録を開始時刻の順に返す。
func (c *Client) ListTimeEntries(ctx context.Context, taskID int64) ([]TimeEntry, error) {
	var resp struct {
		TimeEntries []TimeEntry `json:"time_entries"`
	}
	if err := c.do(ctx, http.MethodGet, taskPath(taskID)+"/time-entries", nil, &resp); err != nil {
		return nil, err
	}
	return resp.TimeEntries, nil
}

// AddTimeEntry は終了済みの区間を追加する。
func (c *Client) AddTimeEntry(ctx context.Context, taskID int64, start, end time.Time) (TimeEntry, error) {
	var e TimeEntry
	body := map[string]time.Time{"started_at": start.UTC(), "ended_at": end.UTC()}
	err := c.do(ctx, http.MethodPost, taskPath(taskID)+"/time-entries", body, &e)
	return e, err
}

// DeleteTimeEntry は時間記録を削除する。
func (c *Client) DeleteTimeEntry(ctx context.Context, entryID int64) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/time-entries/"+strconv.FormatInt(entryID, 10), nil, nil)
}

func taskPath(id int64) string {
	return "/api/v1/tasks/" + strconv.FormatInt(id, 10)
}

// do はリクエストを送り、成功時は out に応答をデコードする。
// GET は通信エラーと 502 / 503 / 504 のときに指数バックオフで再試行する。
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("リクエストの JSON 化に失敗: %w", err)
		}
	}

	attempts := 1
	if method == http.MethodGet {
		attempts += c.retries
	}
	wait := c.backoff

	var lastErr error
	for i := range attempts {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
			case <-time.After(wait):
			}
			wait *= 2
		}
		retry, err := c.send(ctx, method, path, payload, out)
		if err == nil || !retry {
			return err
		}
		lastErr = err
	}
	return lastErr
}

// send は1回分のリクエストを送る。retry は再試行すれば成功しうるエラーかどうか。
func (c *Client) send(ctx context.Context, method, path string, payload []byte, out any) (retry bool, err error) {
	var reqBody io.Reader
	if payload != nil {
		reqBody = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL.JoinPath(path).String(), reqBody)
	if err != nil {
		return false, fmt.Errorf("リクエストの作成に失敗: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// 呼び出し元のキャンセルは再試行しない。接続拒否やタイムアウトは API 停止中・再起動中の可能性がある。
		var netErr net.Error
		retryable := ctx.Err() == nil && (errors.As(err, &netErr) || errors.Is(err, io.EOF))
		return retryable, fmt.Errorf("%w（%s）: %w", ErrUnavailable, c.baseURL.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return true, fmt.Errorf("%w: 応答の読み取りに失敗: %w", ErrUnavailable, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || resp.StatusCode == http.StatusNoContent {
			return false, nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return false, fmt.Errorf("API の応答を解釈できません: %w", err)
		}
		return false, nil
	}
	return isRetryableStatus(resp.StatusCode), toError(resp.StatusCode, data)
}

func isRetryableStatus(code int) bool {
	return code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// toError は API のエラー応答（{"error": {...}}）を Go のエラーに変換する。
func toError(status int, data []byte) error {
	var body struct {
		Error struct {
			Code    string       `json:"code"`
			Message string       `json:"message"`
			Details []FieldError `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &body) // JSON でない応答（Host 検証の 421 など）は下でまとめて扱う

	switch {
	case status == http.StatusNotFound && body.Error.Code == "not_found":
		// 時間記録が見つからない場合も同じく「対象が無い」として扱い、メッセージは API のものを使う。
		if body.Error.Message != "" && body.Error.Message != ErrNotFound.Error() {
			return fmt.Errorf("%w: %s", ErrNotFound, body.Error.Message)
		}
		return ErrNotFound
	case status == http.StatusUnprocessableEntity && body.Error.Code == "validation_failed":
		return &ValidationError{Errors: body.Error.Details}
	case status >= 500:
		return fmt.Errorf("%w: %w", ErrUnavailable, &APIError{StatusCode: status, Code: body.Error.Code, Message: body.Error.Message})
	}
	msg := body.Error.Message
	if msg == "" {
		msg = strings.TrimSpace(string(data))
	}
	return &APIError{StatusCode: status, Code: body.Error.Code, Message: msg}
}
