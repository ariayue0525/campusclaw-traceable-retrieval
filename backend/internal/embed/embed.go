// Package embed 把一段文本转换为定长浮点向量。
//
// 它是「入库与向量检索时使用的模块」，与对话模块相互独立：关键字检索完全不
// 经过这里。实现有两种：
//
//   - gateway：调用课程网关提供的 OpenAI 兼容 /embeddings 接口，密钥只留在服务端；
//   - local：内置的确定性哈希嵌入器，用于没有网关密钥时也能一键启动并演示三种模式。
//
// 两种实现输出的维度必须一致，且与 Qdrant 集合的 size 相同，否则写入会失败。
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder 是嵌入模块的抽象。
type Embedder interface {
	// Embed 把若干段文本转换为等长向量，返回顺序与输入一致。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim 返回向量维度。
	Dim() int
	// Kind 返回实现名称，用于响应中说明本次走了哪条路径。
	Kind() string
}

// ErrUnavailable 表示嵌入服务当前不可用，调用方据此把切片标记为 failed。
var ErrUnavailable = errors.New("embedding service unavailable")

// gatewayEmbedder 调用 OpenAI 兼容的 /embeddings 接口。
type gatewayEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	dim     int
	timeout time.Duration
	client  *http.Client
}

// NewGateway 构造网关嵌入器。
func NewGateway(baseURL, apiKey, model string, dim int, timeout time.Duration) Embedder {
	return &gatewayEmbedder{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, dim: dim, timeout: timeout, client: &http.Client{Timeout: timeout}}
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (g *gatewayEmbedder) Dim() int  { return g.dim }
func (g *gatewayEmbedder) Kind() string { return "gateway" }

func (g *gatewayEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 { return nil, nil }
	payload, err := json.Marshal(embeddingRequest{Model: g.model, Input: texts})
	if err != nil { return nil, err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil { return nil, err }
	req.Header.Set("Content-Type", "application/json")
	if g.apiKey != "" { req.Header.Set("Authorization", "Bearer "+g.apiKey) }
	resp, err := g.client.Do(req)
	if err != nil { return nil, fmt.Errorf("%w: %v", ErrUnavailable, err) }
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil { return nil, fmt.Errorf("%w: %v", ErrUnavailable, err) }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: gateway returned %d", ErrUnavailable, resp.StatusCode)
	}
	var parsed embeddingResponse
	if err := json.Unmarshal(body, &parsed); err != nil { return nil, fmt.Errorf("%w: bad payload", ErrUnavailable) }
	if parsed.Error != nil { return nil, fmt.Errorf("%w: %s", ErrUnavailable, parsed.Error.Message) }
	if len(parsed.Data) != len(texts) { return nil, fmt.Errorf("%w: expected %d vectors, got %d", ErrUnavailable, len(texts), len(parsed.Data)) }
	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(texts) { continue }
		out[d.Index] = d.Embedding
	}
	for i, v := range out {
		if len(v) == 0 { return nil, fmt.Errorf("%w: empty vector at %d", ErrUnavailable, i) }
		if len(v) != g.dim { return nil, fmt.Errorf("%w: vector dim %d does not match collection size %d", ErrUnavailable, len(v), g.dim) }
	}
	return out, nil
}

// localEmbedder 是确定性的本地回退实现：字符 uni/bi-gram 哈希 + 亚线性词频 + L2 归一化。
// 它衡量的是字面与字序的接近程度，不是真实语义相似度；配置网关密钥后即为真实语义嵌入。
type localEmbedder struct{ dim int }

// NewLocal 构造本地回退嵌入器。
func NewLocal(dim int) Embedder { return &localEmbedder{dim: dim} }

func (l *localEmbedder) Dim() int      { return l.dim }
func (l *localEmbedder) Kind() string  { return "local" }

func (l *localEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts { out = append(out, l.vector(text)) }
	return out, nil
}

func (l *localEmbedder) vector(text string) []float32 {
	vec := make([]float64, l.dim)
	counts := map[uint64]float64{}
	runes := []rune(strings.ToLower(text))
	add := func(key string) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(key))
		counts[h.Sum64()]++
	}
	if len(runes) == 0 { return make([]float32, l.dim) }
	for _, r := range runes { add(string(r)) }
	for i := 0; i+1 < len(runes); i++ { add(string(runes[i]) + string(runes[i+1])) }
	for hash, count := range counts {
		idx := int(hash % uint64(l.dim))
		sign := 1.0
		if hash&(1<<63) != 0 { sign = -1.0 }
		vec[idx] += sign * (1 + math.Log(count))
	}
	norm := 0.0
	for _, v := range vec { norm += v * v }
	norm = math.Sqrt(norm)
	out := make([]float32, l.dim)
	if norm == 0 { return out }
	for i, v := range vec { out[i] = float32(v / norm) }
	return out
}

// Cosine 计算两个向量的余弦相似度；维度不一致时返回 0。
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) { return 0 }
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 { return 0 }
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Norm 返回向量的 L2 范数，用于页面上说明向量形态。
func Norm(v []float32) float64 {
	var sum float64
	for _, x := range v { sum += float64(x) * float64(x) }
	return math.Sqrt(sum)
}
