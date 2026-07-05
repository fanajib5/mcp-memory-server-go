package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaClientEmbed(t *testing.T) {
	// Fake Ollama server returning a fixed 2-dim embedding per text.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"test","embeddings":[[0.1,0.2],[0.3,0.4]]}`))
	}))
	defer srv.Close()

	c := NewOllamaClient(srv.URL, "test-model")
	vecs, err := c.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vecs))
	}
	if len(vecs[0]) != 2 || vecs[0][0] != 0.1 {
		t.Fatalf("vec[0] = %v, want [0.1, 0.2]", vecs[0])
	}
}

func TestOllamaClientEmptyInput(t *testing.T) {
	c := NewOllamaClient("http://unused", "test")
	vecs, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("embed empty: %v", err)
	}
	if vecs != nil {
		t.Fatalf("expected nil for empty input, got %v", vecs)
	}
}

func TestOllamaClientServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	c := NewOllamaClient(srv.URL, "bad-model")
	_, err := c.Embed(context.Background(), []string{"hi"})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestNoopEmbedder(t *testing.T) {
	n := NoopEmbedder()
	vecs, err := n.Embed(context.Background(), []string{"anything"})
	if err != nil {
		t.Fatalf("noop embed: %v", err)
	}
	if vecs != nil {
		t.Fatalf("noop should return nil, got %v", vecs)
	}
}
