package main

import (
	"context"
	"testing"
)

func TestStatsQueries(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	CreateEntities(ctx, pool, "statsproj", []EntityInput{
		{Name: "Hub", EntityType: "project", Observations: []string{"a", "b"}},
		{Name: "Leaf", EntityType: "concept"},
		{Name: "Solo", EntityType: "concept"}, // no observations, no relations -> orphan + sparse
	})
	CreateRelations(ctx, pool, "statsproj", []RelationInput{{From: "Hub", To: "Leaf", RelationType: "uses"}})

	d, err := GetEntityDetail(ctx, pool, "statsproj", "Hub")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(d.Observations) != 2 {
		t.Fatalf("obs count = %d", len(d.Observations))
	}
	if d.Observations[0].ID == 0 {
		t.Fatal("observation ID not populated")
	}
	if len(d.Relations) != 1 || d.Relations[0].Other != "Leaf" || d.Relations[0].Direction != "out" {
		t.Fatalf("relations = %+v", d.Relations)
	}

	dl, _ := GetEntityDetail(ctx, pool, "statsproj", "Leaf")
	if len(dl.Relations) != 1 || dl.Relations[0].Direction != "in" || dl.Relations[0].Other != "Hub" {
		t.Fatalf("incoming relations = %+v", dl.Relations)
	}

	rows, err := ListEntities(ctx, pool, "statsproj", "project", "", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Hub" || rows[0].ObsCount != 2 || rows[0].RelCount != 1 {
		t.Fatalf("list rows = %+v", rows)
	}
	rows2, _ := ListEntities(ctx, pool, "statsproj", "", "hub", 50)
	if len(rows2) != 1 {
		t.Fatalf("search rows = %+v", rows2)
	}

	m, err := DashboardMetrics(ctx, pool, "statsproj")
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if m.Entities != 3 || m.Observations != 2 || m.Relations != 1 {
		t.Fatalf("counts = %+v", m)
	}
	if m.Orphans != 1 { // Solo only (Hub has out; Leaf has in)
		t.Fatalf("orphans = %d", m.Orphans)
	}
	if m.Sparse != 2 { // Leaf + Solo, both no observations
		t.Fatalf("sparse = %d", m.Sparse)
	}
	if len(m.TopByObs) == 0 || m.TopByObs[0].Name != "Hub" {
		t.Fatalf("topByObs = %+v", m.TopByObs)
	}
}
