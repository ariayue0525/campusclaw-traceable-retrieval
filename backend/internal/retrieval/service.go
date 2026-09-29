// Package retrieval 编排切分、嵌入、向量库与MySQL之间的协作。
//
// 三条检索路径访问的组件不同，但最后都回到 MySQL 取回 chunk_text：
//
//	keyword → 只查 MySQL 全文索引（不嵌入、不访问 Qdrant）
//	vector  → 问句嵌入 → Qdrant 按班级过滤 → 低于阈值丢弃 → 主键回表
//	hybrid  → 两条路径各按绝对分数过滤后，按名次执行 RRF（k = 60）
//
// 班级标识只来自调用方传入的会话班级，请求中的班级编号进不到这里。
package retrieval

import (
	"context"
	"errors"
	"strings"

	"campusclaw/backend/internal/chunk"
	"campusclaw/backend/internal/config"
	"campusclaw/backend/internal/embed"
	"campusclaw/backend/internal/model"
	"campusclaw/backend/internal/store"
	"campusclaw/backend/internal/vector"
)

// Mode 是三种检索模式。
type Mode string

const (
	ModeKeyword Mode = "keyword"
	ModeVector  Mode = "vector"
	ModeHybrid  Mode = "hybrid"
)

// NoEvidenceMessage 是无命中时返回的固定文案。
const NoEvidenceMessage = "资料中未找到相关内容"

var (
	// ErrBadQuery 表示问句为空或只有空白。
	ErrBadQuery = errors.New("query must not be empty")
	// ErrBadMode 表示显式指定了未知模式。
	ErrBadMode = errors.New("unknown search mode")
	// ErrVectorUnavailable 表示向量库不可用，向量与混合模式据此返回 503。
	ErrVectorUnavailable = errors.New("vector store unavailable")
)

// ParseMode 把请求中的模式字符串规范化：空值按混合处理，未知值报错。
func ParseMode(raw string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ModeHybrid, nil
	case string(ModeKeyword):
		return ModeKeyword, nil
	case string(ModeVector):
		return ModeVector, nil
	case string(ModeHybrid):
		return ModeHybrid, nil
	}
	return "", ErrBadMode
}

// Hit 是一条可回溯的命中结果。摘录取自 MySQL 的 chunk_text，不取自向量库。
type Hit struct {
	ChunkID      int64    `json:"chunk_id"`
	MaterialID   int64    `json:"material_id"`
	MaterialTitle string  `json:"material_title"`
	EntryID      int64    `json:"knowledge_entry_id"`
	ChunkIndex   int      `json:"chunk_index"`
	StartOffset  int      `json:"start_offset"`
	EndOffset    int      `json:"end_offset"`
	Snippet      string   `json:"snippet"`
	Score        float64  `json:"score"`
	KeywordScore *float64 `json:"keyword_score"`
	VectorScore  *float64 `json:"vector_score"`
	KeywordRank  *int     `json:"keyword_rank"`
	VectorRank   *int     `json:"vector_rank"`
	Rank         int      `json:"rank"`
}

// Result 是一次检索的完整响应体。
type Result struct {
	Mode        string    `json:"mode"`
	Query       string    `json:"q"`
	Status      string    `json:"status"`
	Message     string    `json:"message,omitempty"`
	Hits        []Hit     `json:"hits"`
	Total       int       `json:"total"`
	QueryVector []float32 `json:"query_vector"`
	Collection  string    `json:"collection"`
	Embedder    string    `json:"embedder"`
}

// IndexOutcome 描述一次建索引的结果。
type IndexOutcome struct {
	Strategy string `json:"chunk_strategy"`
	Chunks   int    `json:"chunk_count"`
	Ready    int    `json:"ready_count"`
	Failed   int    `json:"failed_count"`
}

// Service 编排切分、嵌入、向量库与检索。
type Service struct {
	store   *store.Store
	embedder embed.Embedder
	vectors *vector.Store
	cfg     config.Config
	// TitleByMaterial 让检索结果带上材料标题；默认直接查库。
	TitleByMaterial func(ctx context.Context, materialID int64) (string, error)
}

