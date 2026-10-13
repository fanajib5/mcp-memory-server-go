// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"context"
	"log"
	"time"

	"mcp-memory-server/internal/event"
	"mcp-memory-server/internal/gateway"
	"mcp-memory-server/internal/repository"
)

// embedTimeout caps how long a single embed call may take. If the embedder is
// slow or down, the usecase stores the observation without an embedding and
// continues — semantic search is best-effort, not blocking.
const embedTimeout = 5 * time.Second

// clampConfidence forces a confidence value into [0.0, 1.0].
func clampConfidence(c float64) float64 {
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

// clampConfidences applies clampConfidence to every value in place; returns the
// same slice. A nil/empty slice means "no confidence provided" (all neutral).
func clampConfidences(c []float64) []float64 {
	for i := range c {
		c[i] = clampConfidence(c[i])
	}
	return c
}

// MemoryUseCase applies domain rules (project/entity/relation normalization,
// validation, defaults) before delegating to the repository. It is the seam
// where future cross-cutting memory features (decay scoring, confidence,
// versioning) will hook in without touching SQL.
type MemoryUseCase struct {
	repo     repository.MemoryRepository
	embedder gateway.Embedder // nil = semantic search off
	bus      *event.Bus       // nil = events disabled
}

// NewMemoryUseCase wires a usecase to its repository. Pass nil for embedder to
// disable semantic features (lexical-only mode). Pass nil for bus to disable
// SSE event publishing.
func NewMemoryUseCase(repo repository.MemoryRepository, embedder gateway.Embedder, bus *event.Bus) *MemoryUseCase {
	return &MemoryUseCase{repo: repo, embedder: embedder, bus: bus}
}

// publish emits an event to the bus if configured. No-op when bus is nil.
func (u *MemoryUseCase) publish(project, entity, eventType string) {
	if u.bus == nil {
		return
	}
	u.bus.Publish(project, event.Event{Type: eventType, Entity: entity})
}

// embedTexts is a fail-safe wrapper: embeds texts with a timeout. On any error
// (timeout, network, Ollama down), returns nil vectors and logs a warning so
// the write/search proceeds in lexical-only mode.
func (u *MemoryUseCase) embedTexts(ctx context.Context, texts []string) [][]float32 {
	if u.embedder == nil || len(texts) == 0 {
		return nil
	}
	embCtx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()
	vecs, err := u.embedder.Embed(embCtx, texts)
	if err != nil {
		log.Printf("embed failed (non-fatal, semantic off for this op): %v", err)
		return nil
	}
	return vecs
}
