// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcp-memory-server/internal/entity"
)

// MemoryRepository is the data-access boundary for the knowledge graph.
// Implementations receive already-normalized values (project non-empty unless
// the operation documents "" = all namespaces, entity types/relation types
// normalized) — domain rules live in the usecase layer.
type MemoryRepository interface {
	CreateEntities(ctx context.Context, project string, entities []entity.EntityInput, replaceObs ...bool) ([]string, error)
	AddObservations(ctx context.Context, project, entityName string, observations []string, confidences []float64, embeddings [][]float32, attr ...entity.Attribution) error
	CreateRelations(ctx context.Context, project string, relations []entity.RelationInput) ([]string, error)
	DeleteEntities(ctx context.Context, project string, names []string) error
	Search(ctx context.Context, project, query string, limit int, queryVec []float32, entityTypes []string, tagsAny []string) ([]entity.SearchResult, []int, error)
	ReadGraph(ctx context.Context, project string) (*entity.FullGraph, error)
	Export(ctx context.Context, project string) (*entity.ExportPayload, error)
	Import(ctx context.Context, project string, g *entity.ExportPayload) (*entity.ImportResult, error)
	UpdateEntity(ctx context.Context, project, oldName, newName, entityType string) error
	DeleteObservation(ctx context.Context, project string, id int) error
	UpdateObservation(ctx context.Context, project string, id int, content string, newConfidence *float64, newEmbedding []float32) error
	DeleteRelation(ctx context.Context, project string, id int) error
	DeleteObservationByContent(ctx context.Context, project, entityName, content string) error
	UpdateObservationByContent(ctx context.Context, project, entityName, oldContent, newContent string, newConfidence *float64, newEmbedding []float32) error
	DeleteRelationByTriple(ctx context.Context, project, from, to, relationType string) error
	TouchAccessed(ctx context.Context, entityIDs []int) error
	GetHistory(ctx context.Context, project, entityName string, limit int) ([]entity.HistoryEntry, error)
	GetTimeline(ctx context.Context, project string, since time.Time, limit int, entityName *string, source *string) ([]entity.HistoryEntry, error)
	LogHistory(ctx context.Context, project, entityName, action string, attr entity.Attribution) error
	GetTopEntities(ctx context.Context, project string, limit int) ([]entity.SearchResult, error)
	GetRecentEntitiesByType(ctx context.Context, project, entityType string, since time.Time) ([]entity.Entity, error)
}

// postgresMemory is the pgx-backed MemoryRepository.
type postgresMemory struct {
	pool *pgxpool.Pool
}

// NewMemoryRepository builds a pgx-backed MemoryRepository.
func NewMemoryRepository(pool *pgxpool.Pool) MemoryRepository {
	return &postgresMemory{pool: pool}
}

func (r *postgresMemory) CreateEntities(ctx context.Context, project string, entities []entity.EntityInput, replaceObs ...bool) ([]string, error) {
	shouldReplace := len(replaceObs) > 0 && replaceObs[0]

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var created []string
	for _, e := range entities {
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, e.Name).Scan(&id)
		if err != nil {
			tags := e.Tags
			if tags == nil {
				tags = []string{}
			}
			if err := tx.QueryRow(ctx,
				`INSERT INTO memory_entities (project_id, name, entity_type, tags) VALUES ($1, $2, $3, $4) RETURNING id`,
				project, e.Name, e.Type, tags,
			).Scan(&id); err != nil {
				return nil, fmt.Errorf("create entity %q: %w", e.Name, err)
			}
		} else {
			if len(e.Tags) > 0 {
				if _, err := tx.Exec(ctx, `UPDATE memory_entities SET tags = $1 WHERE id = $2`, e.Tags, id); err != nil {
					return nil, fmt.Errorf("update tags for %q: %w", e.Name, err)
				}
			}
			if shouldReplace {
				if _, err := tx.Exec(ctx, `DELETE FROM memory_observations WHERE entity_id = $1`, id); err != nil {
					return nil, fmt.Errorf("delete existing observations for %q: %w", e.Name, err)
				}
			}
		}

		for i, obs := range e.Observations {
			var conf any
			var confVal *float64
			if i < len(e.Confidences) {
				conf = e.Confidences[i]
				c := e.Confidences[i]
				confVal = &c
			}
			var emb any
			if i < len(e.Embeddings) {
				emb = vecLiteral(e.Embeddings[i])
			}
			var obsID int
			if err := tx.QueryRow(ctx,
				`INSERT INTO memory_observations (entity_id, content, confidence, embedding, source, session_id, agent_label)
				 VALUES ($1, $2, $3, $4::vector, $5, $6, $7) RETURNING id`,
				id, obs, conf, emb, e.Source, e.SessionID, e.AgentLabel,
			).Scan(&obsID); err != nil {
				return nil, fmt.Errorf("add observation to %q: %w", e.Name, err)
			}

			if _, err := tx.Exec(ctx,
				`INSERT INTO memory_history (project_id, entity_id, entity_name, observation_id, action, new_value, confidence, source, session_id, agent_label)
				 VALUES ($1, $2, $3, $4, 'observation_created', $5, $6, $7, $8, $9)`,
				project, id, e.Name, obsID, obs, confVal, e.Source, e.SessionID, e.AgentLabel,
			); err != nil {
				return nil, fmt.Errorf("record observation_created history for %q: %w", e.Name, err)
			}
		}
		created = append(created, e.Name)
	}
	return created, tx.Commit(ctx)
}

