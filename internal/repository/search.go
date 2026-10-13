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

func (r *postgresMemory) Search(ctx context.Context, project, query string, limit int, queryVec []float32, entityTypes []string, tagsAny []string) ([]entity.SearchResult, []int, error) {
	if limit <= 0 {
		limit = 20
	}
	if queryVec != nil {
		return r.searchHybrid(ctx, project, query, limit, queryVec, entityTypes, tagsAny)
	}
	return r.searchLexical(ctx, project, query, limit, entityTypes, tagsAny)
}

// searchLexical is the Phase 1 path: ts_rank × avg_conf × recency. No semantic.
func (r *postgresMemory) searchLexical(ctx context.Context, project, query string, limit int, entityTypes []string, tagsAny []string) ([]entity.SearchResult, []int, error) {
	sqlStr := `
		SELECT id, name, entity_type, avg_conf,
		       (ts_rank(vec, q) * avg_conf * recency_factor) AS final_score
		FROM (
		  SELECT e.id, e.name, e.entity_type,
		         to_tsvector('simple', e.name || ' ' || coalesce(string_agg(o.content, ' '), '')) AS vec,
		         COALESCE(avg(COALESCE(o.confidence, 1.0)), 1.0) AS avg_conf,
		         exp(-extract(epoch FROM (now() - coalesce(e.last_accessed_at, e.created_at))) / (30 * 86400.0)) AS recency_factor
		  FROM memory_entities e
		  LEFT JOIN memory_observations o ON o.entity_id = e.id
		  WHERE e.project_id = $1`

	args := []any{project, query}
	argIdx := 3

	if len(entityTypes) > 0 {
		sqlStr += fmt.Sprintf(" AND e.entity_type = ANY($%d)", argIdx)
		args = append(args, entityTypes)
		argIdx++
	}
	if len(tagsAny) > 0 {
		sqlStr += fmt.Sprintf(" AND e.tags && $%d", argIdx)
		args = append(args, tagsAny)
		argIdx++
	}

	sqlStr += fmt.Sprintf(`
		  GROUP BY e.id, e.name, e.entity_type, e.last_accessed_at, e.created_at
		) agg
		CROSS JOIN plainto_tsquery('simple', regexp_replace($2, '\\W+', ' ', 'g')) AS q
		WHERE agg.vec @@ q
		ORDER BY (ts_rank(agg.vec, q) * agg.avg_conf * agg.recency_factor) DESC
		LIMIT $%d`, argIdx)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var found []searchRow
	for rows.Next() {
		var rr searchRow
		if err := rows.Scan(&rr.id, &rr.name, &rr.typ, &rr.avgConf, &rr.score); err != nil {
			return nil, nil, err
		}
		found = append(found, rr)
	}
	return r.fetchResults(ctx, found)
}

