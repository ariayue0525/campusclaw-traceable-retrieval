// Package vector 封装 Qdrant 的集合管理与向量读写。
//
// 这里只保存定长浮点向量与若干标识（class_id / material_id / knowledge_entry_id /
// chunk_id / chunk_index），不保存任何正文。向量主键等于 knowledge_chunks.id，
// 检索时用同一个主键回 MySQL 取回 chunk_text。
package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrUnavailable 表示向量库当前不可用。关键字检索不依赖它，
// 向量与混合模式遇到它时应返回 503，而不是编造相似度分数。
var ErrUnavailable = errors.New("vector store unavailable")

// ErrDimensionMismatch 表示向量维度与集合定义不一致。
var ErrDimensionMismatch = errors.New("vector dimension does not match collection")

// Point 是写入向量库的一条记录。
type Point struct {
	ID       int64
	Vector   []float32
	ClassID  int64
	MaterialID int64
	EntryID  int64
	ChunkID  int64
	Index    int
}

// Match 是一次检索命中：主键、余弦分数与 payload 中的标识。
type Match struct {
	ID        int64
	Score     float64
	ClassID   int64
	MaterialID int64
	EntryID   int64
	ChunkID   int64
	Index     int
}

// Store 是 Qdrant 客户端。
type Store struct {
	baseURL    string
	collection string
	client     *http.Client
}

// New 构造客户端。baseURL 形如 http://qdrant:6333。
func New(baseURL, collection string, timeout time.Duration) *Store {
	return &Store{baseURL: strings.TrimRight(baseURL, "/"), collection: collection, client: &http.Client{Timeout: timeout}}
}

// Collection 返回集合名，供响应与日志说明。
func (s *Store) Collection() string { return s.collection }

