// Package llm 封装对话网关：仅在已经检索到本班切片之后才被调用，
// 用来依据这些切片撰写带 [1]、[2] 标注的简短回答。
//
// 它与嵌入模块是两次独立的调用。没有命中切片时，这一整块流程都不会发生。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrUnavailable 表示对话网关当前不可用。
var ErrUnavailable = errors.New("chat gateway unavailable")

// Client 调用 OpenAI 兼容的 /chat/completions 接口。
type Client struct {
	baseURL string
	apiKey  string
	model   string
	timeout time.Duration
	client  *http.Client
}

// New 构造对话客户端。
func New(baseURL, apiKey, model string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, timeout: timeout, client: &http.Client{Timeout: timeout}}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float32   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete 把 system 与 user 消息交给网关并返回回答文本。
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	payload, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    []message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature: 0.2,
		MaxTokens:   600,
	})
	if err != nil { return "", err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil { return "", err }
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" { req.Header.Set("Authorization", "Bearer "+c.apiKey) }
	resp, err := c.client.Do(req)
	if err != nil { return "", fmt.Errorf("%w: %v", ErrUnavailable, err) }
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil { return "", fmt.Errorf("%w: %v", ErrUnavailable, err) }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: gateway returned %d", ErrUnavailable, resp.StatusCode)
	}
	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil { return "", fmt.Errorf("%w: bad payload", ErrUnavailable) }
	if parsed.Error != nil { return "", fmt.Errorf("%w: %s", ErrUnavailable, parsed.Error.Message) }
	if len(parsed.Choices) == 0 { return "", fmt.Errorf("%w: empty choices", ErrUnavailable) }
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