// searchHybrid fuses lexical (ts_rank, weight 0.3) and semantic (cosine, weight 0.7).
// Semantic candidates come from ivfflat ANN top-K per observation (cosine sim >= 0.2).
// Entities match via either path; final_score = hybrid × avg_conf × recency.
func (r *postgresMemory) searchHybrid(ctx context.Context, project, query string, limit int, queryVec []float32, entityTypes []string, tagsAny []string) ([]entity.SearchResult, []int, error) {
	vecStr := vecLiteral(queryVec)

	sqlStr := `
		SELECT id, name, entity_type, avg_conf,
		       (ts_rank(agg.vec, q) * 0.3 + COALESCE(sem.sem_score, 0) * 0.7) * agg.avg_conf * agg.recency_factor AS final_score
		FROM (
		  SELECT e.id, e.name, e.entity_type,
		         to_tsvector('simple', e.name || ' ' || coalesce(string_agg(o.content, ' '), '')) AS vec,
		         COALESCE(avg(COALESCE(o.confidence, 1.0)), 1.0) AS avg_conf,
		         exp(-extract(epoch FROM (now() - coalesce(e.last_accessed_at, e.created_at))) / (30 * 86400.0)) AS recency_factor
		  FROM memory_entities e
		  LEFT JOIN memory_observations o ON o.entity_id = e.id
		  WHERE e.project_id = $1`

	args := []any{project, query, vecStr}
	argIdx := 4

	if len(entityTypes) > 0 {
		sqlStr += fmt.Sprintf(" AND e.entity_type = ANY($%d)", argIdx)
		args = append(args, entityTypes)
		argIdx++
	}
	if len(tagsAny) > 0 {
		sqlStr += fmt.Sprintf(" AND e.tags && $%d", argIdx)
		args = append(args, tagsAny)
		argIdx++
	}

	sqlStr += fmt.Sprintf(`
		  GROUP BY e.id, e.name, e.entity_type, e.last_accessed_at, e.created_at
		) agg
		CROSS JOIN plainto_tsquery('simple', regexp_replace($2, '\\W+', ' ', 'g')) AS q
		LEFT JOIN LATERAL (
		  SELECT max(1 - (o2.embedding <=> $3::vector)) AS sem_score
		  FROM memory_observations o2
		  WHERE o2.entity_id = agg.id AND o2.embedding IS NOT NULL
		) sem ON sem.sem_score >= 0.2
		WHERE agg.vec @@ q OR sem.sem_score IS NOT NULL
		ORDER BY (ts_rank(agg.vec, q) * 0.3 + COALESCE(sem.sem_score, 0) * 0.7) * agg.avg_conf * agg.recency_factor DESC
		LIMIT $%d`, argIdx)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var found []searchRow
	for rows.Next() {
		var rr searchRow
		if err := rows.Scan(&rr.id, &rr.name, &rr.typ, &rr.avgConf, &rr.score); err != nil {
			return nil, nil, err
		}
		found = append(found, rr)
	}
	return r.fetchResults(ctx, found)
}

// searchRow holds per-entity scoring data from the search CTE.
type searchRow struct {
	id      int
	name    string
	typ     string
	avgConf float64
	score   float64
}

// fetchResults loads observations + relations for each matched entity row and
// builds SearchResult + id slices. Shared by lexical and hybrid search paths.
func (r *postgresMemory) fetchResults(ctx context.Context, found []searchRow) ([]entity.SearchResult, []int, error) {
	var results []entity.SearchResult
	var ids []int
	for _, rr := range found {
		obsRows, err := r.pool.Query(ctx,
			`SELECT content FROM memory_observations WHERE entity_id = $1 ORDER BY created_at`, rr.id)
		if err != nil {
			return nil, nil, err
		}
		var observations []string
		for obsRows.Next() {
			var c string
			if err := obsRows.Scan(&c); err != nil {
				obsRows.Close()
				return nil, nil, err
			}
			observations = append(observations, c)
		}
		obsRows.Close()

		var relations []string
		outRows, err := r.pool.Query(ctx, `
			SELECT r.relation_type, te.name FROM memory_relations r
			JOIN memory_entities te ON te.id = r.to_entity_id
			WHERE r.from_entity_id = $1`, rr.id)
		if err != nil {
			return nil, nil, err
		}
		for outRows.Next() {
			var relType, toName string
			if err := outRows.Scan(&relType, &toName); err != nil {
				outRows.Close()
				return nil, nil, err
			}
			relations = append(relations, fmt.Sprintf("%s --%s--> %s", rr.name, relType, toName))
		}
		outRows.Close()

		inRows, err := r.pool.Query(ctx, `
			SELECT r.relation_type, fe.name FROM memory_relations r
			JOIN memory_entities fe ON fe.id = r.from_entity_id
			WHERE r.to_entity_id = $1`, rr.id)
		if err != nil {
			return nil, nil, err
		}
		for inRows.Next() {
			var relType, fromName string
			if err := inRows.Scan(&relType, &fromName); err != nil {
				inRows.Close()
				return nil, nil, err
			}
			relations = append(relations, fmt.Sprintf("%s --%s--> %s", fromName, relType, rr.name))
		}
		inRows.Close()

		var confPtr *float64
		if rr.avgConf != 1.0 {
			c := rr.avgConf
			confPtr = &c
		}
		results = append(results, entity.SearchResult{
			Name: rr.name, Type: rr.typ, Observations: observations, Relations: relations,
			Confidence: confPtr, Score: &rr.score,
		})
		ids = append(ids, rr.id)
	}
	return results, ids, nil
}
