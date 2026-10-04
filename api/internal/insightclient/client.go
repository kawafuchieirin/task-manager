// Package insightclient は insight サービス（振り返りの抽出 API）を呼び出すクライアント。
package insightclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultTimeout = 5 * time.Second
	defaultRetries = 2
	defaultBackoff = 200 * time.Millisecond
	// maxResponseBytes は応答ボディの上限。壊れた応答でメモリを使い切らないようにする。
	maxResponseBytes = 1 << 20
)

// Client は insight のクライアント。task.Extractor を満たす。
type Client struct {
	endpoint string
	http     *http.Client
	retries  int
	backoff  time.Duration
}

// Option は Client の設定を変更する。
type Option func(*Client)

// WithHTTPClient は内部の http.Client を差し替える。
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.http = hc } }

// WithRetry は再試行回数と初回の待ち時間（以降は倍々）を変更する。
func WithRetry(retries int, backoff time.Duration) Option {
	return func(c *Client) { c.retries, c.backoff = retries, backoff }
}

// New は baseURL（例: http://127.0.0.1:8081）の insight を呼ぶクライアントを返す。
func New(baseURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("insight の URL %q は http(s)://host[:port] 形式で指定してください", baseURL)
	}
	c := &Client{
		endpoint: u.JoinPath("/api/v1/extract").String(),
		http:     &http.Client{Timeout: defaultTimeout},
		retries:  defaultRetries,
		backoff:  defaultBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Extract はテキストを insight に送り、学んだこと・できなかったことを返す。
// 抽出は副作用の無い処理なので、通信エラーと 5xx のときは指数バックオフで再試行する（4xx は再試行しない）。
func (c *Client) Extract(ctx context.Context, text string) (learned, notLearned []string, err error) {
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return nil, nil, fmt.Errorf("リクエストの JSON 化に失敗: %w", err)
	}

	wait := c.backoff
	for attempt := 0; ; attempt++ {
		learned, notLearned, retry, err := c.send(ctx, payload)
		if err == nil || !retry || attempt >= c.retries {
			return learned, notLearned, err
		}
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("insight の呼び出しを中断: %w", ctx.Err())
		case <-time.After(wait):
		}
		wait *= 2
	}
}

func (c *Client) send(ctx context.Context, payload []byte) (learned, notLearned []string, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, false, fmt.Errorf("リクエストの作成に失敗: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// 呼び出し元のキャンセル以外（接続拒否・タイムアウトなど）は、insight の再起動中の可能性があるので再試行する。
		return nil, nil, ctx.Err() == nil, fmt.Errorf("insight に接続できません: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, true, fmt.Errorf("insight の応答の読み取りに失敗: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, nil, resp.StatusCode >= 500,
			fmt.Errorf("insight がエラーを返しました（%d）: %s", resp.StatusCode, bytes.TrimSpace(data))
	}
	var body struct {
		Learned    []string `json:"learned"`
		NotLearned []string `json:"not_learned"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, nil, false, fmt.Errorf("insight の応答を解釈できません: %w", err)
	}
	if body.Learned == nil || body.NotLearned == nil {
		return nil, nil, false, errors.New("insight の応答に learned / not_learned がありません")
	}
	return body.Learned, body.NotLearned, false, nil
}
