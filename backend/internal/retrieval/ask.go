package retrieval

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Citation 是回答中的一处出处。数组顺序与回答正文里的 [1]、[2] 标注一一对应。
type Citation struct {
	ChunkID       int64  `json:"chunk_id"`
	MaterialID    int64  `json:"material_id"`
	MaterialTitle string `json:"material_title"`
	ChunkIndex    int    `json:"chunk_index"`
	StartOffset   int    `json:"start_offset"`
	EndOffset     int    `json:"end_offset"`
	Snippet       string `json:"snippet"`
}

// AskResult 是问答接口的响应体。
type AskResult struct {
	Answer    string     `json:"answer"`
	Status    string     `json:"status"`
	Citations []Citation `json:"citations"`
	Mode      string     `json:"mode"`
	// AnswerSource 说明这段回答是怎么来的：由对话网关生成，还是服务端直接摘录切片。
	// 页面据此展示不同的提示，避免把摘录误当成模型结论。
	AnswerSource string `json:"answer_source"`
}

// 回答来源的取值。
const (
	SourceChatGateway = "chat_gateway" // 对话网关依据切片生成
	SourceExtract     = "extract"      // 网关不可用时，服务端直接摘录命中的切片
)

// Turn 是历史对话中的一轮。role 为 system 的消息在解析阶段就被丢弃，不会到达这里。
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Answerer 是对话模块的抽象，只在已经取得本班切片之后被调用。
type Answerer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// ErrAnswerUnavailable 表示对话网关不可用。
var ErrAnswerUnavailable = errors.New("chat gateway unavailable")

// CitationsFrom 把命中切片转换为出处列表，顺序保持不变。
func CitationsFrom(hits []Hit) []Citation {
	out := make([]Citation, 0, len(hits))
	for _, h := range hits {
		out = append(out, Citation{
			ChunkID: h.ChunkID, MaterialID: h.MaterialID, MaterialTitle: h.MaterialTitle,
			ChunkIndex: h.ChunkIndex, StartOffset: h.StartOffset, EndOffset: h.EndOffset, Snippet: h.Snippet,
		})
	}
	return out
}

// Ask 先在本班做混合检索取前 topK 条切片；没有命中时直接返回固定文案，
// 不调用对话网关，也不构造任何提示词。
func (s *Service) Ask(ctx context.Context, classID int64, question string, history []Turn, topK int, chat Answerer) (*AskResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, ErrBadQuery
	}
	if topK < 1 {
		topK = s.cfg.AskTopK
	}

	result, err := s.Search(ctx, classID, question, ModeHybrid, topK, 0)
	if err != nil {
		return nil, err
	}
	opts := composeOptions{allowExtract: s.cfg.AskExtractFallback}
	return compose(ctx, result.Hits, question, history, chat, opts)
}

// composeOptions 只带影响降级行为的开关，便于在测试中隔离验证。
type composeOptions struct {
	// allowExtract 为 true 时，对话网关不可用不再直接报错，
	// 而是退回「按命中切片原样摘录」的回答，仍然带 [1]、[2] 出处。
	allowExtract bool
}

// compose 决定「要不要问模型」：没有命中切片时请求到此为止，
// 编排层因此可以在单元测试里验证生成模型确实没有被调用。
func compose(ctx context.Context, hits []Hit, question string, history []Turn, chat Answerer, opts composeOptions) (*AskResult, error) {
	if len(hits) == 0 {
		// 无依据：请求在检索之后即结束，生成模型不会被触发。
		return &AskResult{Answer: NoEvidenceMessage, Status: "no_evidence", Citations: []Citation{}, Mode: string(ModeHybrid), AnswerSource: SourceExtract}, nil
	}
	citations := CitationsFrom(hits)
	if chat == nil {
		if !opts.allowExtract {
			return nil, ErrAnswerUnavailable
		}
		return &AskResult{Answer: ExtractiveAnswer(question, hits), Status: "ok", Citations: citations, Mode: string(ModeHybrid), AnswerSource: SourceExtract}, nil
	}
	answer, err := chat.Complete(ctx, systemPrompt(), userPrompt(question, history, hits))
	if err == nil && strings.TrimSpace(answer) != "" {
		return &AskResult{Answer: answer, Status: "ok", Citations: citations, Mode: string(ModeHybrid), AnswerSource: SourceChatGateway}, nil
	}
	if !opts.allowExtract {
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAnswerUnavailable, err)
		}
		return nil, ErrAnswerUnavailable
	}
	return &AskResult{Answer: ExtractiveAnswer(question, hits), Status: "ok", Citations: citations, Mode: string(ModeHybrid), AnswerSource: SourceExtract}, nil
}

