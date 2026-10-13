package usecase

import (
	"context"
	"testing"
	"time"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/repository"
)

func TestHistoryObservationEdit(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "histproj", []entity.EntityInput{
		{Name: "E", Type: "project", Observations: []string{"original"}},
	})

	d, _ := h.stats.GetEntityDetail(ctx, "histproj", "E")
	obsID := d.Observations[0].ID

	// Edit via content match.
	h.mem.UpdateObservationByContent(ctx, "histproj", "E", "original", "edited", nil)

	// Edit via ID.
	h.mem.UpdateObservation(ctx, "histproj", obsID, "edited again", nil)

	hist, err := h.mem.GetHistory(ctx, "histproj", "E", 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(hist) != 3 {
		t.Fatalf("history entries = %d, want 3", len(hist))
	}
	// Newest first.
	if hist[0].Action != "observation_updated" || hist[0].NewValue == nil || *hist[0].NewValue != "edited again" {
		t.Fatalf("hist[0] = %+v", hist[0])
	}
	if hist[1].OldValue == nil || *hist[1].OldValue != "original" {
		t.Fatalf("hist[1] old = %+v", hist[1].OldValue)
	}
	if hist[2].Action != "observation_created" {
		t.Fatalf("hist[2] action = %q, want observation_created", hist[2].Action)
	}
}

func TestHistoryObservationDelete(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "delproj", []entity.EntityInput{
		{Name: "D", Type: "project", Observations: []string{"doomed fact"}},
	})
	d, _ := h.stats.GetEntityDetail(ctx, "delproj", "D")
	obsID := d.Observations[0].ID

	h.mem.DeleteObservation(ctx, "delproj", obsID)

	hist, _ := h.mem.GetHistory(ctx, "delproj", "D", 10)
	if len(hist) != 2 {
		t.Fatalf("history entries = %d, want 2 (observation_deleted + observation_created)", len(hist))
	}
	if hist[0].Action != "observation_deleted" {
		t.Fatalf("action = %q, want observation_deleted", hist[0].Action)
	}
	if hist[0].OldValue == nil || *hist[0].OldValue != "doomed fact" {
		t.Fatalf("old value = %+v, want 'doomed fact'", hist[0].OldValue)
	}
	if hist[1].Action != "observation_created" {
		t.Fatalf("action = %q, want observation_created", hist[1].Action)
	}
}

func TestHistoryEntityRename(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "renproj", []entity.EntityInput{
		{Name: "OldName", Type: "project"},
	})
	// Rename only (same type) so we get exactly 1 history entry.
	h.mem.UpdateEntity(ctx, "renproj", "OldName", "NewName", "project")

	hist, _ := h.mem.GetHistory(ctx, "renproj", "NewName", 10)
	if len(hist) != 1 {
		t.Fatalf("history for NewName = %d entries, want 1", len(hist))
	}
	if hist[0].Action != "entity_renamed" {
		t.Fatalf("action = %q, want entity_renamed", hist[0].Action)
	}
}

func TestHistoryEntityTypeChange(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "typeproj", []entity.EntityInput{
		{Name: "T", Type: "project"},
	})
	h.mem.UpdateEntity(ctx, "typeproj", "T", "T", "tool")

	hist, _ := h.mem.GetHistory(ctx, "typeproj", "T", 10)
	found := false
	for _, e := range hist {
		if e.Action == "entity_type_changed" {
			found = true
			if e.OldValue == nil || *e.OldValue != "project" {
				t.Fatalf("type old = %+v, want project", e.OldValue)
			}
			if e.NewValue == nil || *e.NewValue != "tool" {
				t.Fatalf("type new = %+v, want tool", e.NewValue)
			}
		}
	}
	if !found {
		t.Fatal("entity_type_changed not in history")
	}
}

func TestHistoryEmptyForUnchangedEdit(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "unchproj", []entity.EntityInput{
		{Name: "U", Type: "project", Observations: []string{"stable"}},
	})
	// Update with same content — should NOT create a history entry.
	h.mem.UpdateObservationByContent(ctx, "unchproj", "U", "stable", "stable", nil)

	hist, _ := h.mem.GetHistory(ctx, "unchproj", "U", 10)
	if len(hist) != 1 {
		t.Fatalf("history = %d entries, want 1 (observation_created only, no update)", len(hist))
	}
	if hist[0].Action != "observation_created" {
		t.Fatalf("action = %q, want observation_created", hist[0].Action)
	}
}

