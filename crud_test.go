package main

import (
	"context"
	"testing"
)

func TestCRUDOperations(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	if _, err := CreateEntities(ctx, pool, "crudproj", []EntityInput{
		{Name: "Alpha", EntityType: "project", Observations: []string{"obs one"}},
		{Name: "Beta", EntityType: "concept"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := CreateRelations(ctx, pool, "crudproj", []RelationInput{
		{From: "Alpha", To: "Beta", RelationType: "depends on"},
	}); err != nil {
		t.Fatalf("rel: %v", err)
	}
	detail, err := GetEntityDetail(ctx, pool, "crudproj", "Alpha")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	obsID := detail.Observations[0].ID
	relID := detail.Relations[0].ID

	if err := UpdateEntity(ctx, pool, "crudproj", "Alpha", "AlphaRenamed", "tool"); err != nil {
		t.Fatalf("update entity: %v", err)
	}
	d2, _ := GetEntityDetail(ctx, pool, "crudproj", "AlphaRenamed")
	if d2 == nil || d2.Type != "tool" {
		t.Fatalf("after update, got %+v", d2)
	}

	if err := UpdateEntity(ctx, pool, "crudproj", "AlphaRenamed", "Beta", "tool"); err == nil {
		t.Fatal("expected collision error, got nil")
	}

	if err := UpdateObservation(ctx, pool, "crudproj", obsID, "edited obs"); err != nil {
		t.Fatalf("update obs: %v", err)
	}
	d3, _ := GetEntityDetail(ctx, pool, "crudproj", "AlphaRenamed")
	if d3.Observations[0].Content != "edited obs" {
		t.Fatalf("obs content = %q", d3.Observations[0].Content)
	}

	if err := DeleteObservation(ctx, pool, "crudproj", obsID); err != nil {
		t.Fatalf("delete obs: %v", err)
	}
	d4, _ := GetEntityDetail(ctx, pool, "crudproj", "AlphaRenamed")
	if len(d4.Observations) != 0 {
		t.Fatalf("obs not deleted: %+v", d4.Observations)
	}

	if err := DeleteRelation(ctx, pool, "crudproj", relID); err != nil {
		t.Fatalf("delete rel: %v", err)
	}
	d5, _ := GetEntityDetail(ctx, pool, "crudproj", "AlphaRenamed")
	if len(d5.Relations) != 0 {
		t.Fatalf("rel not deleted: %+v", d5.Relations)
	}

	if _, err := CreateEntities(ctx, pool, "otherproj", []EntityInput{
		{Name: "Gamma", EntityType: "tool", Observations: []string{"other obs"}},
	}); err != nil {
		t.Fatalf("create gamma: %v", err)
	}
	g, _ := GetEntityDetail(ctx, pool, "otherproj", "Gamma")
	otherObsID := g.Observations[0].ID
	if err := DeleteObservation(ctx, pool, "crudproj", otherObsID); err == nil {
		t.Fatal("cross-project observation delete must fail")
	}
}

func TestEditByContentAndTriple(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	CreateEntities(ctx, pool, "editproj", []EntityInput{
		{Name: "Delta", EntityType: "project", Observations: []string{"keep this", "drop this"}},
		{Name: "Echo", EntityType: "concept"},
	})
	CreateRelations(ctx, pool, "editproj", []RelationInput{{From: "Delta", To: "Echo", RelationType: "uses"}})

	if err := UpdateEntity(ctx, pool, "editproj", "Delta", "DeltaRenamed", "tool"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if err := UpdateObservationByContent(ctx, pool, "editproj", "DeltaRenamed", "keep this", "kept this"); err != nil {
		t.Fatalf("update obs: %v", err)
	}
	d, _ := GetEntityDetail(ctx, pool, "editproj", "DeltaRenamed")
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

	if err := DeleteObservationByContent(ctx, pool, "editproj", "DeltaRenamed", "drop this"); err != nil {
		t.Fatalf("delete obs: %v", err)
	}
	d2, _ := GetEntityDetail(ctx, pool, "editproj", "DeltaRenamed")
	for _, o := range d2.Observations {
		if o.Content == "drop this" {
			t.Fatal("deleted content still present")
		}
	}

	if err := DeleteRelationByTriple(ctx, pool, "editproj", "DeltaRenamed", "Echo", "uses"); err != nil {
		t.Fatalf("delete rel: %v", err)
	}
	d3, _ := GetEntityDetail(ctx, pool, "editproj", "DeltaRenamed")
	if len(d3.Relations) != 0 {
		t.Fatalf("relation not deleted: %+v", d3.Relations)
	}

	if err := DeleteRelationByTriple(ctx, pool, "editproj", "DeltaRenamed", "Echo", "uses"); err == nil {
		t.Fatal("expected error deleting absent relation")
	}
}
