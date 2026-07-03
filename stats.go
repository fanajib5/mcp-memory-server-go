package main

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type EntityDetailObservation struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
}

type EntityDetailRelation struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Other     string `json:"other"`
	Direction string `json:"direction"` // "out" or "in"
}

type EntityDetail struct {
	Name         string                    `json:"name"`
	Type         string                    `json:"type"`
	Observations []EntityDetailObservation `json:"observations"`
	Relations    []EntityDetailRelation    `json:"relations"`
}

type EntitySummary struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	ObsCount int    `json:"obsCount"`
	RelCount int    `json:"relCount"`
}

type DayCount struct {
	Day          string `json:"day"`
	Entities     int    `json:"entities"`
	Observations int    `json:"observations"`
}

type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type Metrics struct {
	Entities        int             `json:"entities"`
	Observations    int             `json:"observations"`
	Relations       int             `json:"relations"`
	RelationTypes   int             `json:"relationTypes"`
	AllEntities     int             `json:"allEntities"`
	AllObservations int             `json:"allObservations"`
	ByType          []TypeCount     `json:"byType"`
	AvgObs          float64         `json:"avgObs"`
	AvgRel          float64         `json:"avgRel"`
	Orphans         int             `json:"orphans"`
	Sparse          int             `json:"sparse"`
	TopByObs        []EntitySummary `json:"topByObs"`
	TopByRel        []EntitySummary `json:"topByRel"`
	Growth          []DayCount      `json:"growth"`
	RelationTypesX  []TypeCount     `json:"relationTypeCounts"`
	RecentEntities  []EntitySummary `json:"recentEntities"`
}

