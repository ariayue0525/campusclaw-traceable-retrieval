// Package llm 封装对话网关：仅在已经检索到本班切片之后才被调用，
// 用来依据这些切片撰写带 [1]、[2] 标注的简短回答。
//
// 它与嵌入模块是两次独立的调用。没有命中切片时，这一整块流程都不会发生。
package llm

import (
	"bufio"
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

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID string `json:"id"`
	Type string `json:"type"`
	Function struct{Name string `json:"name"`;Arguments string `json:"arguments"`} `json:"function"`
}

type FunctionTool struct{
	Type string `json:"type"`
	Function struct{Name string `json:"name"`;Description string `json:"description"`;Parameters json.RawMessage `json:"parameters"`} `json:"function"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
	Stream      bool      `json:"stream,omitempty"`
	Tools []FunctionTool `json:"tools,omitempty"`
	ToolChoice string `json:"tool_choice,omitempty"`
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
		Messages:    []Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
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

// Stream forwards only incremental text emitted by the OpenAI-compatible
// gateway. It does not synthesize a response or split a completed answer.
func (c *Client) Stream(ctx context.Context, messages []Message, onDelta func(string) error) error {
	calls,err:=c.StreamWithTools(ctx,messages,nil,onDelta)
	if err!=nil{return err}
	if len(calls)>0{return fmt.Errorf("%w: unexpected tool call",ErrUnavailable)}
	return nil
}

// StreamWithTools returns tool calls collected from incremental function-call
// fragments. The host decides whether to execute them and may start another
// streamed model turn with the resulting tool messages.
func (c *Client) StreamWithTools(ctx context.Context,messages []Message,tools []FunctionTool,onDelta func(string)error)([]ToolCall,error){
	request:=chatRequest{Model:c.model,Messages:messages,Temperature:0.2,MaxTokens:600,Stream:true,Tools:tools}
	if len(tools)>0{request.ToolChoice="auto"}
	payload,err:=json.Marshal(request)
	if err!=nil{return nil,err}
	req,err:=http.NewRequestWithContext(ctx,http.MethodPost,c.baseURL+"/chat/completions",bytes.NewReader(payload))
	if err!=nil{return nil,err}
	req.Header.Set("Content-Type","application/json")
	req.Header.Set("Accept","text/event-stream")
	if c.apiKey!=""{req.Header.Set("Authorization","Bearer "+c.apiKey)}
	// Bound connection setup, but let the request context control the entire
	// stream; Client.Timeout would abort a healthy long answer mid-token.
	streamClient:=&http.Client{Transport:&http.Transport{ResponseHeaderTimeout:c.timeout}}
	resp,err:=streamClient.Do(req)
	if err!=nil{return nil,fmt.Errorf("%w: %v",ErrUnavailable,err)}
	defer resp.Body.Close()
	if resp.StatusCode<200||resp.StatusCode>=300{return nil,fmt.Errorf("%w: gateway returned %d",ErrUnavailable,resp.StatusCode)}
	scanner:=bufio.NewScanner(io.LimitReader(resp.Body,8<<20))
	scanner.Buffer(make([]byte,4096),1<<20)
	done:=false
	calls:=map[int]*ToolCall{}
	for scanner.Scan(){
		line:=strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line,"data:"){continue}
		data:=strings.TrimSpace(strings.TrimPrefix(line,"data:"))
		if data=="[DONE]"{done=true;break}
		var part struct{Choices []struct{Delta struct{Content string `json:"content"`;ToolCalls []struct{Index int `json:"index"`;ID string `json:"id"`;Type string `json:"type"`;Function struct{Name string `json:"name"`;Arguments string `json:"arguments"`} `json:"function"`} `json:"tool_calls"`} `json:"delta"`} `json:"choices"`; Error *struct{Message string `json:"message"`} `json:"error"`}
		if err:=json.Unmarshal([]byte(data),&part);err!=nil{return nil,fmt.Errorf("%w: malformed stream event",ErrUnavailable)}
		if part.Error!=nil{return nil,fmt.Errorf("%w: gateway error",ErrUnavailable)}
		if len(part.Choices)>0&&part.Choices[0].Delta.Content!=""{
			if err:=onDelta(part.Choices[0].Delta.Content);err!=nil{return nil,err}
		}
		if len(part.Choices)>0{
			for _,fragment:=range part.Choices[0].Delta.ToolCalls{
				call:=calls[fragment.Index]
				if call==nil{call=&ToolCall{};calls[fragment.Index]=call}
				call.ID+=fragment.ID
				if fragment.Type!=""{call.Type=fragment.Type}
				call.Function.Name+=fragment.Function.Name
				call.Function.Arguments+=fragment.Function.Arguments
			}
		}
	}
	if err:=scanner.Err();err!=nil{return nil,fmt.Errorf("%w: %v",ErrUnavailable,err)}
	if !done{return nil,fmt.Errorf("%w: stream ended before DONE",ErrUnavailable)}
	out:=make([]ToolCall,0,len(calls))
	for i:=0;i<len(calls);i++{call:=calls[i];if call==nil||call.ID==""||call.Function.Name==""{return nil,fmt.Errorf("%w: malformed tool call",ErrUnavailable)};out=append(out,*call)}
	return out,nil
}
