// Package gateway isolates external service integrations (embedding models,
// future cloud backup, etc.) behind small interfaces so the rest of the app
// depends on abstractions, not concrete clients.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder converts text into fixed-dimensional vectors suitable for
// cosine-similarity search. Implementations must be safe for concurrent use.
// A nil Embedder means "semantic search disabled" — callers treat absence as
// lexical-only mode.
type Embedder interface {
	// Embed turns each input text into a vector. The returned slice is
	// parallel to texts: out[i] is the embedding of texts[i]. An empty input
	// yields an empty output (no API call).
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbeddingDim is the vector dimensionality produced by the default model
// (nomic-embed-text). Schema vector(N) must match this.
const EmbeddingDim = 768

// ollamaRequest is the JSON body for POST /api/embed.
type ollamaRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// ollamaResponse is the JSON body returned by POST /api/embed.
type ollamaResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// OllamaClient talks to a local Ollama instance's /api/embed endpoint.
// It reuses a single *http.Client (connection-pooled) and is safe for
// concurrent use.
type OllamaClient struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaClient builds an embedder against baseURL (e.g. "http://ollama:11434")
// using model (e.g. "nomic-embed-text"). The HTTP client has a generous default
// timeout; per-call deadlines should be set via the context.
func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed sends texts to Ollama in a single batch request and returns the
// parallel vector slice. Empty input returns empty output without a call.
func (c *OllamaClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(ollamaRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed: HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: expected %d vectors, got %d", len(texts), len(out.Embeddings))
	}
	return out.Embeddings, nil
}

// noopEmbedder is an Embedder that never produces vectors. Used when Ollama
// is not configured (semantic search off). Embed always returns nil,nil.
type noopEmbedder struct{}

func (noopEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

// NoopEmbedder returns an Embedder that disables semantic features.
func NoopEmbedder() Embedder { return noopEmbedder{} }
