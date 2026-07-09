// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"context"
	"testing"

	"mcp-memory-server/internal/entity"
)

// TestProjectIsolationAndRoundTrip covers project isolation, relation-type
// normalization, and export/import round-trip end-to-end.
func TestProjectIsolationAndRoundTrip(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	if _, err := h.mem.CreateEntities(ctx, "projA", []entity.EntityInput{
		{Name: "X", Type: "project", Observations: []string{"uses pg"}},
		{Name: "Y", Type: "tool"},
	}); err != nil {
		t.Fatalf("create projA: %v", err)
	}
	if _, err := h.mem.CreateEntities(ctx, "projB", []entity.EntityInput{
		{Name: "X", Type: "project"},
	}); err != nil {
		t.Fatalf("create projB: %v", err)
	}

	aHits, err := h.mem.Search(ctx, "projA", "pg", 10)
	if err != nil {
		t.Fatalf("search projA: %v", err)
	}
	if len(aHits) != 1 || aHits[0].Name != "X" {
		t.Fatalf("search projA = %+v, want single hit X", aHits)
	}
	bHits, err := h.mem.Search(ctx, "projB", "pg", 10)
	if err != nil {
		t.Fatalf("search projB: %v", err)
	}
	if len(bHits) != 0 {
		t.Fatalf("search projB = %+v, want no hits (isolation)", bHits)
	}

	// Relation type is normalized: "depends on" -> DEPENDS_ON.
	if _, err := h.mem.CreateRelations(ctx, "projA", []entity.RelationInput{
		{From: "X", To: "Y", RelationType: "depends on"},
	}); err != nil {
		t.Fatalf("relations: %v", err)
	}
	var rt string
	if err := h.pool.QueryRow(ctx, `SELECT relation_type FROM memory_relations`).Scan(&rt); err != nil {
		t.Fatalf("read relation: %v", err)
	}
	if rt != "DEPENDS_ON" {
		t.Fatalf("relation_type = %q, want DEPENDS_ON", rt)
	}

	payload, err := h.mem.Export(ctx, "projA")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(payload.Entities) != 2 || len(payload.Relations) != 1 {
		t.Fatalf("export = %+v, want 2 entities + 1 relation", payload)
	}
	if err := h.mem.DeleteEntities(ctx, "projA", []string{"X", "Y"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	res, err := h.mem.Import(ctx, "projA", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.EntitiesCreated != 2 || res.RelationsCreated != 1 {
		t.Fatalf("import result = %+v, want 2 entities + 1 relation", res)
	}
	restored, err := h.mem.Search(ctx, "projA", "pg", 10)
	if err != nil {
		t.Fatalf("search after import: %v", err)
	}
	if len(restored) != 1 || restored[0].Name != "X" {
		t.Fatalf("search after import = %+v, want X restored", restored)
	}
}

// TestSearchAggregatesAcrossObservations locks in the fix for multi-word search:
// query terms live in DIFFERENT observations of the same entity.
func TestSearchAggregatesAcrossObservations(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	if _, err := h.mem.CreateEntities(ctx, "default", []entity.EntityInput{
		{
			Name: "Toko Contoh",
			Type: "project",
			Observations: []string{
				"sebuah proyek rintisan",
				"bergerak di bidang bisnis kuliner",
			},
		},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	hits, err := h.mem.Search(ctx, "default", "proyek bisnis", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Name != "Toko Contoh" {
		t.Fatalf("search 'proyek bisnis' = %+v, want single hit 'Toko Contoh'", hits)
	}

	hits2, err := h.mem.Search(ctx, "default", "kuliner", 10)
	if err != nil {
		t.Fatalf("search2: %v", err)
	}
	if len(hits2) != 1 || hits2[0].Name != "Toko Contoh" {
		t.Fatalf("search 'kuliner' = %+v, want single hit 'Toko Contoh'", hits2)
	}
}
