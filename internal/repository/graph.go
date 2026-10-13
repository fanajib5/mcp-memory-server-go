// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package repository

import (
	"context"
	"fmt"
	"time"

	"mcp-memory-server/internal/entity"
)

// GetTopEntities returns top entities in the project ordered by recency and confidence.
func (r *postgresMemory) GetTopEntities(ctx context.Context, project string, limit int) ([]entity.SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, entity_type, avg_conf, (avg_conf * recency_factor) AS final_score
		FROM (
		  SELECT e.id, e.name, e.entity_type,
		         COALESCE(avg(COALESCE(o.confidence, 1.0)), 1.0) AS avg_conf,
		         exp(-extract(epoch FROM (now() - coalesce(e.last_accessed_at, e.created_at))) / (30 * 86400.0)) AS recency_factor
		  FROM memory_entities e
		  LEFT JOIN memory_observations o ON o.entity_id = e.id
		  WHERE e.project_id = $1
		  GROUP BY e.id, e.name, e.entity_type, e.last_accessed_at, e.created_at
		) agg
		ORDER BY (avg_conf * recency_factor) DESC, id DESC
		LIMIT $2`, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var found []searchRow
	for rows.Next() {
		var rr searchRow
		if err := rows.Scan(&rr.id, &rr.name, &rr.typ, &rr.avgConf, &rr.score); err != nil {
			return nil, err
		}
		found = append(found, rr)
	}

	results, _, err := r.fetchResults(ctx, found)
	return results, err
}

// GetRecentEntitiesByType returns entities of a given entity_type updated since a cutoff time.
func (r *postgresMemory) GetRecentEntitiesByType(ctx context.Context, project, entityType string, since time.Time) ([]entity.Entity, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.id, e.name, e.entity_type
		FROM memory_entities e
		WHERE e.project_id = $1 AND e.entity_type = $2 AND e.updated_at >= $3
		ORDER BY e.updated_at DESC, e.id DESC`, project, entityType, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type entRow struct {
		id   int
		name string
		typ  string
	}
	var entities []entRow
	for rows.Next() {
		var er entRow
		if err := rows.Scan(&er.id, &er.name, &er.typ); err != nil {
			return nil, err
		}
		entities = append(entities, er)
	}

	var out []entity.Entity
	for _, e := range entities {
		obsRows, err := r.pool.Query(ctx,
			`SELECT content FROM memory_observations WHERE entity_id = $1 ORDER BY created_at`, e.id)
		if err != nil {
			return nil, err
		}
		var observations []string
		for obsRows.Next() {
			var c string
			if err := obsRows.Scan(&c); err != nil {
				obsRows.Close()
				return nil, err
			}
			observations = append(observations, c)
		}
		obsRows.Close()

		out = append(out, entity.Entity{
			Name:         e.name,
			EntityType:   e.typ,
			Observations: observations,
		})
	}

	return out, nil
}

// ReadGraph returns the graph for one project, or all projects when project == "".
func (r *postgresMemory) ReadGraph(ctx context.Context, project string) (*entity.FullGraph, error) {
	entQuery := `SELECT id, name, entity_type FROM memory_entities ORDER BY name`
	var entArgs []any
	if project != "" {
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
	if project != "" {
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
	if project != "" {
		relQuery += ` WHERE fe.project_id = $1 AND te.project_id = $1`
		relArgs = []any{project}
	}
	relRows, err := r.pool.Query(ctx, relQuery, relArgs...)
	if err != nil {
		return nil, err
	}
	defer relRows.Close()
	var relations []string
	for relRows.Next() {
		var from, relType, to string
		if err := relRows.Scan(&from, &relType, &to); err != nil {
			return nil, err
		}
		relations = append(relations, fmt.Sprintf("%s --%s--> %s", from, relType, to))
	}

	graph := &entity.FullGraph{Relations: relations}
	for _, e := range entities {
		graph.Entities = append(graph.Entities, entity.Entity{
			Name: e.name, EntityType: e.typ, Observations: obsByEntity[e.id],
		})
	}
	return graph, nil
}
