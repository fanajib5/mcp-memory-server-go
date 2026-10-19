// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package repository

import (
	"context"
	"fmt"
	"strings"

	"mcp-memory-server/internal/entity"
)

// searchTokens splits a query into lowercase literal-substring fallback tokens.
// Split on whitespace only: punctuation stays attached so identifier forms like
// "pg_hba" or "pg_hba.conf" match stored text literally.
func searchTokens(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// searchAggSubquery builds the per-entity aggregation: FTS vector + lowercase
// haystack (name + concatenated observations) + confidence/recency factors.
// Shared by the lexical and hybrid search paths.
// Params: $1 project ("" = all), $2 query (unused here), $3 entityTypes,
// $4 tagsAny.
const searchAggSubquery = `
	  SELECT e.id, e.name, e.entity_type, e.project_id,
	         to_tsvector('simple', e.name || ' ' || coalesce(string_agg(o.content, ' '), '')) AS vec,
	         lower(e.name || ' ' || coalesce(string_agg(o.content, ' '), '')) AS haystack,
	         COALESCE(avg(COALESCE(o.confidence, 1.0)), 1.0) AS avg_conf,
	         exp(-extract(epoch FROM (now() - coalesce(e.last_accessed_at, e.created_at))) / (30 * 86400.0)) AS recency_factor
	  FROM memory_entities e
	  LEFT JOIN memory_observations o ON o.entity_id = e.id
	  WHERE ($1 = '' OR e.project_id = $1)
	    AND (COALESCE(array_length($3::text[], 1), 0) = 0 OR e.entity_type = ANY($3::text[]))
	    AND (COALESCE(array_length($4::text[], 1), 0) = 0 OR e.tags && $4::text[])
	  GROUP BY e.id, e.name, e.entity_type, e.project_id, e.last_accessed_at, e.created_at`

// searchJoinBlock builds the match expressions joined to the aggregation.
//
// Three match paths are OR-ed, because identifier-style lexemes do not
// round-trip through to_tsvector tokenization:
//   - q:      plainto over the raw query (exact identifier lexemes, e.g. 'sekoolah.id')
//   - q_norm: plainto over punctuation-normalized query (hyphen/underscore
//     variants, e.g. 'embedded-postgres' -> 'embedded' & 'postgres')
//   - substr_hit: every whitespace token occurs literally in the haystack
//     (e.g. 'pg_hba' inside stored 'pg_hba.conf', where FTS sees one lexeme)
//
// Params: $2 query, $5 tokens.
const searchJoinBlock = `
	CROSS JOIN plainto_tsquery('simple', $2) AS q
	CROSS JOIN plainto_tsquery('simple', regexp_replace($2, '\W+', ' ', 'g')) AS q_norm
	CROSS JOIN LATERAL (
	  SELECT COALESCE(array_length($5::text[], 1), 0) > 0
	         AND NOT EXISTS (SELECT 1 FROM unnest($5::text[]) tok
	                        WHERE strpos(agg.haystack, tok) = 0) AS substr_hit
	) sub`

// searchScoreExpr ranks the three match paths: full-text rank when available,
// else a small constant for substring-only hits, so identifier fallbacks still
// surface above the LIMIT cut.
const searchScoreExpr = `GREATEST(
	           CASE WHEN agg.vec @@ q THEN ts_rank(agg.vec, q) ELSE 0 END,
	           CASE WHEN agg.vec @@ q_norm THEN ts_rank(agg.vec, q_norm) ELSE 0 END,
	           CASE WHEN sub.substr_hit THEN 0.05 ELSE 0 END)`

func (r *postgresMemory) Search(ctx context.Context, project, query string, limit int, queryVec []float32, entityTypes []string, tagsAny []string) ([]entity.SearchResult, []int, error) {
	if limit <= 0 {
		limit = 20
	}
	if queryVec != nil {
		return r.searchHybrid(ctx, project, query, limit, queryVec, entityTypes, tagsAny)
	}
	return r.searchLexical(ctx, project, query, limit, entityTypes, tagsAny)
}

// searchLexical is the Phase 1 path: match-score × avg_conf × recency. No semantic.
// project == "" searches all namespaces.
func (r *postgresMemory) searchLexical(ctx context.Context, project, query string, limit int, entityTypes []string, tagsAny []string) ([]entity.SearchResult, []int, error) {
	sqlStr := `
		SELECT id, name, entity_type, project_id, avg_conf,
		       (` + searchScoreExpr + ` * avg_conf * recency_factor) AS final_score
		FROM (` + searchAggSubquery + `
		) agg` + searchJoinBlock + `
		WHERE agg.vec @@ q OR agg.vec @@ q_norm OR sub.substr_hit
		ORDER BY (` + searchScoreExpr + ` * agg.avg_conf * agg.recency_factor) DESC
		LIMIT $6`

	rows, err := r.pool.Query(ctx, sqlStr, project, query, entityTypes, tagsAny, searchTokens(query), limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var found []searchRow
	for rows.Next() {
		var rr searchRow
		if err := rows.Scan(&rr.id, &rr.name, &rr.typ, &rr.project, &rr.avgConf, &rr.score); err != nil {
			return nil, nil, err
		}
		found = append(found, rr)
	}
	return r.fetchResults(ctx, found)
}

// searchHybrid fuses lexical (match-score, weight 0.3) and semantic (cosine, weight 0.7).
// Semantic candidates come from ivfflat ANN top-K per observation (cosine sim >= 0.2).
// Entities match via either path; final_score = hybrid × avg_conf × recency.
// project == "" searches all namespaces.
func (r *postgresMemory) searchHybrid(ctx context.Context, project, query string, limit int, queryVec []float32, entityTypes []string, tagsAny []string) ([]entity.SearchResult, []int, error) {
	vecStr := vecLiteral(queryVec)

	sqlStr := `
		SELECT id, name, entity_type, project_id, avg_conf,
		       ((` + searchScoreExpr + ` * 0.3 + COALESCE(sem.sem_score, 0) * 0.7)
		         * avg_conf * recency_factor) AS final_score
		FROM (` + searchAggSubquery + `
		) agg` + searchJoinBlock + `
		LEFT JOIN LATERAL (
		  SELECT max(1 - (o2.embedding <=> $6::vector)) AS sem_score
		  FROM memory_observations o2
		  WHERE o2.entity_id = agg.id AND o2.embedding IS NOT NULL
		) sem ON sem.sem_score >= 0.2
		WHERE agg.vec @@ q OR agg.vec @@ q_norm OR sub.substr_hit OR sem.sem_score IS NOT NULL
		ORDER BY ((` + searchScoreExpr + ` * 0.3 + COALESCE(sem.sem_score, 0) * 0.7)
		         * agg.avg_conf * agg.recency_factor) DESC
		LIMIT $7`

	rows, err := r.pool.Query(ctx, sqlStr, project, query, entityTypes, tagsAny, searchTokens(query), vecStr, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var found []searchRow
	for rows.Next() {
		var rr searchRow
		if err := rows.Scan(&rr.id, &rr.name, &rr.typ, &rr.project, &rr.avgConf, &rr.score); err != nil {
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
	project string
	avgConf float64
	score   float64
}

// fetchResults loads observations + relations for each matched entity row and
// builds SearchResult + id slices. Shared by lexical and hybrid search paths.
func (r *postgresMemory) fetchResults(ctx context.Context, found []searchRow) ([]entity.SearchResult, []int, error) {
	results := make([]entity.SearchResult, 0, len(found))
	ids := make([]int, 0, len(found))
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
			Name: rr.name, Type: rr.typ, Project: rr.project, Observations: observations, Relations: relations,
			Confidence: confPtr, Score: &rr.score,
		})
		ids = append(ids, rr.id)
	}
	return results, ids, nil
}
