// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package usecase

import (
	"context"
	"testing"
	"time"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/repository"
)

func TestGetTimeline(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	repo := repository.NewMemoryRepository(h.pool)

	// Create entities in timelineproj and otherproj
	if _, err := h.mem.CreateEntities(ctx, "timelineproj", []entity.EntityInput{
		{Name: "EntA", Type: "project", Observations: []string{"event 1"}},
		{Name: "EntB", Type: "task", Observations: []string{"event 2"}},
	}); err != nil {
		t.Fatalf("create timelineproj: %v", err)
	}
	if _, err := h.mem.CreateEntities(ctx, "otherproj", []entity.EntityInput{
		{Name: "EntC", Type: "project", Observations: []string{"event 3"}},
	}); err != nil {
		t.Fatalf("create otherproj: %v", err)
	}

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

	// Filter by source
	srcA := "agent-alpha"
	srcB := "agent-beta"
	if err := repo.AddObservations(ctx, "timelineproj", "EntA", []string{"event 4"}, nil, nil, entity.Attribution{Source: &srcA}); err != nil {
		t.Fatalf("add obs 4: %v", err)
	}
	if err := repo.AddObservations(ctx, "timelineproj", "EntA", []string{"event 5"}, nil, nil, entity.Attribution{Source: &srcB}); err != nil {
		t.Fatalf("add obs 5: %v", err)
	}

	srcFilter := "agent-alpha"
	bySource, err := repo.GetTimeline(ctx, "timelineproj", time.Time{}, 50, nil, &srcFilter)
	if err != nil {
		t.Fatalf("GetTimeline with source filter failed: %v", err)
	}
	if len(bySource) != 1 || bySource[0].Source == nil || *bySource[0].Source != srcA {
		t.Fatalf("expected 1 entry with source %q, got %d entries: %+v", srcA, len(bySource), bySource)
	}

	// Filter by since
	cutoff := time.Now().Add(-10 * time.Second)
	sincePast, err := repo.GetTimeline(ctx, "timelineproj", cutoff, 50, nil, nil)
	if err != nil {
		t.Fatalf("GetTimeline with since cutoff in past failed: %v", err)
	}
	if len(sincePast) != 4 {
		t.Fatalf("expected 4 entries since past cutoff, got %d", len(sincePast))
	}

	sinceFuture, err := repo.GetTimeline(ctx, "timelineproj", time.Now().Add(10*time.Minute), 50, nil, nil)
	if err != nil {
		t.Fatalf("GetTimeline with since cutoff in future failed: %v", err)
	}
	if len(sinceFuture) != 0 {
		t.Fatalf("expected 0 entries since future cutoff, got %d", len(sinceFuture))
	}

	// Verify tie-breaker: order is descending by ID when happened_at is equal
	for i := 1; i < len(sincePast); i++ {
		if sincePast[i-1].HappenedAt == sincePast[i].HappenedAt && sincePast[i-1].ID <= sincePast[i].ID {
			t.Errorf("tie-breaker failed: entry[%d].ID=%d <= entry[%d].ID=%d", i-1, sincePast[i-1].ID, i, sincePast[i].ID)
		}
	}
}
