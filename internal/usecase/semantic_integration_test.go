// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"context"
	"hash/fnv"
	"testing"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/gateway"
)

// hashEmbedder produces a deterministic 1024-dim vector from text via FNV hash.
// Texts with similar words hash to similar-ish buckets — not real semantics,
// but deterministic and sufficient to verify the embedding storage + hybrid
// query path runs without error.
type hashEmbedder struct{}

func (hashEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, gateway.EmbeddingDim)
		h := fnv.New64a()
		h.Write([]byte(t))
		seed := h.Sum64()
		for j := range v {
			seed = seed*6364136223846793005 + 1442695040888963407
			v[j] = float32(seed%1000) / 1000.0
		}
		out[i] = v
	}
	return out, nil
}

// TestEmbedOnWriteStored verifies embeddings are stored during AddObservations.
func TestEmbedOnWriteStored(t *testing.T) {
	h := newTestHarnessWithEmbedder(t, hashEmbedder{})
	defer h.close()
	ctx := context.Background()

	h.mem.AddObservations(ctx, "embproj", "A", []string{"embedded fact"}, nil)

	var embCount int
	err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM memory_observations o JOIN memory_entities e ON e.id=o.entity_id WHERE e.name='A' AND o.embedding IS NOT NULL`).Scan(&embCount)
	if err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	if embCount != 1 {
		t.Fatalf("embedded obs count = %d, want 1", embCount)
	}
}

// TestSemanticSearchNoError verifies the hybrid query path runs without error
// when queryVec is provided (embedder active). It doesn't assert semantic
// ranking quality (that needs a real model) — just that the SQL path works.
func TestSemanticSearchNoError(t *testing.T) {
	h := newTestHarnessWithEmbedder(t, hashEmbedder{})
	defer h.close()
	ctx := context.Background()

	h.mem.AddObservations(ctx, "semproj", "A", []string{"semantic test content"}, nil)
	h.mem.AddObservations(ctx, "semproj", "B", []string{"unrelated stuff"}, nil)

	results, err := h.mem.Search(ctx, "semproj", "semantic", 10)
	if err != nil {
		t.Fatalf("semantic search error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("semantic search returned no results")
	}
}

// TestSearchFallbackNilEmbedder verifies that search works in lexical-only
// mode when embedder is nil (Ollama not configured).
func TestSearchFallbackNilEmbedder(t *testing.T) {
	h := newTestHarnessWithEmbedder(t, nil)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "fallbackproj", []entity.EntityInput{
		{Name: "X", Type: "project", Observations: []string{"fallback keyword"}},
	})
	results, err := h.mem.Search(ctx, "fallbackproj", "fallback", 10)
	if err != nil {
		t.Fatalf("lexical fallback search: %v", err)
	}
	if len(results) != 1 || results[0].Name != "X" {
		t.Fatalf("fallback results = %+v", results)
	}
}

// TestBackfillEmbedsExisting verifies that UpdateObservation can add embeddings
// to rows that previously had none (simulating the backfill path).
func TestBackfillEmbedsExisting(t *testing.T) {
	h := newTestHarnessWithEmbedder(t, nil) // start with nil embedder
	defer h.close()
	ctx := context.Background()

	// Create observation without embedding (nil embedder).
	h.mem.CreateEntities(ctx, "bfproj", []entity.EntityInput{
		{Name: "B", Type: "project", Observations: []string{"no embedding yet"}},
	})
	var obsID int
	h.pool.QueryRow(ctx,
		`SELECT o.id FROM memory_observations o JOIN memory_entities e ON e.id=o.entity_id WHERE e.name='B'`).Scan(&obsID)

	// Verify NULL before backfill.
	var nullCount int
	h.pool.QueryRow(ctx, `SELECT count(*) FROM memory_observations WHERE id=$1 AND embedding IS NULL`, obsID).Scan(&nullCount)
	if nullCount != 1 {
		t.Fatalf("expected NULL embedding before backfill, got count=%d", nullCount)
	}

	// Now switch to an embedder and re-embed via UpdateObservation.
	h.mem.embedder = hashEmbedder{}
	if err := h.mem.UpdateObservation(ctx, "bfproj", obsID, "now embedded", nil); err != nil {
		t.Fatalf("backfill update: %v", err)
	}

	var embCount int
	h.pool.QueryRow(ctx, `SELECT count(*) FROM memory_observations WHERE id=$1 AND embedding IS NOT NULL`, obsID).Scan(&embCount)
	if embCount != 1 {
		t.Fatalf("expected non-NULL embedding after backfill, got count=%d", embCount)
	}
}
