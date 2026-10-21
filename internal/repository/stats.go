// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcp-memory-server/internal/entity"
)

// StatsRepository holds read-only queries that feed the web UI (dashboard,
// entity detail, graph viz). Kept separate from MemoryRepository so the write
// path stays focused and UI metrics can evolve independently.
type StatsRepository interface {
	GetEntityDetail(ctx context.Context, project, name string) (*entity.EntityDetail, error)
	ListEntities(ctx context.Context, project, typeFilter, query string, limit int) ([]entity.EntitySummary, error)
	DashboardMetrics(ctx context.Context, project string) (*entity.Metrics, error)
	GraphData(ctx context.Context, project string) (*entity.GraphPayload, error)
	ObservationByID(ctx context.Context, project string, id int) (content, entityName string, err error)
}

type postgresStats struct {
	pool *pgxpool.Pool
}

// NewStatsRepository builds a pgx-backed StatsRepository.
func NewStatsRepository(pool *pgxpool.Pool) StatsRepository {
	return &postgresStats{pool: pool}
}

func (r *postgresStats) GetEntityDetail(ctx context.Context, project, name string) (*entity.EntityDetail, error) {
	d := &entity.EntityDetail{}
	err := r.pool.QueryRow(ctx,
		`SELECT name, entity_type FROM memory_entities WHERE project_id = $1 AND name = $2`, project, name,
	).Scan(&d.Name, &d.Type)
	if err != nil {
		return nil, err
	}

	oRows, err := r.pool.Query(ctx, `
		SELECT o.id, o.content, o.confidence FROM memory_observations o
		JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND e.name = $2 ORDER BY o.created_at`, project, name)
	if err != nil {
		return nil, err
	}
	for oRows.Next() {
		var o entity.EntityDetailObservation
		if err := oRows.Scan(&o.ID, &o.Content, &o.Confidence); err != nil {
			oRows.Close()
			return nil, err
		}
		d.Observations = append(d.Observations, o)
	}
	oRows.Close()

	outRows, err := r.pool.Query(ctx, `
		SELECT r.id, r.relation_type, te.name FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		JOIN memory_entities te ON te.id = r.to_entity_id
		WHERE fe.project_id = $1 AND fe.name = $2`, project, name)
	if err != nil {
		return nil, err
	}
	for outRows.Next() {
		var rel entity.EntityDetailRelation
		if err := outRows.Scan(&rel.ID, &rel.Type, &rel.Other); err != nil {
			outRows.Close()
			return nil, err
		}
		rel.Direction = "out"
		d.Relations = append(d.Relations, rel)
	}
	outRows.Close()

	inRows, err := r.pool.Query(ctx, `
		SELECT r.id, r.relation_type, fe.name FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		JOIN memory_entities te ON te.id = r.to_entity_id
		WHERE te.project_id = $1 AND te.name = $2`, project, name)
	if err != nil {
		return nil, err
	}
	for inRows.Next() {
		var rel entity.EntityDetailRelation
		if err := inRows.Scan(&rel.ID, &rel.Type, &rel.Other); err != nil {
			inRows.Close()
			return nil, err
		}
		rel.Direction = "in"
		d.Relations = append(d.Relations, rel)
	}
	inRows.Close()

	return d, nil
}

func (r *postgresStats) ListEntities(ctx context.Context, project, typeFilter, query string, limit int) ([]entity.EntitySummary, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT e.name, e.entity_type,
		       COUNT(DISTINCT o.id) AS obs, COUNT(DISTINCT r.id) AS rel,
		       to_char(coalesce(e.last_accessed_at, e.created_at), 'YYYY-MM-DD"T"HH24:MI:SS') AS accessed
		FROM memory_entities e
		LEFT JOIN memory_observations o ON o.entity_id = e.id
		LEFT JOIN memory_relations r ON r.from_entity_id = e.id
		WHERE e.project_id = $1
		  AND ($2 = '' OR e.entity_type = $2)
		  AND ($3 = '' OR e.name ILIKE '%' || $3 || '%')
		GROUP BY e.id, e.name, e.entity_type, e.last_accessed_at, e.created_at
		ORDER BY e.name
		LIMIT $4`, project, typeFilter, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.EntitySummary
	for rows.Next() {
		var s entity.EntitySummary
		var accessed string
		if err := rows.Scan(&s.Name, &s.Type, &s.ObsCount, &s.RelCount, &accessed); err != nil {
			return nil, err
		}
		s.LastAccessed = &accessed
		out = append(out, s)
	}
	return out, nil
}

func (r *postgresStats) ObservationByID(ctx context.Context, project string, id int) (content, entityName string, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT o.content, e.name FROM memory_observations o
		JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND o.id = $2`, project, id,
	).Scan(&content, &entityName)
	return
}