// EnsureCollection 幂等地创建集合；已存在时若维度不一致则返回错误。
func (s *Store) EnsureCollection(ctx context.Context, dim int) error {
	var existing struct {
		Result struct {
			Config struct {
				Params struct {
					Vectors struct {
						Size     int    `json:"size"`
						Distance string `json:"distance"`
					} `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}
	body, status, err := s.do(ctx, http.MethodGet, "/collections/"+s.collection, nil)
	if err != nil { return err }
	if status == http.StatusOK {
		if err := json.Unmarshal(body, &existing); err == nil && existing.Result.Config.Params.Vectors.Size > 0 {
			if existing.Result.Config.Params.Vectors.Size != dim {
				return fmt.Errorf("%w: collection %s has size %d, embedding dim is %d", ErrDimensionMismatch, s.collection, existing.Result.Config.Params.Vectors.Size, dim)
			}
			return nil
		}
	}
	payload := map[string]any{"vectors": map[string]any{"size": dim, "distance": "Cosine"}}
	if _, _, err := s.do(ctx, http.MethodPut, "/collections/"+s.collection, payload); err != nil { return err }
	return nil
}

// Upsert 写入或更新若干向量点。
func (s *Store) Upsert(ctx context.Context, points []Point) error {
	if len(points) == 0 { return nil }
	items := make([]map[string]any, 0, len(points))
	for _, p := range points {
		items = append(items, map[string]any{
			"id":      p.ID,
			"vector":  p.Vector,
			"payload": map[string]any{
				"class_id":            p.ClassID,
				"material_id":         p.MaterialID,
				"knowledge_entry_id":  p.EntryID,
				"chunk_id":            p.ChunkID,
				"chunk_index":         p.Index,
			},
		})
	}
	_, _, err := s.do(ctx, http.MethodPut, "/collections/"+s.collection+"/points?wait=true", map[string]any{"points": items})
	return err
}

// Search 按班级过滤做余弦检索，返回分数不低于 minScore 的候选。
func (s *Store) Search(ctx context.Context, classID int64, query []float32, limit int, minScore float64) ([]Match, error) {
	if limit < 1 { limit = 10 }
	payload := map[string]any{
		"vector":          query,
		"limit":           limit,
		"with_payload":    true,
		"score_threshold": minScore,
		"filter": map[string]any{
			"must": []map[string]any{{"key": "class_id", "match": map[string]any{"value": classID}}},
		},
	}
	body, _, err := s.do(ctx, http.MethodPost, "/collections/"+s.collection+"/points/search", payload)
	if err != nil { return nil, err }
	var parsed struct {
		Result []struct {
			ID      json.RawMessage `json:"id"`
			Score   float64         `json:"score"`
			Payload struct {
				ClassID   int64 `json:"class_id"`
				MaterialID int64 `json:"material_id"`
				EntryID   int64 `json:"knowledge_entry_id"`
				ChunkID   int64 `json:"chunk_id"`
				Index     int   `json:"chunk_index"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil { return nil, fmt.Errorf("%w: bad search payload", ErrUnavailable) }
	matches := make([]Match, 0, len(parsed.Result))
	for _, r := range parsed.Result {
		id, err := parsePointID(r.ID)
		if err != nil { continue }
		if r.Score < minScore { continue }
		matches = append(matches, Match{
			ID: id, Score: r.Score,
			ClassID: r.Payload.ClassID, MaterialID: r.Payload.MaterialID,
			EntryID: r.Payload.EntryID, ChunkID: r.Payload.ChunkID, Index: r.Payload.Index,
		})
	}
	return matches, nil
}

// Retrieve 读取若干主键的完整向量与 payload，用于页面上的两库对照。
func (s *Store) Retrieve(ctx context.Context, ids []int64) (map[int64]Retrieved, error) {
	if len(ids) == 0 { return map[int64]Retrieved{}, nil }
	body, _, err := s.do(ctx, http.MethodPost, "/collections/"+s.collection+"/points", map[string]any{"ids": ids, "with_payload": true, "with_vector": true})
	if err != nil { return nil, err }
	var parsed struct {
		Result []struct {
			ID      json.RawMessage `json:"id"`
			Vector  []float32       `json:"vector"`
			Payload struct {
				ClassID   int64 `json:"class_id"`
				MaterialID int64 `json:"material_id"`
				EntryID   int64 `json:"knowledge_entry_id"`
				ChunkID   int64 `json:"chunk_id"`
				Index     int   `json:"chunk_index"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil { return nil, fmt.Errorf("%w: bad retrieve payload", ErrUnavailable) }
	out := make(map[int64]Retrieved, len(parsed.Result))
	for _, r := range parsed.Result {
		id, err := parsePointID(r.ID)
		if err != nil { continue }
		out[id] = Retrieved{
			ID: id, Vector: r.Vector,
			ClassID: r.Payload.ClassID, MaterialID: r.Payload.MaterialID,
			EntryID: r.Payload.EntryID, ChunkID: r.Payload.ChunkID, Index: r.Payload.Index,
		}
	}
	return out, nil
}

// Retrieved 是读取到的完整向量记录。
type Retrieved struct {
	ID         int64
	Vector     []float32
	ClassID    int64
	MaterialID int64
	EntryID    int64
	ChunkID    int64
	Index      int
}

// Delete 删除若干主键对应的向量点。
func (s *Store) Delete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 { return nil }
	_, _, err := s.do(ctx, http.MethodPost, "/collections/"+s.collection+"/points/delete?wait=true", map[string]any{"points": ids})
	return err
}

func parsePointID(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 { return 0, errors.New("empty id") }
	text := strings.Trim(string(raw), `"`)
	return strconv.ParseInt(text, 10, 64)
}

// do 执行请求并区分「网络/5xx 故障」与业务错误。
func (s *Store) do(ctx context.Context, method, path string, payload any) ([]byte, int, error) {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil { return nil, 0, err }
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil { return nil, 0, err }
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil { return nil, 0, fmt.Errorf("%w: %v", ErrUnavailable, err) }
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil { return nil, resp.StatusCode, fmt.Errorf("%w: %v", ErrUnavailable, err) }
	if resp.StatusCode >= 500 {
		return nil, resp.StatusCode, fmt.Errorf("%w: qdrant returned %d", ErrUnavailable, resp.StatusCode)
	}
	return body, resp.StatusCode, nil
}
