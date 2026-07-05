package usecase

import (
	"context"
	"testing"

	"mcp-memory-server/internal/entity"
)

func TestStatsQueries(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "statsproj", []entity.EntityInput{
		{Name: "Hub", Type: "project", Observations: []string{"a", "b"}},
		{Name: "Leaf", Type: "concept"},
		{Name: "Solo", Type: "concept"}, // no observations, no relations -> orphan + sparse
	})
	h.mem.CreateRelations(ctx, "statsproj", []entity.RelationInput{{From: "Hub", To: "Leaf", RelationType: "uses"}})

	d, err := h.stats.GetEntityDetail(ctx, "statsproj", "Hub")
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

	dl, _ := h.stats.GetEntityDetail(ctx, "statsproj", "Leaf")
	if len(dl.Relations) != 1 || dl.Relations[0].Direction != "in" || dl.Relations[0].Other != "Hub" {
		t.Fatalf("incoming relations = %+v", dl.Relations)
	}

	rows, err := h.stats.ListEntities(ctx, "statsproj", "project", "", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Hub" || rows[0].ObsCount != 2 || rows[0].RelCount != 1 {
		t.Fatalf("list rows = %+v", rows)
	}
	rows2, _ := h.stats.ListEntities(ctx, "statsproj", "", "hub", 50)
	if len(rows2) != 1 {
		t.Fatalf("search rows = %+v", rows2)
	}

	m, err := h.stats.DashboardMetrics(ctx, "statsproj")
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if m.Entities != 3 || m.Observations != 2 || m.Relations != 1 {
		t.Fatalf("counts = %+v", m)
	}
	if m.Orphans != 1 {
		t.Fatalf("orphans = %d", m.Orphans)
	}
	if m.Sparse != 2 {
		t.Fatalf("sparse = %d", m.Sparse)
	}
	if len(m.TopByObs) == 0 || m.TopByObs[0].Name != "Hub" {
		t.Fatalf("topByObs = %+v", m.TopByObs)
	}
}

func TestGraphData(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "graphproj", []entity.EntityInput{
		{Name: "A", Type: "project"},
		{Name: "B", Type: "tool"},
	})
	h.mem.CreateRelations(ctx, "graphproj", []entity.RelationInput{{From: "A", To: "B", RelationType: "uses"}})

	g, err := h.stats.GraphData(ctx, "graphproj")
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(g.Nodes))
	}
	if g.Nodes[0].ID == 0 {
		t.Fatal("node ID not populated")
	}
	if len(g.Edges) != 1 || g.Edges[0].From == 0 || g.Edges[0].Label != "USES" {
		t.Fatalf("edges = %+v", g.Edges)
	}
}
