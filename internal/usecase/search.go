// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"context"
	"log"
	"time"

	"mcp-memory-server/internal/entity"
)

// Search runs the query, then best-effort bumps last_accessed_at for every
// matched entity (retrieval = access). A touch failure is logged, not fatal,
// so a search never fails due to access-tracking.
// Optional filters: filterParams[0] = entityTypes []string, filterParams[1] = tagsAny []string.
func (u *MemoryUseCase) Search(ctx context.Context, project, query string, limit int, filterParams ...[]string) ([]entity.SearchResult, error) {
	project = defaultProject(project)
	if limit <= 0 {
		limit = 20
	}
	var entityTypes, tagsAny []string
	if len(filterParams) > 0 {
		entityTypes = filterParams[0]
	}
	if len(filterParams) > 1 {
		tagsAny = filterParams[1]
	}

	// Best-effort query embedding for semantic fusion (nil = lexical-only).
	queryVec := u.embedTexts(ctx, []string{query})
	var vec []float32
	if len(queryVec) > 0 {
		vec = queryVec[0]
	}
	results, ids, err := u.repo.Search(ctx, project, query, limit, vec, entityTypes, tagsAny)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		if terr := u.repo.TouchAccessed(ctx, ids); terr != nil {
			log.Printf("search: touch last_accessed_at failed (non-fatal): %v", terr)
		}
	}
	return results, nil
}

// ReadGraph keeps project=="" to mean "all projects" (intentionally not defaulted).
func (u *MemoryUseCase) ReadGraph(ctx context.Context, project string) (*entity.FullGraph, error) {
	return u.repo.ReadGraph(ctx, project)
}

// Export keeps project=="" to mean "all projects" (intentionally not defaulted).
func (u *MemoryUseCase) Export(ctx context.Context, project string) (*entity.ExportPayload, error) {
	return u.repo.Export(ctx, project)
}

func (u *MemoryUseCase) Import(ctx context.Context, project string, g *entity.ExportPayload) (*entity.ImportResult, error) {
	project = defaultProject(project)
	if g != nil {
		for i := range g.Entities {
			g.Entities[i].Type = normalizeEntityType(g.Entities[i].Type)
		}
		for i := range g.Relations {
			g.Relations[i].RelationType = normalizeRelationType(g.Relations[i].RelationType)
		}
	}
	return u.repo.Import(ctx, project, g)
}

// GetHistory returns the audit trail for one entity within a project.
func (u *MemoryUseCase) GetHistory(ctx context.Context, project, entityName string, limit int) ([]entity.HistoryEntry, error) {
	return u.repo.GetHistory(ctx, defaultProject(project), entityName, limit)
}

// GetTimeline returns chronological audit trail events across entities in a project.
func (u *MemoryUseCase) GetTimeline(ctx context.Context, project string, since time.Time, limit int, entityName *string, source *string) ([]entity.HistoryEntry, error) {
	return u.repo.GetTimeline(ctx, defaultProject(project), since, limit, entityName, source)
}