func (r *postgresMemory) AddObservations(ctx context.Context, project, entityName string, observations []string, confidences []float64, embeddings [][]float32, attr ...entity.Attribution) error {
	var source, sessionID, agentLabel *string
	if len(attr) > 0 {
		source = attr[0].Source
		sessionID = attr[0].SessionID
		agentLabel = attr[0].AgentLabel
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int
	err = tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, entityName).Scan(&id)
	if err != nil {
		if err := tx.QueryRow(ctx,
			`INSERT INTO memory_entities (project_id, name, entity_type) VALUES ($1, $2, 'concept') RETURNING id`,
			project, entityName,
		).Scan(&id); err != nil {
			return fmt.Errorf("create entity %q: %w", entityName, err)
		}
	}
	for i, obs := range observations {
		var conf any
		var confVal *float64
		if i < len(confidences) {
			conf = confidences[i]
			c := confidences[i]
			confVal = &c
		}
		var emb any
		if i < len(embeddings) {
			emb = vecLiteral(embeddings[i])
		}
		var obsID int
		if err := tx.QueryRow(ctx,
			`INSERT INTO memory_observations (entity_id, content, confidence, embedding, source, session_id, agent_label)
			 VALUES ($1, $2, $3, $4::vector, $5, $6, $7) RETURNING id`,
			id, obs, conf, emb, source, sessionID, agentLabel,
		).Scan(&obsID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO memory_history (project_id, entity_id, entity_name, observation_id, action, new_value, confidence, source, session_id, agent_label)
			 VALUES ($1, $2, $3, $4, 'observation_created', $5, $6, $7, $8, $9)`,
			project, id, entityName, obsID, obs, confVal, source, sessionID, agentLabel,
		); err != nil {
			return fmt.Errorf("record observation_created history for %q: %w", entityName, err)
		}
	}
	return tx.Commit(ctx)
}

func (r *postgresMemory) CreateRelations(ctx context.Context, project string, relations []entity.RelationInput) ([]string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// getID resolves an entity by (project, name), creating it as 'concept' if absent.
	getID := func(name string) (int, error) {
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, name).Scan(&id)
		if err == nil {
			return id, nil
		}
		err = tx.QueryRow(ctx,
			`INSERT INTO memory_entities (project_id, name, entity_type) VALUES ($1, $2, 'concept') RETURNING id`,
			project, name,
		).Scan(&id)
		return id, err
	}

	var created []string
	for _, rel := range relations {
		if rel.RelationType == "" {
			return nil, fmt.Errorf("relation has empty type")
		}
		fromID, err := getID(rel.From)
		if err != nil {
			return nil, err
		}
		toID, err := getID(rel.To)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO memory_relations (from_entity_id, to_entity_id, relation_type)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (from_entity_id, to_entity_id, relation_type) DO NOTHING`,
			fromID, toID, rel.RelationType,
		)
		if err != nil {
			return nil, err
		}
		created = append(created, fmt.Sprintf("%s --%s--> %s", rel.From, rel.RelationType, rel.To))
	}
	return created, tx.Commit(ctx)
}

func (r *postgresMemory) DeleteEntities(ctx context.Context, project string, names []string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM memory_entities WHERE project_id = $1 AND name = ANY($2::text[])`, project, names)
	return err
}

// UpdateEntity renames and/or changes the type of an entity within a project.
// Rejects a rename if the new name already exists in the same project.
func (r *postgresMemory) UpdateEntity(ctx context.Context, project, oldName, newName, entityType string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int
	var oldType string
	err = tx.QueryRow(ctx,
		`SELECT id, entity_type FROM memory_entities WHERE project_id = $1 AND name = $2`, project, oldName,
	).Scan(&id, &oldType)
	if err != nil {
		return fmt.Errorf("entity %q not found in project %q: %w", oldName, project, err)
	}

	if newName != oldName {
		var collision int
		err = tx.QueryRow(ctx,
			`SELECT 1 FROM memory_entities WHERE project_id = $1 AND name = $2 AND id <> $3`,
			project, newName, id,
		).Scan(&collision)
		if err == nil {
			return fmt.Errorf("name %q already exists in project %q", newName, project)
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE memory_entities SET name = $2, entity_type = $3 WHERE id = $1`, id, newName, entityType,
	); err != nil {
		return err
	}

	// History: capture rename and/or type change. entity_name = current (post-mutation) name
	// so GetHistory (queried by current name) finds both events.
	if newName != oldName {
		_, _ = tx.Exec(ctx, `INSERT INTO memory_history (project_id, entity_id, entity_name, action, old_value, new_value) VALUES ($1, $2, $3, 'entity_renamed', $4, $5)`,
			project, id, newName, oldName, newName)
	}
	if entityType != oldType {
		_, _ = tx.Exec(ctx, `INSERT INTO memory_history (project_id, entity_id, entity_name, action, old_value, new_value) VALUES ($1, $2, $3, 'entity_type_changed', $4, $5)`,
			project, id, newName, oldType, entityType)
	}

	return tx.Commit(ctx)
}