func TestHistoryObservationCreatedLogging(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	// 1. CreateEntities should log observation_created
	h.mem.CreateEntities(ctx, "auditproj", []entity.EntityInput{
		{Name: "Alpha", Type: "project", Observations: []string{"fact 1", "fact 2"}},
	})

	hist, err := h.mem.GetHistory(ctx, "auditproj", "Alpha", 10)
	if err != nil {
		t.Fatalf("get history Alpha: %v", err)
	}
	if len(hist) < 2 {
		t.Fatalf("expected at least 2 history entries for Alpha, got %d: %+v", len(hist), hist)
	}
	for _, entry := range hist {
		if entry.Action != "observation_created" {
			t.Errorf("expected action observation_created, got %q", entry.Action)
		}
		if entry.ProjectID != "auditproj" {
			t.Errorf("expected project_id auditproj, got %q", entry.ProjectID)
		}
	}

	// 2. AddObservations should also log observation_created
	if err := h.mem.AddObservations(ctx, "auditproj", "Alpha", []string{"fact 3"}, nil); err != nil {
		t.Fatalf("add observations: %v", err)
	}

	histAfter, err := h.mem.GetHistory(ctx, "auditproj", "Alpha", 10)
	if err != nil {
		t.Fatalf("get history after: %v", err)
	}
	if len(histAfter) != 3 {
		t.Fatalf("expected 3 history entries for Alpha, got %d: %+v", len(histAfter), histAfter)
	}
	if histAfter[0].NewValue == nil || *histAfter[0].NewValue != "fact 3" {
		t.Errorf("expected newest history to have newValue 'fact 3', got %+v", histAfter[0].NewValue)
	}
}

func TestHistoryProjectIsolation(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	// Create entity with same name in two different projects
	h.mem.CreateEntities(ctx, "proj1", []entity.EntityInput{
		{Name: "SharedName", Type: "concept", Observations: []string{"proj1 fact"}},
	})
	h.mem.CreateEntities(ctx, "proj2", []entity.EntityInput{
		{Name: "SharedName", Type: "concept", Observations: []string{"proj2 fact"}},
	})

	hist1, err := h.mem.GetHistory(ctx, "proj1", "SharedName", 10)
	if err != nil {
		t.Fatalf("get history proj1: %v", err)
	}
	if len(hist1) != 1 {
		t.Fatalf("expected 1 history entry for proj1, got %d", len(hist1))
	}
	if hist1[0].ProjectID != "proj1" || hist1[0].NewValue == nil || *hist1[0].NewValue != "proj1 fact" {
		t.Errorf("unexpected hist1 entry: %+v", hist1[0])
	}

	hist2, err := h.mem.GetHistory(ctx, "proj2", "SharedName", 10)
	if err != nil {
		t.Fatalf("get history proj2: %v", err)
	}
	if len(hist2) != 1 {
		t.Fatalf("expected 1 history entry for proj2, got %d", len(hist2))
	}
	if hist2[0].ProjectID != "proj2" || hist2[0].NewValue == nil || *hist2[0].NewValue != "proj2 fact" {
		t.Errorf("unexpected hist2 entry: %+v", hist2[0])
	}
}

func TestCreateEntitiesReplaceObs(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	// Create entity initial observations
	repo := repository.NewMemoryRepository(h.pool)
	_, err := repo.CreateEntities(ctx, "replproj", []entity.EntityInput{
		{Name: "Target", Type: "checkpoint", Observations: []string{"old obs 1", "old obs 2"}},
	})
	if err != nil {
		t.Fatalf("create initial: %v", err)
	}

	detail, err := h.stats.GetEntityDetail(ctx, "replproj", "Target")
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if len(detail.Observations) != 2 {
		t.Fatalf("expected 2 observations initially, got %d", len(detail.Observations))
	}

	// Re-create with replaceObs = true
	_, err = repo.CreateEntities(ctx, "replproj", []entity.EntityInput{
		{Name: "Target", Type: "checkpoint", Observations: []string{"fresh obs"}},
	}, true)
	if err != nil {
		t.Fatalf("replace create: %v", err)
	}

	detailAfter, err := h.stats.GetEntityDetail(ctx, "replproj", "Target")
	if err != nil {
		t.Fatalf("get detail after: %v", err)
	}
	if len(detailAfter.Observations) != 1 {
		t.Fatalf("expected 1 observation after replace, got %d", len(detailAfter.Observations))
	}
	if detailAfter.Observations[0].Content != "fresh obs" {
		t.Errorf("expected 'fresh obs', got %q", detailAfter.Observations[0].Content)
	}
}

func TestGetTimeline(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	repo := repository.NewMemoryRepository(h.pool)

	// Create entities in timelineproj and otherproj
	h.mem.CreateEntities(ctx, "timelineproj", []entity.EntityInput{
		{Name: "EntA", Type: "project", Observations: []string{"event 1"}},
		{Name: "EntB", Type: "task", Observations: []string{"event 2"}},
	})
	h.mem.CreateEntities(ctx, "otherproj", []entity.EntityInput{
		{Name: "EntC", Type: "project", Observations: []string{"event 3"}},
	})

	// GetTimeline for timelineproj
	entries, err := repo.GetTimeline(ctx, "timelineproj", time.Time{}, 50, nil, nil)
	if err != nil {
		t.Fatalf("GetTimeline failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 timeline entries for timelineproj, got %d", len(entries))
	}
	for _, entry := range entries {
		if entry.ProjectID != "timelineproj" {
			t.Errorf("expected timelineproj, got %q", entry.ProjectID)
		}
	}

	// Filter by entityName
	entName := "EntA"
	filtered, err := repo.GetTimeline(ctx, "timelineproj", time.Time{}, 50, &entName, nil)
	if err != nil {
		t.Fatalf("GetTimeline with entity filter failed: %v", err)
	}
	if len(filtered) != 1 || filtered[0].EntityName != "EntA" {
		t.Fatalf("expected 1 entry for EntA, got %d", len(filtered))
	}
}

