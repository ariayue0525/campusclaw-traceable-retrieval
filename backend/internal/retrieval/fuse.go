package retrieval

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"

	"campusclaw/backend/internal/model"
)

// fuseConfig 是融合时需要的少量参数，避免把整个配置结构体传进来。
type fuseConfig struct {
	rrfK         float64
	snippetChars int
	query        string
}

// lookups 把「回表」这一动作收敛成两个闭包：班级条件在闭包里被捕获，
// 融合逻辑因此不接触数据库，也不接触班级编号本身。
type lookups struct {
	chunks func(ctx context.Context, ids []int64) (map[int64]model.Chunk, error)
	titles func(ctx context.Context, ids []int64) (map[int64]string, error)
}

// fused 是同一条切片在两条路径上的排序信息。
type fused struct {
	id           int64
	score        float64
	keywordScore *float64
	vectorScore  *float64
	keywordRank  *int
	vectorRank   *int
}

// bestRank 返回该切片在两条路径中的最好名次，用作同分时的稳定次序。
func (f *fused) bestRank() int {
	best := 1 << 30
	if f.keywordRank != nil && *f.keywordRank < best { best = *f.keywordRank }
	if f.vectorRank != nil && *f.vectorRank < best { best = *f.vectorRank }
	return best
}

// fuse 把两条路径的候选按名次融合为最终次序。
//
// 融合的是名次而不是分数：全文相关度与余弦相似度的量纲不同，直接相加没有意义。
// 每一路都已经按自己的绝对分数过滤过（关键字：全文索引命中；向量：余弦不低于阈值），
// 缺席的一路不贡献任何分数。
func fuse(keyword, vectorHits []ranked, byID map[int64]model.Chunk, lk lookups, cfg fuseConfig) []Hit {
	index := map[int64]*fused{}
	order := []int64{}
	ensure := func(id int64) *fused {
		if item, ok := index[id]; ok { return item }
		item := &fused{id: id}
		index[id] = item
		order = append(order, id)
		return item
	}
	for _, r := range keyword {
		item := ensure(r.id)
		score, rank := r.score, r.rank
		item.keywordScore, item.keywordRank = &score, &rank
	}
	for _, r := range vectorHits {
		item := ensure(r.id)
		score, rank := r.score, r.rank
		item.vectorScore, item.vectorRank = &score, &rank
	}
	for _, item := range index {
		sum := 0.0
		if item.keywordRank != nil { sum += 1 / (cfg.rrfK + float64(*item.keywordRank)) }
		if item.vectorRank != nil { sum += 1 / (cfg.rrfK + float64(*item.vectorRank)) }
		item.score = sum
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := index[order[i]], index[order[j]]
		if a.score != b.score { return a.score > b.score }
		return a.bestRank() < b.bestRank()
	})

	// 回表补齐：任何路径命中的主键都要再用同一班级条件核对一次。
	missing := []int64{}
	for _, id := range order { if _, ok := byID[id]; !ok { missing = append(missing, id) } }
	if len(missing) > 0 && lk.chunks != nil {
		if extra, err := lk.chunks(context.Background(), missing); err == nil {
			for _, c := range extra { byID[c.ID] = c }
		}
	}
	titles := materialTitles(context.Background(), lk, byID, order)

	hits := make([]Hit, 0, len(order))
	rank := 0
	for _, id := range order {
		c, ok := byID[id]
		if !ok { continue }
		rank++
		item := index[id]
		hits = append(hits, Hit{
			ChunkID: c.ID, MaterialID: c.MaterialID, MaterialTitle: titles[c.MaterialID], EntryID: c.EntryID,
			ChunkIndex: c.ChunkIndex, StartOffset: c.StartOffset, EndOffset: c.EndOffset,
			Snippet: snippet(c.ChunkText, cfg.query, cfg.snippetChars),
			Score: item.score, KeywordScore: item.keywordScore, VectorScore: item.vectorScore,
			KeywordRank: item.keywordRank, VectorRank: item.vectorRank, Rank: rank,
		})
	}
	return hits
}

// materialTitles 批量取回材料标题，命中结果才能显示「出自哪一份材料」。
func materialTitles(ctx context.Context, lk lookups, byID map[int64]model.Chunk, order []int64) map[int64]string {
	out := map[int64]string{}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, id := range order {
		c, ok := byID[id]
		if !ok || seen[c.MaterialID] { continue }
		seen[c.MaterialID] = true
		ids = append(ids, c.MaterialID)
	}
	if len(ids) == 0 || lk.titles == nil { return out }
	titles, err := lk.titles(ctx, ids)
	if err != nil { return out }
	for id, title := range titles { out[id] = title }
	return out
}

// snippet 从 MySQL 的 chunk_text 中截取一段摘录，优先落在查询词附近。
// 摘录始终取自关系库，向量库里没有正文。
func snippet(text, query string, max int) string {
	if max < 20 { max = 20 }
	runes := []rune(text)
	if len(runes) <= max { return text }
	at := -1
	if query != "" {
		if i := strings.Index(text, query); i >= 0 { at = utf8.RuneCountInString(text[:i]) }
		if at < 0 {
			// 退一步：用查询中的任意二字片段定位。
			qr := []rune(query)
			for i := 0; i+2 <= len(qr); i++ {
				if j := strings.Index(text, string(qr[i:i+2])); j >= 0 {
					at = utf8.RuneCountInString(text[:j])
					break
				}
			}
		}
	}
	start := 0
	if at >= 0 {
		start = at - max/3
		if start < 0 { start = 0 }
	}
	end := start + max
	if end > len(runes) {
		end = len(runes)
		start = end - max
		if start < 0 { start = 0 }
	}
	out := string(runes[start:end])
	if start > 0 { out = "…" + out }
	if end < len(runes) { out = out + "…" }
	return out
}