func GetEntityDetail(ctx context.Context, pool *pgxpool.Pool, project, name string) (*EntityDetail, error) {
	project = defaultProject(project)
	d := &EntityDetail{}
	err := pool.QueryRow(ctx,
		`SELECT name, entity_type FROM memory_entities WHERE project_id = $1 AND name = $2`, project, name,
	).Scan(&d.Name, &d.Type)
	if err != nil {
		return nil, err
	}

	oRows, err := pool.Query(ctx, `
		SELECT o.id, o.content FROM memory_observations o
		JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND e.name = $2 ORDER BY o.created_at`, project, name)
	if err != nil {
		return nil, err
	}
	for oRows.Next() {
		var o EntityDetailObservation
		if err := oRows.Scan(&o.ID, &o.Content); err != nil {
			oRows.Close()
			return nil, err
		}
		d.Observations = append(d.Observations, o)
	}
	oRows.Close()

	outRows, err := pool.Query(ctx, `
		SELECT r.id, r.relation_type, te.name FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		JOIN memory_entities te ON te.id = r.to_entity_id
		WHERE fe.project_id = $1 AND fe.name = $2`, project, name)
	if err != nil {
		return nil, err
	}
	for outRows.Next() {
		var rel EntityDetailRelation
		if err := outRows.Scan(&rel.ID, &rel.Type, &rel.Other); err != nil {
			outRows.Close()
			return nil, err
		}
		rel.Direction = "out"
		d.Relations = append(d.Relations, rel)
	}
	outRows.Close()

	inRows, err := pool.Query(ctx, `
		SELECT r.id, r.relation_type, fe.name FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		JOIN memory_entities te ON te.id = r.to_entity_id
		WHERE te.project_id = $1 AND te.name = $2`, project, name)
	if err != nil {
		return nil, err
	}
	for inRows.Next() {
		var rel EntityDetailRelation
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

func ListEntities(ctx context.Context, pool *pgxpool.Pool, project, typeFilter, query string, limit int) ([]EntitySummary, error) {
	project = defaultProject(project)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		SELECT e.name, e.entity_type,
		       COUNT(DISTINCT o.id) AS obs, COUNT(DISTINCT r.id) AS rel
		FROM memory_entities e
		LEFT JOIN memory_observations o ON o.entity_id = e.id
		LEFT JOIN memory_relations r ON r.from_entity_id = e.id
		WHERE e.project_id = $1
		  AND ($2 = '' OR e.entity_type = $2)
		  AND ($3 = '' OR e.name ILIKE '%' || $3 || '%')
		GROUP BY e.id, e.name, e.entity_type
		ORDER BY e.name
		LIMIT $4`, project, typeFilter, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntitySummary
	for rows.Next() {
		var s EntitySummary
		if err := rows.Scan(&s.Name, &s.Type, &s.ObsCount, &s.RelCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func DashboardMetrics(ctx context.Context, pool *pgxpool.Pool, project string) (*Metrics, error) {
	project = defaultProject(project)
	m := &Metrics{}

	if err := pool.QueryRow(ctx, `
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

	r, err := pool.Query(ctx, `SELECT entity_type, COUNT(*) FROM memory_entities WHERE project_id = $1 GROUP BY entity_type ORDER BY COUNT(*) DESC`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var tc TypeCount
		if err := r.Scan(&tc.Type, &tc.Count); err != nil {
			r.Close()
			return nil, err
		}
		m.ByType = append(m.ByType, tc)
	}
	r.Close()

	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM memory_entities e
		WHERE e.project_id = $1
		  AND NOT EXISTS (SELECT 1 FROM memory_relations r WHERE r.from_entity_id = e.id)
		  AND NOT EXISTS (SELECT 1 FROM memory_relations r WHERE r.to_entity_id = e.id)`, project,
	).Scan(&m.Orphans); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM memory_entities e
		WHERE e.project_id = $1
		  AND NOT EXISTS (SELECT 1 FROM memory_observations o WHERE o.entity_id = e.id)`, project,
	).Scan(&m.Sparse); err != nil {
		return nil, err
	}

	r, err = pool.Query(ctx, `
		SELECT e.name, e.entity_type, COUNT(o.id) c FROM memory_entities e
		LEFT JOIN memory_observations o ON o.entity_id = e.id
		WHERE e.project_id = $1
		GROUP BY e.id, e.name, e.entity_type
		ORDER BY c DESC, e.name LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var s EntitySummary
		if err := r.Scan(&s.Name, &s.Type, &s.ObsCount); err != nil {
			r.Close()
			return nil, err
		}
		m.TopByObs = append(m.TopByObs, s)
	}
	r.Close()

	r, err = pool.Query(ctx, `
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
	for r.Next() {
		var s EntitySummary
		if err := r.Scan(&s.Name, &s.Type, &s.RelCount); err != nil {
			r.Close()
			return nil, err
		}
		m.TopByRel = append(m.TopByRel, s)
	}
	r.Close()

	r, err = pool.Query(ctx, `
		SELECT r.relation_type, COUNT(*) FROM memory_relations r
		JOIN memory_entities e ON e.id = r.from_entity_id
		WHERE e.project_id = $1 GROUP BY r.relation_type ORDER BY COUNT(*) DESC LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var tc TypeCount
		if err := r.Scan(&tc.Type, &tc.Count); err != nil {
			r.Close()
			return nil, err
		}
		m.RelationTypesX = append(m.RelationTypesX, tc)
	}
	r.Close()

	r, err = pool.Query(ctx, `
		SELECT e.name, e.entity_type FROM memory_entities e
		WHERE e.project_id = $1 ORDER BY e.created_at DESC LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var s EntitySummary
		if err := r.Scan(&s.Name, &s.Type); err != nil {
			r.Close()
			return nil, err
		}
		m.RecentEntities = append(m.RecentEntities, s)
	}
	r.Close()

	entByDay := map[string]int{}
	r, err = pool.Query(ctx, `
		SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') d, COUNT(*)
		FROM memory_entities WHERE project_id = $1 AND created_at >= now() - interval '30 day'
		GROUP BY 1`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var d string
		var c int
		if err := r.Scan(&d, &c); err != nil {
			r.Close()
			return nil, err
		}
		entByDay[d] = c
	}
	r.Close()
	r, err = pool.Query(ctx, `
		SELECT to_char(date_trunc('day', o.created_at), 'YYYY-MM-DD') d, COUNT(*)
		FROM memory_observations o JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND o.created_at >= now() - interval '30 day'
		GROUP BY 1`, project)
	if err != nil {
		return nil, err
	}
	obsByDay := map[string]int{}
	for r.Next() {
		var d string
		var c int
		if err := r.Scan(&d, &c); err != nil {
			r.Close()
			return nil, err
		}
		obsByDay[d] = c
	}
	r.Close()
	allDays := map[string]bool{}
	for d := range entByDay {
		allDays[d] = true
	}
	for d := range obsByDay {
		allDays[d] = true
	}
	for d := range allDays {
		m.Growth = append(m.Growth, DayCount{Day: d, Entities: entByDay[d], Observations: obsByDay[d]})
	}

	return m, nil
}