// ExtractiveAnswer 在对话网关不可用时给出「摘录式」回答。
// 它只把已经命中的切片原文按顺序编号列出，不做总结、不补充资料之外的信息，
// 因此依旧满足「可追溯、不编造」；页面会注明这不是模型生成的结论。
func ExtractiveAnswer(question string, hits []Hit) string {
	var b strings.Builder
	b.WriteString("未连接对话网关，以下按原文摘录资料中的相关段落（未作改写、未补充资料之外的内容）：\n\n")
	for i, h := range hits {
		title := h.MaterialTitle
		if title == "" {
			title = "未命名材料"
		}
		body := strings.TrimSpace(h.Snippet)
		runes := []rune(body)
		if len(runes) > 160 {
			body = string(runes[:160]) + "……"
		}
		b.WriteString(fmt.Sprintf("[%d] 《%s》第 %d 段（字符 %d–%d）\n%s\n\n", i+1, title, h.ChunkIndex+1, h.StartOffset, h.EndOffset, body))
	}
	return strings.TrimRight(b.String(), "\n")
}

// systemPrompt 由服务端写入，约束模型只依据编号对应的资料作答。
func systemPrompt() string {
	return "你是班级知识库助手。只能依据用户提供的编号资料回答问题，回答里用 [1]、[2] 这样的编号标注出处，编号与资料顺序一致。" +
		"资料中没有依据时，直接回答“资料中未找到相关内容”，不要编造，也不要使用你自己的知识。" +
		"回答保持简短，两三句话即可。"
}

// userPrompt 组装对话模块的输入：材料标题、切片序号、切片正文，以及用户最新一句提问。
// 向量分量、其他班级的切片与客户端构造的 system 消息都不在这里。
func userPrompt(question string, history []Turn, hits []Hit) string {
	var b strings.Builder
	b.WriteString("以下是检索到的本班资料：\n\n")
	for i, h := range hits {
		b.WriteString(fmt.Sprintf("[%d] 材料：%s（第 %d 段，字符 %d–%d）\n%s\n\n", i+1, h.MaterialTitle, h.ChunkIndex+1, h.StartOffset, h.EndOffset, h.Snippet))
	}
	if len(history) > 0 {
		b.WriteString("此前的对话（仅供理解上下文，不作为依据）：\n")
		for _, t := range history {
			if t.Role != "user" && t.Role != "assistant" {
				continue
			}
			b.WriteString(fmt.Sprintf("%s：%s\n", t.Role, strings.TrimSpace(t.Content)))
		}
		b.WriteString("\n")
	}
	b.WriteString("请只用上面编号对应的资料回答这一句：")
	b.WriteString(question)
	return b.String()
}

// ChunkView 是页面上「MySQL 与向量库对照」所需的形态：同一条切片在两库各存了什么。
type ChunkView struct {
	ChunkIndex  int        `json:"chunk_index"`
	Status      string     `json:"index_status"`
	StartOffset int        `json:"start_offset"`
	EndOffset   int        `json:"end_offset"`
	Excerpt     string     `json:"excerpt"`
	Text        string     `json:"chunk_text"`
	MySQL       MySQLRef   `json:"mysql"`
	Point       *PointView `json:"vector_point"`
	Linked      bool       `json:"linked"`
	Collection  string     `json:"collection"`
}

// MySQLRef 是关系库一侧的标识。
type MySQLRef struct {
	ChunkID    int64 `json:"chunk_id"`
	EntryID    int64 `json:"knowledge_entry_id"`
	MaterialID int64 `json:"material_id"`
	ClassID    int64 `json:"class_id"`
}

// PointView 是向量库一侧的标识与形态；这里只有浮点向量和若干标识，没有正文。
type PointView struct {
	PointID    int64     `json:"point_id"`
	ChunkID    int64     `json:"payload_chunk_id"`
	EntryID    int64     `json:"payload_entry_id"`
	MaterialID int64     `json:"payload_material_id"`
	ClassID    int64     `json:"payload_class_id"`
	ChunkIndex int       `json:"payload_chunk_index"`
	Dim        int       `json:"dim"`
	Norm       float64   `json:"norm"`
	Preview    []float32 `json:"preview"`
}
