package usecase

import (
	"context"
	"testing"

	"mcp-memory-server/internal/entity"
)

// TestConfidenceStoredAndReturned verifies confidence values survive create + detail.
func TestConfidenceStoredAndReturned(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	if err := h.mem.AddObservations(ctx, "confproj", "A", []string{"sure fact", "guess"}, []float64{0.9, 0.3}); err != nil {
		t.Fatalf("add: %v", err)
	}
	d, err := h.stats.GetEntityDetail(ctx, "confproj", "A")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(d.Observations) != 2 {
		t.Fatalf("obs count = %d", len(d.Observations))
	}
	// Observations ordered by created_at; first = "sure fact" (0.9).
	// REAL (float32) loses precision, so compare with tolerance.
	if d.Observations[0].Confidence == nil {
		t.Fatalf("obs[0] confidence = <nil>, want ~0.9")
	}
	if diff := *d.Observations[0].Confidence - 0.9; diff > 0.01 || diff < -0.01 {
		t.Fatalf("obs[0] confidence = %.4f, want ~0.9", *d.Observations[0].Confidence)
	}

	// Update observation confidence via content.
	newC := 0.5
	if err := h.mem.UpdateObservationByContent(ctx, "confproj", "A", "guess", "revised guess", &newC); err != nil {
		t.Fatalf("update: %v", err)
	}
	d2, _ := h.stats.GetEntityDetail(ctx, "confproj", "A")
	for _, o := range d2.Observations {
		if o.Content == "revised guess" {
			if o.Confidence == nil {
				t.Fatalf("revised confidence = <nil>, want ~0.5")
			}
			if diff2 := *o.Confidence - 0.5; diff2 > 0.01 || diff2 < -0.01 {
				t.Fatalf("revised confidence = %.4f, want ~0.5", *o.Confidence)
			}
		}
	}
}

// TestConfidenceClamping verifies values out of [0,1] are clamped by the usecase.
func TestConfidenceClamping(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	// 1.7 -> 1.0, -0.4 -> 0.0
	if err := h.mem.AddObservations(ctx, "clampproj", "A", []string{"over", "under"}, []float64{1.7, -0.4}); err != nil {
		t.Fatalf("add: %v", err)
	}
	d, _ := h.stats.GetEntityDetail(ctx, "clampproj", "A")
	var over, under *float64
	for _, o := range d.Observations {
		if o.Content == "over" {
			over = o.Confidence
		}
		if o.Content == "under" {
			under = o.Confidence
		}
	}
	if over == nil || *over != 1.0 {
		t.Fatalf("over clamped = %v, want 1.0", over)
	}
	if under == nil || *under != 0.0 {
		t.Fatalf("under clamped = %v, want 0.0", under)
	}
}

// TestConfidenceNeutral verifies omitting confidence stores NULL (nil pointer).
func TestConfidenceNeutral(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.AddObservations(ctx, "neutralproj", "A", []string{"no confidence given"}, nil)
	d, _ := h.stats.GetEntityDetail(ctx, "neutralproj", "A")
	if d.Observations[0].Confidence != nil {
		t.Fatalf("neutral confidence = %v, want nil", d.Observations[0].Confidence)
	}
}

// TestSearchAccessTouch verifies that Search bumps last_accessed_at.
func TestSearchAccessTouch(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "touchproj", []entity.EntityInput{
		{Name: "X", Type: "project", Observations: []string{"matchable"}},
	})
	// Before search: last_accessed_at is NULL → scan into *string yields nil, no error.
	var laBefore *string
	if err := h.pool.QueryRow(ctx, `SELECT to_char(last_accessed_at, 'YYYY-MM-DD HH24:MI:SS') FROM memory_entities WHERE name='X'`).Scan(&laBefore); err != nil {
		t.Fatalf("pre-search scan err: %v", err)
	}
	if laBefore != nil {
		t.Fatalf("last_accessed_at should be NULL before any search, got %v", *laBefore)
	}

	// Search triggers touch.
	if _, err := h.mem.Search(ctx, "touchproj", "matchable", 10); err != nil {
		t.Fatalf("search: %v", err)
	}

	// After search: last_accessed_at should be set (non-NULL → non-nil pointer).
	var laAfter string
	if err := h.pool.QueryRow(ctx, `SELECT to_char(last_accessed_at, 'YYYY-MM-DD HH24:MI:SS') FROM memory_entities WHERE name='X'`).Scan(&laAfter); err != nil {
		t.Fatalf("last_accessed_at not set after search: %v", err)
	}
}

// TestReRankByConfidence verifies that for the same text query, an entity with
// higher-confidence matching observation ranks above one with low confidence.
func TestReRankByConfidence(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	// Two entities, both matching "alpha feature", different confidence.
	h.mem.AddObservations(ctx, "rankproj", "High", []string{"alpha feature described"}, []float64{1.0})
	h.mem.AddObservations(ctx, "rankproj", "Low", []string{"alpha feature described"}, []float64{0.1})

	results, err := h.mem.Search(ctx, "rankproj", "alpha feature", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "High" {
		t.Fatalf("re-rank: top result = %q, want High (confidence 1.0)", results[0].Name)
	}
}

// TestSearchScoreExposed verifies the Score field is populated in search results.
func TestSearchScoreExposed(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	h.mem.CreateEntities(ctx, "scoreproj", []entity.EntityInput{
		{Name: "S", Type: "project", Observations: []string{"findable keyword"}},
	})
	results, err := h.mem.Search(ctx, "scoreproj", "findable", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].Score == nil {
		t.Fatalf("score not exposed: %+v", results)
	}
}
