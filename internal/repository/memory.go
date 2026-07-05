package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcp-memory-server/internal/entity"
)

// MemoryRepository is the data-access boundary for the knowledge graph.
// Implementations receive already-normalized values (project non-empty, entity
// types/relation types normalized) — domain rules live in the usecase layer.
type MemoryRepository interface {
	CreateEntities(ctx context.Context, project string, entities []entity.EntityInput) ([]string, error)
	AddObservations(ctx context.Context, project, entityName string, observations []string, confidences []float64) error
	CreateRelations(ctx context.Context, project string, relations []entity.RelationInput) ([]string, error)
	DeleteEntities(ctx context.Context, project string, names []string) error
	Search(ctx context.Context, project, query string, limit int) ([]entity.SearchResult, []int, error)
	ReadGraph(ctx context.Context, project string) (*entity.FullGraph, error)
	Export(ctx context.Context, project string) (*entity.ExportPayload, error)
	Import(ctx context.Context, project string, g *entity.ExportPayload) (*entity.ImportResult, error)
	UpdateEntity(ctx context.Context, project, oldName, newName, entityType string) error
	DeleteObservation(ctx context.Context, project string, id int) error
	UpdateObservation(ctx context.Context, project string, id int, content string, newConfidence *float64) error
	DeleteRelation(ctx context.Context, project string, id int) error
	DeleteObservationByContent(ctx context.Context, project, entityName, content string) error
	UpdateObservationByContent(ctx context.Context, project, entityName, oldContent, newContent string, newConfidence *float64) error
	DeleteRelationByTriple(ctx context.Context, project, from, to, relationType string) error
	TouchAccessed(ctx context.Context, entityIDs []int) error
}

// postgresMemory is the pgx-backed MemoryRepository.
type postgresMemory struct {
	pool *pgxpool.Pool
}

// NewMemoryRepository builds a pgx-backed MemoryRepository.
func NewMemoryRepository(pool *pgxpool.Pool) MemoryRepository {
	return &postgresMemory{pool: pool}
}

