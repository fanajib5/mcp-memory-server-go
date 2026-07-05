package usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/event"
	"mcp-memory-server/internal/gateway"
	"mcp-memory-server/internal/repository"
)

// embedTimeout caps how long a single embed call may take. If the embedder is
// slow or down, the usecase stores the observation without an embedding and
// continues — semantic search is best-effort, not blocking.
const embedTimeout = 5 * time.Second

// clampConfidence forces a confidence value into [0.0, 1.0].
func clampConfidence(c float64) float64 {
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

// clampConfidences applies clampConfidence to every value in place; returns the
// same slice. A nil/empty slice means "no confidence provided" (all neutral).
func clampConfidences(c []float64) []float64 {
	for i := range c {
		c[i] = clampConfidence(c[i])
	}
	return c
}

// MemoryUseCase applies domain rules (project/entity/relation normalization,
// validation, defaults) before delegating to the repository. It is the seam
// where future cross-cutting memory features (decay scoring, confidence,
// versioning) will hook in without touching SQL.
type MemoryUseCase struct {
	repo     repository.MemoryRepository
	embedder gateway.Embedder // nil = semantic search off
	bus      *event.Bus       // nil = events disabled
}

// NewMemoryUseCase wires a usecase to its repository. Pass nil for embedder to
// disable semantic features (lexical-only mode). Pass nil for bus to disable
// SSE event publishing.
func NewMemoryUseCase(repo repository.MemoryRepository, embedder gateway.Embedder, bus *event.Bus) *MemoryUseCase {
	return &MemoryUseCase{repo: repo, embedder: embedder, bus: bus}
}

// publish emits an event to the bus if configured. No-op when bus is nil.
func (u *MemoryUseCase) publish(project, entity, eventType string) {
	if u.bus == nil {
		return
	}
	u.bus.Publish(project, event.Event{Type: eventType, Entity: entity})
}

// embedTexts is a fail-safe wrapper: embeds texts with a timeout. On any error
// (timeout, network, Ollama down), returns nil vectors and logs a warning so
// the write/search proceeds in lexical-only mode.
func (u *MemoryUseCase) embedTexts(ctx context.Context, texts []string) [][]float32 {
	if u.embedder == nil || len(texts) == 0 {
		return nil
	}
	embCtx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()
	vecs, err := u.embedder.Embed(embCtx, texts)
	if err != nil {
		log.Printf("embed failed (non-fatal, semantic off for this op): %v", err)
		return nil
	}
	return vecs
}

func (u *MemoryUseCase) CreateEntities(ctx context.Context, project string, entities []entity.EntityInput) ([]string, error) {
	project = defaultProject(project)
	for i := range entities {
		entities[i].Type = normalizeEntityType(entities[i].Type)
		entities[i].Confidences = clampConfidences(entities[i].Confidences)
		if len(entities[i].Observations) > 0 {
			entities[i].Embeddings = u.embedTexts(ctx, entities[i].Observations)
		}
	}
	created, err := u.repo.CreateEntities(ctx, project, entities)
	if err != nil {
		return nil, err
	}
	for _, name := range created {
		u.publish(project, name, "entity_created")
	}
	return created, nil
}

func (u *MemoryUseCase) AddObservations(ctx context.Context, project, entityName string, observations []string, confidences []float64) error {
	project = defaultProject(project)
	embeddings := u.embedTexts(ctx, observations)
	if err := u.repo.AddObservations(ctx, project, entityName, observations, clampConfidences(confidences), embeddings); err != nil {
		return err
	}
	u.publish(project, entityName, "observation_added")
	return nil
}

func (u *MemoryUseCase) CreateRelations(ctx context.Context, project string, relations []entity.RelationInput) ([]string, error) {
	project = defaultProject(project)
	for i := range relations {
		relations[i].RelationType = normalizeRelationType(relations[i].RelationType)
		if relations[i].RelationType == "" {
			return nil, fmt.Errorf("relation has empty type after normalization")
		}
	}
	created, err := u.repo.CreateRelations(ctx, project, relations)
	if err != nil {
		return nil, err
	}
	for _, r := range relations {
		u.publish(project, r.From, "relation_created")
	}
	return created, nil
}

func (u *MemoryUseCase) DeleteEntities(ctx context.Context, project string, names []string) error {
	project = defaultProject(project)
	if err := u.repo.DeleteEntities(ctx, project, names); err != nil {
		return err
	}
	for _, name := range names {
		u.publish(project, name, "entity_deleted")
	}
	return nil
}

// Search runs the query, then best-effort bumps last_accessed_at for every
// matched entity (retrieval = access). A touch failure is logged, not fatal,
// so a search never fails due to access-tracking.
func (u *MemoryUseCase) Search(ctx context.Context, project, query string, limit int) ([]entity.SearchResult, error) {
	project = defaultProject(project)
	if limit <= 0 {
		limit = 20
	}
	// Best-effort query embedding for semantic fusion (nil = lexical-only).
	queryVec := u.embedTexts(ctx, []string{query})
	var vec []float32
	if len(queryVec) > 0 {
		vec = queryVec[0]
	}
	results, ids, err := u.repo.Search(ctx, project, query, limit, vec)
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

func (u *MemoryUseCase) UpdateEntity(ctx context.Context, project, oldName, newName, entityType string) error {
	project = defaultProject(project)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("entity name is required")
	}
	et := normalizeEntityType(entityType)
	if err := u.repo.UpdateEntity(ctx, project, oldName, newName, et); err != nil {
		return err
	}
	if newName != oldName {
		u.publish(project, newName, "entity_renamed")
	} else {
		u.publish(project, newName, "entity_type_changed")
	}
	return nil
}

func (u *MemoryUseCase) DeleteObservation(ctx context.Context, project string, id int) error {
	project = defaultProject(project)
	if err := u.repo.DeleteObservation(ctx, project, id); err != nil {
		return err
	}
	u.publish(project, "", "observation_deleted")
	return nil
}

func (u *MemoryUseCase) UpdateObservation(ctx context.Context, project string, id int, content string, newConfidence *float64) error {
	project = defaultProject(project)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("observation content is required")
	}
	var conf *float64
	if newConfidence != nil {
		c := clampConfidence(*newConfidence)
		conf = &c
	}
	emb := u.embedTexts(ctx, []string{content})
	var vec []float32
	if len(emb) > 0 {
		vec = emb[0]
	}
	if err := u.repo.UpdateObservation(ctx, project, id, content, conf, vec); err != nil {
		return err
	}
	u.publish(project, "", "observation_updated")
	return nil
}

