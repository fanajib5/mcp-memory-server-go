// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"context"
	"testing"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/gateway"
)

// orthogonalEmbedder returns mutually orthogonal unit vectors for query texts
// vs stored texts, so semantic similarity is always 0 (< 0.2 threshold) and
// hybrid search must rely on its lexical path to match.
type orthogonalEmbedder struct {
	queryTexts map[string]bool
}

func (e orthogonalEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, gateway.EmbeddingDim)
		if e.queryTexts[t] {
			v[1] = 1
		} else {
			v[0] = 1
		}
		out[i] = v
	}
	return out, nil
}

// TestSearchIdentifierForms covers identifier-style queries whose lexemes do
// not round-trip through to_tsvector tokenization: suffixed terms ("pg_hba" vs
// stored "pg_hba.conf"), hyphen/underscore variants ("embedded-postgres" vs
// stored "embedded_postgres"), and dotted names ("Sekoolah.id").
func TestSearchIdentifierForms(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	seed := []entity.EntityInput{{
		Name: "PostgresNotes",
		Type: "concept",
		Observations: []string{
			"config pg_hba.conf file controls auth",
			"uses embedded_postgres at 55432 for tests",
			"entity Sekoolah.id done",
		},
	}}
	if _, err := h.mem.CreateEntities(ctx, "ftsproj", seed); err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, q := range []string{"pg_hba", "embedded-postgres", "55432", "Sekoolah.id"} {
		hits, err := h.mem.Search(ctx, "ftsproj", q, 10)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if len(hits) != 1 || hits[0].Name != "PostgresNotes" {
			t.Errorf("search %q = %+v, want single hit PostgresNotes", q, hits)
		}
	}
}

// TestSearchIdentifierFormsHybrid runs the same identifier matrix through the
// hybrid path (embedder present) with semantic similarity pinned to 0, proving
// the lexical matcher inside hybrid search handles identifier forms too.
func TestSearchIdentifierFormsHybrid(t *testing.T) {
	queries := []string{"pg_hba", "embedded-postgres", "55432", "Sekoolah.id"}
	qset := map[string]bool{}
	for _, q := range queries {
		qset[q] = true
	}
	h := newTestHarnessWithEmbedder(t, orthogonalEmbedder{queryTexts: qset})
	defer h.close()
	ctx := context.Background()

	if _, err := h.mem.CreateEntities(ctx, "ftsproj", []entity.EntityInput{{
		Name: "PostgresNotes",
		Type: "concept",
		Observations: []string{
			"config pg_hba.conf file controls auth",
			"uses embedded_postgres at 55432 for tests",
			"entity Sekoolah.id done",
		},
	}}); err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, q := range queries {
		hits, err := h.mem.Search(ctx, "ftsproj", q, 10)
		if err != nil {
			t.Fatalf("hybrid search %q: %v", q, err)
		}
		if len(hits) != 1 || hits[0].Name != "PostgresNotes" {
			t.Errorf("hybrid search %q = %+v, want single hit PostgresNotes", q, hits)
		}
	}
}

// TestSearchCrossProject covers the silent defaultProject fallback: a blank
// project must search all namespaces and tag every hit with its project, while
// an explicit project keeps scoping. No hits yields an empty slice, not nil.
func TestSearchCrossProject(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	if _, err := h.mem.CreateEntities(ctx, "nsA", []entity.EntityInput{
		{Name: "Alpha", Type: "concept", Observations: []string{"mentions watermarking"}},
	}); err != nil {
		t.Fatalf("create nsA: %v", err)
	}
	if _, err := h.mem.CreateEntities(ctx, "nsB", []entity.EntityInput{
		{Name: "Beta", Type: "concept", Observations: []string{"mentions watermarking"}},
	}); err != nil {
		t.Fatalf("create nsB: %v", err)
	}

	all, err := h.mem.Search(ctx, "", "watermarking", 10)
	if err != nil {
		t.Fatalf("search all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("search all = %+v, want 2 hits (nsA + nsB)", all)
	}
	gotProjects := map[string]string{}
	for _, r := range all {
		gotProjects[r.Name] = r.Project
	}
	if gotProjects["Alpha"] != "nsA" || gotProjects["Beta"] != "nsB" {
		t.Errorf("project tags = %v, want Alpha->nsA, Beta->nsB", gotProjects)
	}

	scoped, err := h.mem.Search(ctx, "nsA", "watermarking", 10)
	if err != nil {
		t.Fatalf("search nsA: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Name != "Alpha" || scoped[0].Project != "nsA" {
		t.Errorf("search nsA = %+v, want single hit Alpha in nsA", scoped)
	}

	miss, err := h.mem.Search(ctx, "nsC", "watermarking", 10)
	if err != nil {
		t.Fatalf("search nsC: %v", err)
	}
	if miss == nil || len(miss) != 0 {
		t.Errorf("search nsC = %#v, want empty non-nil slice", miss)
	}
}
