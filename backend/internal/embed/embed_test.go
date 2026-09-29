package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalEmbeddingIsDeterministicAndNormalized(t *testing.T) {
	e := NewLocal(64)
	ctx := context.Background()
	first, err := e.Embed(ctx, []string{"课间不要在走廊追逐打闹"})
	if err != nil { t.Fatal(err) }
	again, err := e.Embed(ctx, []string{"课间不要在走廊追逐打闹"})
	if err != nil { t.Fatal(err) }
	if len(first[0]) != 64 { t.Fatalf("unexpected dim %d", len(first[0])) }
	for i := range first[0] { if first[0][i] != again[0][i] { t.Fatal("local embedding must be deterministic") } }
	if n := Norm(first[0]); n < 0.999 || n > 1.001 { t.Fatalf("expected unit vector, got %f", n) }
}

func TestLocalSimilarityOrdersRelatedTextHigher(t *testing.T) {
	e := NewLocal(256)
	ctx := context.Background()
	vecs, err := e.Embed(ctx, []string{
		"课间休息时间不要在走廊里追逐打闹，也不要大声喧哗。",
		"课间休息时不要在走廊追逐打闹",
		"明天的股票市场行情预计上涨。",
	})
	if err != nil { t.Fatal(err) }
	near := Cosine(vecs[0], vecs[1])
	far := Cosine(vecs[0], vecs[2])
	if near <= far { t.Fatalf("related text should score higher: near=%f far=%f", near, far) }
	if near < 0.35 { t.Fatalf("related text should clear the 0.35 threshold, got %f", near) }
	if far > 0.35 { t.Logf("unrelated text scored %f, above default threshold", far) }
}

func TestGatewayEmbeddingParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" { t.Fatalf("unexpected path %s", r.URL.Path) }
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" { t.Fatalf("missing bearer token: %q", got) }
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0.1,0.2,0.3]},{"index":0,"embedding":[0.3,0.4,0.5]}]}`))
	}))
	defer server.Close()
	g := NewGateway(server.URL, "test-key", "text-embedding-3-small", 3, 0)
	got, err := g.Embed(context.Background(), []string{"b", "a"})
	if err != nil { t.Fatal(err) }
	if len(got) != 2 || got[0][0] != 0.3 || got[1][2] != 0.3 { t.Fatalf("embeddings must be ordered by index: %+v", got) }
	if g.Kind() != "gateway" { t.Fatalf("unexpected kind %s", g.Kind()) }
}

func TestGatewayDimensionMismatchFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()
	g := NewGateway(server.URL, "test-key", "m", 8, 0)
	if _, err := g.Embed(context.Background(), []string{"a"}); err == nil { t.Fatal("expected dimension mismatch to fail") }
}

func TestGatewayErrorIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	g := NewGateway(server.URL, "test-key", "m", 8, 0)
	_, err := g.Embed(context.Background(), []string{"a"})
	if err == nil { t.Fatal("expected error") }
	if !errorsIs(err, ErrUnavailable) { t.Fatalf("expected ErrUnavailable, got %v", err) }
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target { return true }
		u, ok := err.(interface{ Unwrap() error })
		if !ok { return false }
		err = u.Unwrap()
	}
	return false
}
