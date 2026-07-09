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

func TestCRUDOperations(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	if _, err := h.mem.CreateEntities(ctx, "crudproj", []entity.EntityInput{
		{Name: "Alpha", Type: "project", Observations: []string{"obs one"}},
		{Name: "Beta", Type: "concept"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := h.mem.CreateRelations(ctx, "crudproj", []entity.RelationInput{
		{From: "Alpha", To: "Beta", RelationType: "depends on"},
	}); err != nil {
		t.Fatalf("rel: %v", err)
	}
	detail, err := h.stats.GetEntityDetail(ctx, "crudproj", "Alpha")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	obsID := detail.Observations[0].ID
	relID := detail.Relations[0].ID

	if err := h.mem.UpdateEntity(ctx, "crudproj", "Alpha", "AlphaRenamed", "tool"); err != nil {
		t.Fatalf("update entity: %v", err)
	}
	d2, _ := h.stats.GetEntityDetail(ctx, "crudproj", "AlphaRenamed")
	if d2 == nil || d2.Type != "tool" {
		t.Fatalf("after update, got %+v", d2)
	}

	if err := h.mem.UpdateEntity(ctx, "crudproj", "AlphaRenamed", "Beta", "tool"); err == nil {
		t.Fatal("expected collision error, got nil")
	}

	if err := h.mem.UpdateObservation(ctx, "crudproj", obsID, "edited obs", nil); err != nil {
		t.Fatalf("update obs: %v", err)
	}
	d3, _ := h.stats.GetEntityDetail(ctx, "crudproj", "AlphaRenamed")
	if d3.Observations[0].Content != "edited obs" {
		t.Fatalf("obs content = %q", d3.Observations[0].Content)
	}

	if err := h.mem.DeleteObservation(ctx, "crudproj", obsID); err != nil {
		t.Fatalf("delete obs: %v", err)
	}
	d4, _ := h.stats.GetEntityDetail(ctx, "crudproj", "AlphaRenamed")
	if len(d4.Observations) != 0 {
		t.Fatalf("obs not deleted: %+v", d4.Observations)
	}

	if err := h.mem.DeleteRelation(ctx, "crudproj", relID); err != nil {
		t.Fatalf("delete rel: %v", err)
	}
	d5, _ := h.stats.GetEntityDetail(ctx, "crudproj", "AlphaRenamed")
	if len(d5.Relations) != 0 {
		t.Fatalf("rel not deleted: %+v", d5.Relations)
	}

	if _, err := h.mem.CreateEntities(ctx, "otherproj", []entity.EntityInput{
		{Name: "Gamma", Type: "tool", Observations: []string{"other obs"}},
	}); err != nil {
		t.Fatalf("create gamma: %v", err)
	}
	g, _ := h.stats.GetEntityDetail(ctx, "otherproj", "Gamma")
	otherObsID := g.Observations[0].ID
	if err := h.mem.DeleteObservation(ctx, "crudproj", otherObsID); err == nil {
		t.Fatal("cross-project observation delete must fail")
	}
}

func TestEditByContentAndTriple(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "editproj", []entity.EntityInput{
		{Name: "Delta", Type: "project", Observations: []string{"keep this", "drop this"}},
		{Name: "Echo", Type: "concept"},
	})
	h.mem.CreateRelations(ctx, "editproj", []entity.RelationInput{{From: "Delta", To: "Echo", RelationType: "uses"}})

	if err := h.mem.UpdateEntity(ctx, "editproj", "Delta", "DeltaRenamed", "tool"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if err := h.mem.UpdateObservationByContent(ctx, "editproj", "DeltaRenamed", "keep this", "kept this", nil); err != nil {
		t.Fatalf("update obs: %v", err)
	}
	d, _ := h.stats.GetEntityDetail(ctx, "editproj", "DeltaRenamed")
	seen := false
	for _, o := range d.Observations {
		if o.Content == "kept this" {
			seen = true
		}
		if o.Content == "keep this" {
			t.Fatal("old content still present after update")
		}
	}
	if !seen {
		t.Fatal("updated content missing")
	}

	if err := h.mem.DeleteObservationByContent(ctx, "editproj", "DeltaRenamed", "drop this"); err != nil {
		t.Fatalf("delete obs: %v", err)
	}
	d2, _ := h.stats.GetEntityDetail(ctx, "editproj", "DeltaRenamed")
	for _, o := range d2.Observations {
		if o.Content == "drop this" {
			t.Fatal("deleted content still present")
		}
	}

	if err := h.mem.DeleteRelationByTriple(ctx, "editproj", "DeltaRenamed", "Echo", "uses"); err != nil {
		t.Fatalf("delete rel: %v", err)
	}
	d3, _ := h.stats.GetEntityDetail(ctx, "editproj", "DeltaRenamed")
	if len(d3.Relations) != 0 {
		t.Fatalf("relation not deleted: %+v", d3.Relations)
	}

	if err := h.mem.DeleteRelationByTriple(ctx, "editproj", "DeltaRenamed", "Echo", "uses"); err == nil {
		t.Fatal("expected error deleting absent relation")
	}
}
