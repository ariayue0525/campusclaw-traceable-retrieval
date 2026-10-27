package vector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpsertSendsIdentifiersOnly(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":{"operation_id":0,"status":"completed"}}`))
	}))
	defer server.Close()
	s := New(server.URL, "campusclaw_chunks", 0)
	err := s.Upsert(context.Background(), []Point{{ID: 7, Vector: []float32{0.1, 0.2}, ClassID: 1, MaterialID: 2, EntryID: 3, ChunkID: 7, Index: 0}})
	if err != nil { t.Fatal(err) }
	points := captured["points"].([]any)[0].(map[string]any)
	payload := points["payload"].(map[string]any)
	for _, key := range []string{"class_id", "material_id", "knowledge_entry_id", "chunk_id", "chunk_index"} {
		if _, ok := payload[key]; !ok { t.Fatalf("payload missing %s", key) }
	}
	if _, ok := payload["chunk_text"]; ok { t.Fatal("payload must not carry chunk text") }
	if payload["chunk_id"] != float64(7) { t.Fatalf("chunk_id must equal point id, got %v", payload["chunk_id"]) }
}

func TestSearchFiltersByClassAndThreshold(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte(`{"result":[
			{"id":11,"score":0.82,"payload":{"class_id":1,"material_id":2,"knowledge_entry_id":3,"chunk_id":11,"chunk_index":0}},
			{"id":12,"score":0.10,"payload":{"class_id":1,"material_id":2,"knowledge_entry_id":3,"chunk_id":12,"chunk_index":1}}
		]}`))
	}))
	defer server.Close()
	s := New(server.URL, "campusclaw_chunks", 0)
	matches, err := s.Search(context.Background(), 1, []float32{0.1, 0.2}, 10, 0.35)
	if err != nil { t.Fatal(err) }
	if len(matches) != 1 || matches[0].ID != 11 { t.Fatalf("expected only the high scoring match, got %+v", matches) }
	filter := captured["filter"].(map[string]any)["must"].([]any)[0].(map[string]any)
	if filter["key"] != "class_id" { t.Fatalf("search must filter by class_id: %+v", filter) }
	if captured["score_threshold"] != 0.35 { t.Fatalf("expected threshold to be sent, got %v", captured["score_threshold"]) }
}

func TestSearchScopedFiltersMaterialBeforeRanking(t *testing.T){
	var captured map[string]any
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_,_ = w.Write([]byte(`{"result":[]}`))
	}));defer server.Close()
	s:=New(server.URL,"chunks",0)
	if _,err:=s.SearchScoped(context.Background(),1,[]int64{10,11},[]float32{1},4,0.2);err!=nil{t.Fatal(err)}
	must:=captured["filter"].(map[string]any)["must"].([]any)
	if len(must)!=2{t.Fatalf("expected class and material filters: %+v",must)}
	material:=must[1].(map[string]any)
	if material["key"]!="material_id"{t.Fatalf("missing material filter: %+v",material)}
	anyIDs:=material["match"].(map[string]any)["any"].([]any)
	if len(anyIDs)!=2||anyIDs[0]!=float64(10)||anyIDs[1]!=float64(11){t.Fatalf("wrong allow-list %+v",anyIDs)}
}

func TestSearchReportsUnavailableOnServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	s := New(server.URL, "campusclaw_chunks", 0)
	if _, err := s.Search(context.Background(), 1, []float32{0.1}, 10, 0.35); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestEnsureCollectionRejectsDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":1536,"distance":"Cosine"}}}}}`))
	}))
	defer server.Close()
	s := New(server.URL, "campusclaw_chunks", 0)
	if err := s.EnsureCollection(context.Background(), 256); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("expected ErrDimensionMismatch, got %v", err)
	}
}

func TestEnsureCollectionCreatesCollection(t *testing.T) {
	var path, method string
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		if r.Body != nil { _ = json.NewDecoder(r.Body).Decode(&captured) }
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":true}`))
	}))
	defer server.Close()
	s := New(server.URL, "campusclaw_chunks", 0)
	if err := s.EnsureCollection(context.Background(), 256); err != nil { t.Fatal(err) }
	if method != http.MethodPut || !strings.HasSuffix(path, "/collections/campusclaw_chunks") { t.Fatalf("unexpected %s %s", method, path) }
	vectors := captured["vectors"].(map[string]any)
	if vectors["distance"] != "Cosine" || vectors["size"] != float64(256) { t.Fatalf("unexpected collection params %+v", vectors) }
}

func TestDeleteSendsPointIds(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":{"status":"completed"}}`))
	}))
	defer server.Close()
	s := New(server.URL, "campusclaw_chunks", 0)
	if err := s.Delete(context.Background(), []int64{4, 5}); err != nil { t.Fatal(err) }
	points := captured["points"].([]any)
	if len(points) != 2 { t.Fatalf("expected two ids, got %+v", points) }
}
