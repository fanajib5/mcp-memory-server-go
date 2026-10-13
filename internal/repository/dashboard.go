// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package repository

import (
	"context"

	"mcp-memory-server/internal/entity"
)

func (r *postgresStats) DashboardMetrics(ctx context.Context, project string) (*entity.Metrics, error) {
	m := &entity.Metrics{}

	if err := r.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM memory_entities WHERE project_id = $1),
		  (SELECT COUNT(*) FROM memory_observations o JOIN memory_entities e ON e.id = o.entity_id WHERE e.project_id = $1),
		  (SELECT COUNT(*) FROM memory_relations r JOIN memory_entities e ON e.id = r.from_entity_id WHERE e.project_id = $1),
		  (SELECT COUNT(DISTINCT r.relation_type) FROM memory_relations r JOIN memory_entities e ON e.id = r.from_entity_id WHERE e.project_id = $1),
		  (SELECT COUNT(*) FROM memory_entities),
		  (SELECT COUNT(*) FROM memory_observations)`, project,
	).Scan(&m.Entities, &m.Observations, &m.Relations, &m.RelationTypes, &m.AllEntities, &m.AllObservations); err != nil {
		return nil, err
	}
	if m.Entities > 0 {
		m.AvgObs = float64(m.Observations) / float64(m.Entities)
		m.AvgRel = float64(m.Relations) / float64(m.Entities)
	}

	r2, err := r.pool.Query(ctx, `SELECT entity_type, COUNT(*) FROM memory_entities WHERE project_id = $1 GROUP BY entity_type ORDER BY COUNT(*) DESC`, project)
	if err != nil {
		return nil, err
	}
	for r2.Next() {
		var tc entity.TypeCount
		if err := r2.Scan(&tc.Type, &tc.Count); err != nil {
			r2.Close()
			return nil, err
		}
		m.ByType = append(m.ByType, tc)
	}
	r2.Close()

	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM memory_entities e
		WHERE e.project_id = $1
		  AND NOT EXISTS (SELECT 1 FROM memory_relations r WHERE r.from_entity_id = e.id)
		  AND NOT EXISTS (SELECT 1 FROM memory_relations r WHERE r.to_entity_id = e.id)`, project,
	).Scan(&m.Orphans); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM memory_entities e
		WHERE e.project_id = $1
		  AND NOT EXISTS (SELECT 1 FROM memory_observations o WHERE o.entity_id = e.id)`, project,
	).Scan(&m.Sparse); err != nil {
		return nil, err
	}

	r2, err = r.pool.Query(ctx, `
		SELECT e.name, e.entity_type, COUNT(o.id) c FROM memory_entities e
		LEFT JOIN memory_observations o ON o.entity_id = e.id
		WHERE e.project_id = $1
		GROUP BY e.id, e.name, e.entity_type
		ORDER BY c DESC, e.name LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r2.Next() {
		var s entity.EntitySummary
		if err := r2.Scan(&s.Name, &s.Type, &s.ObsCount); err != nil {
			r2.Close()
			return nil, err
		}
		m.TopByObs = append(m.TopByObs, s)
	}
	r2.Close()

	r2, err = r.pool.Query(ctx, `
		SELECT e.name, e.entity_type, x.cnt FROM memory_entities e
		JOIN LATERAL (
		  SELECT ((SELECT COUNT(*) FROM memory_relations r WHERE r.from_entity_id = e.id)
		        + (SELECT COUNT(*) FROM memory_relations r WHERE r.to_entity_id = e.id)) AS cnt
		) x ON true
		WHERE e.project_id = $1
		ORDER BY x.cnt DESC, e.name LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r2.Next() {
		var s entity.EntitySummary
		if err := r2.Scan(&s.Name, &s.Type, &s.RelCount); err != nil {
			r2.Close()
			return nil, err
		}
		m.TopByRel = append(m.TopByRel, s)
	}
	r2.Close()

	r2, err = r.pool.Query(ctx, `
		SELECT r.relation_type, COUNT(*) FROM memory_relations r
		JOIN memory_entities e ON e.id = r.from_entity_id
		WHERE e.project_id = $1 GROUP BY r.relation_type ORDER BY COUNT(*) DESC LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r2.Next() {
		var tc entity.TypeCount
		if err := r2.Scan(&tc.Type, &tc.Count); err != nil {
			r2.Close()
			return nil, err
		}
		m.RelationTypesX = append(m.RelationTypesX, tc)
	}
	r2.Close()

	r2, err = r.pool.Query(ctx, `
		SELECT e.name, e.entity_type FROM memory_entities e
		WHERE e.project_id = $1 ORDER BY e.created_at DESC LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r2.Next() {
		var s entity.EntitySummary
		if err := r2.Scan(&s.Name, &s.Type); err != nil {
			r2.Close()
			return nil, err
		}
		m.RecentEntities = append(m.RecentEntities, s)
	}
	r2.Close()

	entByDay := map[string]int{}
	r2, err = r.pool.Query(ctx, `
		SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') d, COUNT(*)
		FROM memory_entities WHERE project_id = $1 AND created_at >= now() - interval '30 day'
		GROUP BY 1`, project)
	if err != nil {
		return nil, err
	}
	for r2.Next() {
		var d string
		var c int
		if err := r2.Scan(&d, &c); err != nil {
			r2.Close()
			return nil, err
		}
		entByDay[d] = c
	}
	r2.Close()
	r2, err = r.pool.Query(ctx, `
		SELECT to_char(date_trunc('day', o.created_at), 'YYYY-MM-DD') d, COUNT(*)
		FROM memory_observations o JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND o.created_at >= now() - interval '30 day'
		GROUP BY 1`, project)
	if err != nil {
		return nil, err
	}
	obsByDay := map[string]int{}
	for r2.Next() {
		var d string
		var c int
		if err := r2.Scan(&d, &c); err != nil {
			r2.Close()
			return nil, err
		}
		obsByDay[d] = c
	}
	r2.Close()
	allDays := map[string]bool{}
	for d := range entByDay {
		allDays[d] = true
	}
	for d := range obsByDay {
		allDays[d] = true
	}
	for d := range allDays {
		m.Growth = append(m.Growth, entity.DayCount{Day: d, Entities: entByDay[d], Observations: obsByDay[d]})
	}

	return m, nil
}

func (r *postgresStats) GraphData(ctx context.Context, project string) (*entity.GraphPayload, error) {
	g := &entity.GraphPayload{}

	rows, err := r.pool.Query(ctx, `SELECT id, name, entity_type FROM memory_entities WHERE project_id = $1 ORDER BY name`, project)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n entity.GraphNode
		if err := rows.Scan(&n.ID, &n.Label, &n.Group); err != nil {
			rows.Close()
			return nil, err
		}
		g.Nodes = append(g.Nodes, n)
	}
	rows.Close()

	rrows, err := r.pool.Query(ctx, `
		SELECT r.from_entity_id, r.to_entity_id, r.relation_type FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		WHERE fe.project_id = $1`, project)
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var e entity.GraphEdge
		if err := rrows.Scan(&e.From, &e.To, &e.Label); err != nil {
			rrows.Close()
			return nil, err
		}
		g.Edges = append(g.Edges, e)
	}
	rrows.Close()

	return g, nil
}