func (u *MemoryUseCase) DeleteRelation(ctx context.Context, project string, id int) error {
	project = defaultProject(project)
	if err := u.repo.DeleteRelation(ctx, project, id); err != nil {
		return err
	}
	u.publish(project, "", "relation_deleted")
	return nil
}

func (u *MemoryUseCase) DeleteObservationByContent(ctx context.Context, project, entityName, content string) error {
	project = defaultProject(project)
	if err := u.repo.DeleteObservationByContent(ctx, project, entityName, content); err != nil {
		return err
	}
	u.publish(project, entityName, "observation_deleted")
	return nil
}

func (u *MemoryUseCase) UpdateObservationByContent(ctx context.Context, project, entityName, oldContent, newContent string, newConfidence *float64) error {
	project = defaultProject(project)
	if strings.TrimSpace(newContent) == "" {
		return fmt.Errorf("new content is required")
	}
	var conf *float64
	if newConfidence != nil {
		c := clampConfidence(*newConfidence)
		conf = &c
	}
	emb := u.embedTexts(ctx, []string{newContent})
	var vec []float32
	if len(emb) > 0 {
		vec = emb[0]
	}
	if err := u.repo.UpdateObservationByContent(ctx, project, entityName, oldContent, newContent, conf, vec); err != nil {
		return err
	}
	u.publish(project, entityName, "observation_updated")
	return nil
}

func (u *MemoryUseCase) DeleteRelationByTriple(ctx context.Context, project, from, to, relationType string) error {
	project = defaultProject(project)
	relType := normalizeRelationType(relationType)
	if relType == "" {
		return fmt.Errorf("relation type is required")
	}
	if err := u.repo.DeleteRelationByTriple(ctx, project, from, to, relType); err != nil {
		return err
	}
	u.publish(project, from, "relation_deleted")
	return nil
}

// GetHistory returns the audit trail for one entity within a project.
func (u *MemoryUseCase) GetHistory(ctx context.Context, project, entityName string, limit int) ([]entity.HistoryEntry, error) {
	return u.repo.GetHistory(ctx, defaultProject(project), entityName, limit)
}