func (r *postgresMemory) CreateEntities(ctx context.Context, project string, entities []entity.EntityInput) ([]string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var created []string
	for _, e := range entities {
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, e.Name).Scan(&id)
		if err != nil {
			if err := tx.QueryRow(ctx,
				`INSERT INTO memory_entities (project_id, name, entity_type) VALUES ($1, $2, $3) RETURNING id`,
				project, e.Name, e.Type,
			).Scan(&id); err != nil {
				return nil, fmt.Errorf("create entity %q: %w", e.Name, err)
			}
		}
		for i, obs := range e.Observations {
			var conf any
			if i < len(e.Confidences) {
				conf = e.Confidences[i]
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO memory_observations (entity_id, content, confidence) VALUES ($1, $2, $3)`, id, obs, conf,
			); err != nil {
				return nil, fmt.Errorf("add observation to %q: %w", e.Name, err)
			}
		}
		created = append(created, e.Name)
	}
	return created, tx.Commit(ctx)
}

func (r *postgresMemory) AddObservations(ctx context.Context, project, entityName string, observations []string, confidences []float64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

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
		if i < len(confidences) {
			conf = confidences[i]
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO memory_observations (entity_id, content, confidence) VALUES ($1, $2, $3)`, id, obs, conf,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *postgresMemory) CreateRelations(ctx context.Context, project string, relations []entity.RelationInput) ([]string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

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

func (r *postgresMemory) Search(ctx context.Context, project, query string, limit int) ([]entity.SearchResult, []int, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, entity_type, avg_conf,
		       (ts_rank(vec, q) * avg_conf * recency_factor) AS final_score
		FROM (
		  SELECT e.id, e.name, e.entity_type,
		         to_tsvector('simple', e.name || ' ' || coalesce(string_agg(o.content, ' '), '')) AS vec,
		         COALESCE(avg(COALESCE(o.confidence, 1.0)), 1.0) AS avg_conf,
		         exp(-extract(epoch FROM (now() - coalesce(e.last_accessed_at, e.created_at))) / (30 * 86400.0)) AS recency_factor
		  FROM memory_entities e
		  LEFT JOIN memory_observations o ON o.entity_id = e.id
		  WHERE e.project_id = $1
		  GROUP BY e.id, e.name, e.entity_type, e.last_accessed_at, e.created_at
		) agg
		CROSS JOIN websearch_to_tsquery('simple', $2) AS q
		WHERE agg.vec @@ q
		ORDER BY (ts_rank(agg.vec, q) * agg.avg_conf * agg.recency_factor) DESC
		LIMIT $3`, project, query, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type row struct {
		id      int
		name    string
		typ     string
		avgConf float64
		score   float64
	}
	var found []row
	for rows.Next() {
		var rr row
		if err := rows.Scan(&rr.id, &rr.name, &rr.typ, &rr.avgConf, &rr.score); err != nil {
			return nil, nil, err
		}
		found = append(found, rr)
	}

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

// TouchAccessed bumps last_accessed_at for the given entity IDs. Best-effort:
// the caller (usecase) should ignore errors so a search never fails due to this.
func (r *postgresMemory) TouchAccessed(ctx context.Context, entityIDs []int) error {
	if len(entityIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `UPDATE memory_entities SET last_accessed_at = now() WHERE id = ANY($1::int[])`, entityIDs)
	return err
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
	defer tx.Rollback(ctx)

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

// UpdateEntity renames and/or changes the type of an entity within a project.
// Rejects a rename if the new name already exists in the same project.
func (r *postgresMemory) UpdateEntity(ctx context.Context, project, oldName, newName, entityType string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var id int
	err = tx.QueryRow(ctx,
		`SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, oldName,
	).Scan(&id)
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
	return tx.Commit(ctx)
}

func (r *postgresMemory) DeleteObservation(ctx context.Context, project string, id int) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM memory_observations o
		USING memory_entities e
		WHERE o.entity_id = e.id AND e.project_id = $1 AND o.id = $2`, project, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation not found in project %q", project)
	}
	return nil
}

func (r *postgresMemory) UpdateObservation(ctx context.Context, project string, id int, content string, newConfidence *float64) error {
	confExpr := "o.confidence"
	var args []any
	args = append(args, project, id, content)
	if newConfidence != nil {
		confExpr = fmt.Sprintf("$%d", len(args)+1)
		args = append(args, *newConfidence)
	}
	tag, err := r.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE memory_observations o SET content = $3, confidence = %s
		FROM memory_entities e
		WHERE o.entity_id = e.id AND e.project_id = $1 AND o.id = $2`, confExpr), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation not found in project %q", project)
	}
	return nil
}

func (r *postgresMemory) DeleteRelation(ctx context.Context, project string, id int) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM memory_relations r
		USING memory_entities fe
		WHERE r.from_entity_id = fe.id AND fe.project_id = $1 AND r.id = $2`, project, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("relation not found in project %q", project)
	}
	return nil
}

func (r *postgresMemory) DeleteObservationByContent(ctx context.Context, project, entityName, content string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM memory_observations o
		USING memory_entities e
		WHERE o.entity_id = e.id AND e.project_id = $1 AND e.name = $2 AND o.content = $3`,
		project, entityName, content)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation %q not found on entity %q in project %q", content, entityName, project)
	}
	return nil
}

func (r *postgresMemory) UpdateObservationByContent(ctx context.Context, project, entityName, oldContent, newContent string, newConfidence *float64) error {
	confExpr := "o.confidence"
	var args []any
	args = append(args, project, entityName, oldContent, newContent)
	if newConfidence != nil {
		confExpr = fmt.Sprintf("$%d", len(args)+1)
		args = append(args, *newConfidence)
	}
	tag, err := r.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE memory_observations o SET content = $4, confidence = %s
		FROM memory_entities e
		WHERE o.entity_id = e.id AND e.project_id = $1 AND e.name = $2 AND o.content = $3`, confExpr), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation %q not found on entity %q in project %q", oldContent, entityName, project)
	}
	return nil
}

func (r *postgresMemory) DeleteRelationByTriple(ctx context.Context, project, from, to, relationType string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM memory_relations r
		USING memory_entities fe, memory_entities te
		WHERE r.from_entity_id = fe.id AND r.to_entity_id = te.id
		  AND fe.project_id = $1 AND te.project_id = $1
		  AND fe.name = $2 AND te.name = $3 AND r.relation_type = $4`,
		project, from, to, relationType)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("relation %q --%s--> %q not found in project %q", from, relationType, to, project)
	}
	return nil
}