// New 构造检索服务。
func New(st *store.Store, em embed.Embedder, vs *vector.Store, cfg config.Config) *Service {
	return &Service{store: st, embedder: em, vectors: vs, cfg: cfg}
}

// ---- 建索引 ----

// IndexMaterial 按策略重建一份材料的切片与向量：先删除旧切片与旧向量，
// 再写入新切片（pending），逐批嵌入后写入 Qdrant 并把成功的切片标为 ready。
// 嵌入失败的切片保留在 MySQL 中，标记为 failed，不写入不完整的向量数据。
func (s *Service) IndexMaterial(ctx context.Context, ref store.EntryRef, body string, opt chunk.Options) (IndexOutcome, error) {
	opt, err := chunk.Normalize(opt)
	if err != nil { return IndexOutcome{}, err }
	pieces, err := chunk.Split(body, opt)
	if err != nil { return IndexOutcome{}, err }
	if len(pieces) > s.cfg.IndexMaxChunks { pieces = pieces[:s.cfg.IndexMaxChunks] }

	inputs := make([]store.NewChunk, 0, len(pieces))
	for _, p := range pieces { inputs = append(inputs, store.NewChunk{Text: p.Text, Start: p.Start, End: p.End}) }
	oldIDs, newIDs, err := s.store.ReplaceChunks(ctx, ref, string(opt.Strategy), inputs)
	if err != nil { return IndexOutcome{}, err }
	outcome := IndexOutcome{Strategy: string(opt.Strategy), Chunks: len(newIDs)}
	if len(newIDs) == 0 { return outcome, nil }

	// 旧向量先清掉，避免遗留过期主键。
	if len(oldIDs) > 0 {
		if err := s.vectors.Delete(ctx, oldIDs); err != nil { return outcome, err }
	}

	failed := []int64{}
	ready := []int64{}
	batch := 16
	for start := 0; start < len(newIDs); start += batch {
		end := start + batch
		if end > len(newIDs) { end = len(newIDs) }
		ids := newIDs[start:end]
		texts := make([]string, 0, len(ids))
		for _, p := range pieces[start:end] { texts = append(texts, p.Text) }
		vectors, err := s.embedder.Embed(ctx, texts)
		if err != nil || len(vectors) != len(ids) {
			failed = append(failed, ids...)
			continue
		}
		points := make([]vector.Point, 0, len(ids))
		for i, id := range ids {
			points = append(points, vector.Point{
				ID: id, Vector: vectors[i],
				ClassID: ref.ClassID, MaterialID: ref.MaterialID, EntryID: ref.EntryID,
				ChunkID: id, // 向量主键 = knowledge_chunks.id = payload.chunk_id
				Index:   start + i,
			})
		}
		if err := s.vectors.Upsert(ctx, points); err != nil {
			failed = append(failed, ids...)
			continue
		}
		ready = append(ready, ids...)
	}
	if len(failed) > 0 { _ = s.store.SetChunkStatus(ctx, failed, "failed") }
	if len(ready) > 0 {
		if err := s.store.SetChunkStatus(ctx, ready, "ready"); err != nil { return outcome, err }
	}
	outcome.Ready, outcome.Failed = len(ready), len(failed)
	return outcome, nil
}

// ---- 检索 ----

