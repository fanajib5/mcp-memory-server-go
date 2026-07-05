package usecase

import (
	"context"
	"testing"

	"mcp-memory-server/internal/entity"
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
	if len(hist) != 2 {
		t.Fatalf("history entries = %d, want 2", len(hist))
	}
	// Newest first.
	if hist[0].Action != "observation_updated" || hist[0].NewValue == nil || *hist[0].NewValue != "edited again" {
		t.Fatalf("hist[0] = %+v", hist[0])
	}
	if hist[1].OldValue == nil || *hist[1].OldValue != "original" {
		t.Fatalf("hist[1] old = %+v", hist[1].OldValue)
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
	if len(hist) != 1 {
		t.Fatalf("history entries = %d, want 1 (observation_deleted)", len(hist))
	}
	if hist[0].Action != "observation_deleted" {
		t.Fatalf("action = %q, want observation_deleted", hist[0].Action)
	}
	if hist[0].OldValue == nil || *hist[0].OldValue != "doomed fact" {
		t.Fatalf("old value = %+v, want 'doomed fact'", hist[0].OldValue)
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
	if len(hist) != 0 {
		t.Fatalf("history = %d entries, want 0 (content unchanged)", len(hist))
	}
}
