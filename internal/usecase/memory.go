package usecase

import (
	"context"
	"fmt"
	"strings"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/repository"
)

// MemoryUseCase applies domain rules (project/entity/relation normalization,
// validation, defaults) before delegating to the repository. It is the seam
// where future cross-cutting memory features (decay scoring, confidence,
// versioning) will hook in without touching SQL.
type MemoryUseCase struct {
	repo repository.MemoryRepository
}

// NewMemoryUseCase wires a usecase to its repository.
func NewMemoryUseCase(repo repository.MemoryRepository) *MemoryUseCase {
	return &MemoryUseCase{repo: repo}
}

func (u *MemoryUseCase) CreateEntities(ctx context.Context, project string, entities []entity.EntityInput) ([]string, error) {
	project = defaultProject(project)
	for i := range entities {
		entities[i].Type = normalizeEntityType(entities[i].Type)
	}
	return u.repo.CreateEntities(ctx, project, entities)
}

func (u *MemoryUseCase) AddObservations(ctx context.Context, project, entityName string, observations []string) error {
	project = defaultProject(project)
	return u.repo.AddObservations(ctx, project, entityName, observations)
}

func (u *MemoryUseCase) CreateRelations(ctx context.Context, project string, relations []entity.RelationInput) ([]string, error) {
	project = defaultProject(project)
	for i := range relations {
		relations[i].RelationType = normalizeRelationType(relations[i].RelationType)
		if relations[i].RelationType == "" {
			return nil, fmt.Errorf("relation has empty type after normalization")
		}
	}
	return u.repo.CreateRelations(ctx, project, relations)
}

func (u *MemoryUseCase) DeleteEntities(ctx context.Context, project string, names []string) error {
	project = defaultProject(project)
	return u.repo.DeleteEntities(ctx, project, names)
}

func (u *MemoryUseCase) Search(ctx context.Context, project, query string, limit int) ([]entity.SearchResult, error) {
	project = defaultProject(project)
	if limit <= 0 {
		limit = 20
	}
	return u.repo.Search(ctx, project, query, limit)
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

func (u *MemoryUseCase) UpdateEntity(ctx context.Context, project, oldName, newName, entityType string) error {
	project = defaultProject(project)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("entity name is required")
	}
	et := normalizeEntityType(entityType)
	return u.repo.UpdateEntity(ctx, project, oldName, newName, et)
}

func (u *MemoryUseCase) DeleteObservation(ctx context.Context, project string, id int) error {
	project = defaultProject(project)
	return u.repo.DeleteObservation(ctx, project, id)
}

func (u *MemoryUseCase) UpdateObservation(ctx context.Context, project string, id int, content string) error {
	project = defaultProject(project)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("observation content is required")
	}
	return u.repo.UpdateObservation(ctx, project, id, content)
}

func (u *MemoryUseCase) DeleteRelation(ctx context.Context, project string, id int) error {
	project = defaultProject(project)
	return u.repo.DeleteRelation(ctx, project, id)
}

func (u *MemoryUseCase) DeleteObservationByContent(ctx context.Context, project, entityName, content string) error {
	project = defaultProject(project)
	return u.repo.DeleteObservationByContent(ctx, project, entityName, content)
}

func (u *MemoryUseCase) UpdateObservationByContent(ctx context.Context, project, entityName, oldContent, newContent string) error {
	project = defaultProject(project)
	if strings.TrimSpace(newContent) == "" {
		return fmt.Errorf("new content is required")
	}
	return u.repo.UpdateObservationByContent(ctx, project, entityName, oldContent, newContent)
}

func (u *MemoryUseCase) DeleteRelationByTriple(ctx context.Context, project, from, to, relationType string) error {
	project = defaultProject(project)
	relType := normalizeRelationType(relationType)
	if relType == "" {
		return fmt.Errorf("relation type is required")
	}
	return u.repo.DeleteRelationByTriple(ctx, project, from, to, relType)
}
