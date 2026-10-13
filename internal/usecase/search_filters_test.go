package usecase

import (
	"context"
	"testing"

	"mcp-memory-server/internal/entity"
)

func TestSearchFilters(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	project := "filterproj"

	// Setup entities with different entity types, tags, and observations
	entities := []entity.EntityInput{
		{
			Name:         "ArchDecision1",
			Type:         "decision",
			Tags:         []string{"architecture", "backend"},
			Observations: []string{"use PostgreSQL for relational and vector storage"},
		},
		{
			Name:         "ArchDecision2",
			Type:         "decision",
			Tags:         []string{"architecture", "frontend"},
			Observations: []string{"use HTMX and Tailwind for dashboard UI"},
		},
		{
			Name:         "BackendTool",
			Type:         "tool",
			Tags:         []string{"backend", "database"},
			Observations: []string{"PostgreSQL database server instance"},
		},
		{
			Name:         "FrontendTool",
			Type:         "tool",
			Tags:         []string{"frontend"},
			Observations: []string{"Tailwind CSS compiler tool"},
		},
		{
			Name:         "ProjectEntity",
			Type:         "project",
			Tags:         []string{"core"},
			Observations: []string{"knowledge graph memory service in Go"},
		},
	}

	if _, err := h.mem.CreateEntities(ctx, project, entities); err != nil {
		t.Fatalf("CreateEntities failed: %v", err)
	}

	// 1. Unfiltered search: query "PostgreSQL" should return ArchDecision1 and BackendTool
	hits, err := h.mem.Search(ctx, project, "PostgreSQL", 10)
	if err != nil {
		t.Fatalf("Search without filters failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits for 'PostgreSQL', got %d: %+v", len(hits), hits)
	}

	// 2. Filter by entity_types: ["decision"]
	decisionHits, err := h.mem.Search(ctx, project, "PostgreSQL", 10, []string{"decision"}, nil)
	if err != nil {
		t.Fatalf("Search with entity_types filter failed: %v", err)
	}
	if len(decisionHits) != 1 || decisionHits[0].Name != "ArchDecision1" {
		t.Fatalf("expected only ArchDecision1 for entity_types=['decision'], got %+v", decisionHits)
	}

	// 3. Filter by entity_types: ["tool"]
	toolHits, err := h.mem.Search(ctx, project, "PostgreSQL", 10, []string{"tool"}, nil)
	if err != nil {
		t.Fatalf("Search with entity_types=['tool'] failed: %v", err)
	}
	if len(toolHits) != 1 || toolHits[0].Name != "BackendTool" {
		t.Fatalf("expected only BackendTool for entity_types=['tool'], got %+v", toolHits)
	}

	// 4. Filter by tags_any: ["backend"]
	// Both ArchDecision1 and BackendTool have "backend" tag
	backendHits, err := h.mem.Search(ctx, project, "PostgreSQL", 10, nil, []string{"backend"})
	if err != nil {
		t.Fatalf("Search with tags_any=['backend'] failed: %v", err)
	}
	if len(backendHits) != 2 {
		t.Fatalf("expected 2 hits for tags_any=['backend'], got %d: %+v", len(backendHits), backendHits)
	}

	// 5. Filter by tags_any: ["database"]
	// Only BackendTool has "database" tag
	dbHits, err := h.mem.Search(ctx, project, "PostgreSQL", 10, nil, []string{"database"})
	if err != nil {
		t.Fatalf("Search with tags_any=['database'] failed: %v", err)
	}
	if len(dbHits) != 1 || dbHits[0].Name != "BackendTool" {
		t.Fatalf("expected only BackendTool for tags_any=['database'], got %+v", dbHits)
	}

	// 6. Filter by both entity_types: ["decision"] AND tags_any: ["frontend"]
	// Query "UI": ArchDecision2 matches "UI"
	bothHits, err := h.mem.Search(ctx, project, "UI", 10, []string{"decision"}, []string{"frontend"})
	if err != nil {
		t.Fatalf("Search with both entity_types and tags_any failed: %v", err)
	}
	if len(bothHits) != 1 || bothHits[0].Name != "ArchDecision2" {
		t.Fatalf("expected only ArchDecision2, got %+v", bothHits)
	}

	// 7. Filter combination that matches nothing: entity_types: ["decision"] AND tags_any: ["database"]
	noHits, err := h.mem.Search(ctx, project, "PostgreSQL", 10, []string{"decision"}, []string{"database"})
	if err != nil {
		t.Fatalf("Search with non-matching filter combination failed: %v", err)
	}
	if len(noHits) != 0 {
		t.Fatalf("expected 0 hits, got %+v", noHits)
	}
}