// Search 在会话班级范围内执行检索。
func (s *Service) Search(ctx context.Context, classID int64, query string, mode Mode, limit, offset int) (*Result, error) {
	query = strings.TrimSpace(query)
	if query == "" { return nil, ErrBadQuery }
	if limit < 1 { limit = s.cfg.SearchLimit }
	if limit > s.cfg.SearchMaxLimit { limit = s.cfg.SearchMaxLimit }
	if offset < 0 { offset = 0 }

	result := &Result{Mode: string(mode), Query: query, Hits: []Hit{}, Total: 0, Collection: s.vectors.Collection(), Embedder: s.embedder.Kind()}

	var keyword []ranked
	var vectorHits []ranked
	var chunksByID map[int64]model.Chunk
	switch mode {
	case ModeKeyword:
		keyword = s.keywordPath(ctx, classID, query, limit+offset)
		chunksByID = loadChunks(ctx, s.store, classID, keyword)
	case ModeVector:
		var err error
		vectorHits, chunksByID, result.QueryVector, err = s.vectorPath(ctx, classID, query, limit+offset)
		if err != nil { return nil, err }
	case ModeHybrid:
		keyword = s.keywordPath(ctx, classID, query, limit+offset)
		var err error
		vectorHits, chunksByID, result.QueryVector, err = s.vectorPath(ctx, classID, query, limit+offset)
		if err != nil { return nil, err }
	}

	// 回表闭包在这里捕获班级编号：后续任何一次取正文、取标题都带同一个班级条件。
	lk := lookups{
		chunks: func(ctx context.Context, ids []int64) (map[int64]model.Chunk, error) {
			list, err := s.store.ChunksByIDs(ctx, classID, ids)
			if err != nil { return nil, err }
			out := make(map[int64]model.Chunk, len(list))
			for _, c := range list { out[c.ID] = c }
			return out, nil
		},
		titles: func(ctx context.Context, ids []int64) (map[int64]string, error) { return s.store.TitlesByMaterialIDs(ctx, classID, ids) },
	}
	hits := fuse(keyword, vectorHits, chunksByID, lk, fuseConfig{rrfK: s.cfg.RRFK, snippetChars: s.cfg.SnippetMaxChars, query: query})
	if len(hits) == 0 {
		result.Status = "no_evidence"
		result.Message = NoEvidenceMessage
		result.Hits = []Hit{}
		return result, nil
	}
	result.Total = len(hits)
	if offset >= len(hits) {
		result.Hits = []Hit{}
		return result, nil
	}
	end := offset + limit
	if end > len(hits) { end = len(hits) }
	page := hits[offset:end]
	for i := range page { page[i].Rank = offset + i + 1 }
	result.Hits = page
	result.Status = "ok"
	return result, nil
}

// ranked 是一条候选的排序信息：分数与名次来自各自的那条路径。
type ranked struct {
	id    int64
	score float64
	rank  int
}

func (s *Service) keywordPath(ctx context.Context, classID int64, query string, limit int) []ranked {
	rows, err := s.store.KeywordSearch(ctx, classID, query, limit)
	if err != nil { return nil }
	out := make([]ranked, 0, len(rows))
	for i, r := range rows { out = append(out, ranked{id: r.Chunk.ID, score: r.Score, rank: i + 1}) }
	return out
}

func (s *Service) vectorPath(ctx context.Context, classID int64, query string, limit int) ([]ranked, map[int64]model.Chunk, []float32, error) {
	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 { return nil, nil, nil, ErrVectorUnavailable }
	queryVector := vecs[0]
	matches, err := s.vectors.Search(ctx, classID, queryVector, s.cfg.VectorCandidates, s.cfg.VectorMinScore)
	if err != nil { return nil, nil, queryVector, ErrVectorUnavailable }
	ids := make([]int64, 0, len(matches))
	for _, m := range matches { ids = append(ids, m.ID) }
	chunks, err := s.store.ChunksByIDs(ctx, classID, ids)
	if err != nil { return nil, nil, queryVector, err }
	byID := make(map[int64]model.Chunk, len(chunks))
	for _, c := range chunks { byID[c.ID] = c }
	out := []ranked{}
	rank := 0
	for _, m := range matches {
		if _, ok := byID[m.ID]; !ok { continue } // 回表时班级条件再次核对，缺席即丢弃
		rank++
		if rank > limit { break }
		out = append(out, ranked{id: m.ID, score: m.Score, rank: rank})
	}
	return out, byID, queryVector, nil
}

func loadChunks(ctx context.Context, st *store.Store, classID int64, list []ranked) map[int64]model.Chunk {
	ids := make([]int64, 0, len(list))
	for _, r := range list { ids = append(ids, r.id) }
	chunks, err := st.ChunksByIDs(ctx, classID, ids)
	if err != nil { return map[int64]model.Chunk{} }
	byID := make(map[int64]model.Chunk, len(chunks))
	for _, c := range chunks { byID[c.ID] = c }
	return byID
}
