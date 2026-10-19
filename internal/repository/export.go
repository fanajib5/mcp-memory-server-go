// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package repository

import (
	"context"
	"fmt"

	"mcp-memory-server/internal/entity"
)

// Export returns a structured payload for one project (blank = all projects).
func (r *postgresMemory) Export(ctx context.Context, project string) (*entity.ExportPayload, error) {
	scope := project != ""
	entQuery := `SELECT id, name, entity_type FROM memory_entities ORDER BY name`
	var entArgs []any
	if scope {
		entQuery = `SELECT id, name, entity_type FROM memory_entities WHERE project_id = $1 ORDER BY name`
		entArgs = []any{project}
	}
	entRows, err := r.pool.Query(ctx, entQuery, entArgs...)
	if err != nil {
		return nil, err
	}
	defer entRows.Close()

	type entRow struct {
		id   int
		name string
		typ  string
	}
	var entities []entRow
	for entRows.Next() {
		var e entRow
		if err := entRows.Scan(&e.id, &e.name, &e.typ); err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}

	obsQuery := `SELECT o.entity_id, o.content FROM memory_observations o JOIN memory_entities e ON e.id = o.entity_id`
	var obsArgs []any
	if scope {
		obsQuery += ` WHERE e.project_id = $1`
		obsArgs = []any{project}
	}
	obsRows, err := r.pool.Query(ctx, obsQuery, obsArgs...)
	if err != nil {
		return nil, err
	}
	defer obsRows.Close()
	obsByEntity := map[int][]string{}
	for obsRows.Next() {
		var entityID int
		var content string
		if err := obsRows.Scan(&entityID, &content); err != nil {
			return nil, err
		}
		obsByEntity[entityID] = append(obsByEntity[entityID], content)
	}

	relQuery := `SELECT fe.name, r.relation_type, te.name
		FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		JOIN memory_entities te ON te.id = r.to_entity_id`
	var relArgs []any
	if scope {
		relQuery += ` WHERE fe.project_id = $1 AND te.project_id = $1`
		relArgs = []any{project}
	}
	relRows, err := r.pool.Query(ctx, relQuery, relArgs...)
	if err != nil {
		return nil, err
	}
	defer relRows.Close()
	var relations []entity.ExportRelation
	for relRows.Next() {
		var from, relType, to string
		if err := relRows.Scan(&from, &relType, &to); err != nil {
			return nil, err
		}
		relations = append(relations, entity.ExportRelation{From: from, RelationType: relType, To: to})
	}

	payload := &entity.ExportPayload{Project: project, Relations: relations}
	for _, e := range entities {
		payload.Entities = append(payload.Entities, entity.ExportEntity{
			Name: e.name, Type: e.typ, Observations: obsByEntity[e.id],
		})
	}
	return payload, nil
}

// Import loads a structured payload into a project. Idempotent: existing
// entities are reused (observations appended), existing relations are skipped.
func (r *postgresMemory) Import(ctx context.Context, project string, g *entity.ExportPayload) (*entity.ImportResult, error) {
	if g == nil {
		return &entity.ImportResult{}, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	getOrCreate := func(name string) (int, error) {
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

	res := &entity.ImportResult{}
	for _, e := range g.Entities {
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, e.Name).Scan(&id)
		if err != nil {
			if err := tx.QueryRow(ctx,
				`INSERT INTO memory_entities (project_id, name, entity_type) VALUES ($1, $2, $3) RETURNING id`,
				project, e.Name, e.Type,
			).Scan(&id); err != nil {
				return nil, fmt.Errorf("import entity %q: %w", e.Name, err)
			}
			res.EntitiesCreated++
		}
		for _, obs := range e.Observations {
			if _, err := tx.Exec(ctx,
				`INSERT INTO memory_observations (entity_id, content) VALUES ($1, $2)`, id, obs,
			); err != nil {
				return nil, err
			}
		}
	}
	for _, rel := range g.Relations {
		if rel.RelationType == "" {
			continue
		}
		fromID, err := getOrCreate(rel.From)
		if err != nil {
			return nil, err
		}
		toID, err := getOrCreate(rel.To)
		if err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO memory_relations (from_entity_id, to_entity_id, relation_type)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (from_entity_id, to_entity_id, relation_type) DO NOTHING`,
			fromID, toID, rel.RelationType,
		)
		if err != nil {
			return nil, err
		}
		res.RelationsCreated += int(tag.RowsAffected())
	}
	return res, tx.Commit(ctx)
}

// ---- Mutations (rename / update / delete) ----
