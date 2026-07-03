package main

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaFS embed.FS

// EnsureSchema runs schema.sql on startup so the container is self-sufficient.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	sql, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema.sql: %w", err)
	}
	_, err = pool.Exec(ctx, string(sql))
	return err
}

// defaultProject normalizes the project selector for writes: blank -> "default".
// (Read/export functions intentionally keep "" to mean "all projects".)
func defaultProject(p string) string {
	if p = strings.TrimSpace(p); p == "" {
		return "default"
	}
	return p
}

// normalizeEntityType lowercases + trims; empty becomes "concept".
// The DB FK (memory_entity_types) rejects anything not registered.
func normalizeEntityType(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		s = "concept"
	}
	return s
}

// normalizeRelationType forces UPPER_SNAKE_CASE: trim, uppercase, and turn any
// run of non-alphanumeric characters into a single "_".
func normalizeRelationType(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	prevUnder := false
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnder = false
			continue
		}
		if !prevUnder && b.Len() > 0 {
			b.WriteByte('_')
			prevUnder = true
		}
	}
	return strings.Trim(b.String(), "_")
}

type Entity struct {
	Name         string   `json:"name"`
	EntityType   string   `json:"type"`
	Observations []string `json:"observations"`
}

type EntityInput struct {
	Name         string   `json:"name" jsonschema:"Unique entity name within its project, e.g. 'MIS-APAR' or 'Faiq'"`
	EntityType   string   `json:"entityType,omitempty" jsonschema:"Registered type: project, person, decision, tool, concept, place"`
	Observations []string `json:"observations,omitempty" jsonschema:"Facts about this entity"`
}

type RelationInput struct {
	From         string `json:"from"`
	To           string `json:"to"`
	RelationType string `json:"relationType" jsonschema:"Active voice, UPPER_SNAKE_CASE, e.g. DEPLOYED_VIA"`
}

type SearchResult struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Observations []string `json:"observations"`
	Relations    []string `json:"relations"`
}

type FullGraph struct {
	Entities  []Entity `json:"entities"`
	Relations []string `json:"relations"`
}

// Structured graph shape used by export/import: relations carry their parts
// explicitly so import needs no fragile string parsing (lossless round-trip).
type ExportEntity struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Observations []string `json:"observations"`
}

type ExportRelation struct {
	From         string `json:"from"`
	RelationType string `json:"relationType"`
	To           string `json:"to"`
}

type ExportPayload struct {
	Project   string           `json:"project,omitempty"`
	Entities  []ExportEntity   `json:"entities"`
	Relations []ExportRelation `json:"relations"`
}

type ImportResult struct {
	EntitiesCreated  int `json:"entitiesCreated"`
	RelationsCreated int `json:"relationsCreated"`
}

func CreateEntities(ctx context.Context, pool *pgxpool.Pool, project string, entities []EntityInput) ([]string, error) {
	project = defaultProject(project)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var created []string
	for _, e := range entities {
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, e.Name).Scan(&id)
		if err != nil {
			et := normalizeEntityType(e.EntityType)
			if err := tx.QueryRow(ctx,
				`INSERT INTO memory_entities (project_id, name, entity_type) VALUES ($1, $2, $3) RETURNING id`,
				project, e.Name, et,
			).Scan(&id); err != nil {
				return nil, fmt.Errorf("create entity %q: %w", e.Name, err)
			}
		}
		for _, obs := range e.Observations {
			if _, err := tx.Exec(ctx,
				`INSERT INTO memory_observations (entity_id, content) VALUES ($1, $2)`, id, obs,
			); err != nil {
				return nil, fmt.Errorf("add observation to %q: %w", e.Name, err)
			}
		}
		created = append(created, e.Name)
	}
	return created, tx.Commit(ctx)
}

func AddObservations(ctx context.Context, pool *pgxpool.Pool, project, entityName string, observations []string) error {
	project = defaultProject(project)
	tx, err := pool.Begin(ctx)
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
	for _, obs := range observations {
		if _, err := tx.Exec(ctx,
			`INSERT INTO memory_observations (entity_id, content) VALUES ($1, $2)`, id, obs,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func CreateRelations(ctx context.Context, pool *pgxpool.Pool, project string, relations []RelationInput) ([]string, error) {
	project = defaultProject(project)
	tx, err := pool.Begin(ctx)
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
	for _, r := range relations {
		relType := normalizeRelationType(r.RelationType)
		if relType == "" {
			return nil, fmt.Errorf("relation has empty type after normalization: %q", r.RelationType)
		}
		fromID, err := getID(r.From)
		if err != nil {
			return nil, err
		}
		toID, err := getID(r.To)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO memory_relations (from_entity_id, to_entity_id, relation_type)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (from_entity_id, to_entity_id, relation_type) DO NOTHING`,
			fromID, toID, relType,
		)
		if err != nil {
			return nil, err
		}
		created = append(created, fmt.Sprintf("%s --%s--> %s", r.From, relType, r.To))
	}
	return created, tx.Commit(ctx)
}

func DeleteEntities(ctx context.Context, pool *pgxpool.Pool, project string, names []string) error {
	project = defaultProject(project)
	_, err := pool.Exec(ctx, `DELETE FROM memory_entities WHERE project_id = $1 AND name = ANY($2::text[])`, project, names)
	return err
}

func SearchMemory(ctx context.Context, pool *pgxpool.Pool, project, query string, limit int) ([]SearchResult, error) {
	project = defaultProject(project)
	if limit <= 0 {
		limit = 20
	}
	// Aggregate the entity's name + ALL its observations into a single tsvector
	// before matching, so a multi-word query matches when its terms are spread
	// across different observations. The previous per-row `@@` match ANDed the
	// query terms within a single observation/name, so multi-word queries almost
	// never matched. websearch_to_tsquery also tolerates punctuation and supports
	// "quoted phrases", OR, and -exclusion. Results ranked by relevance.
	rows, err := pool.Query(ctx, `
		SELECT id, name, entity_type
		FROM (
		  SELECT e.id, e.name, e.entity_type,
		         to_tsvector('simple', e.name || ' ' || coalesce(string_agg(o.content, ' '), '')) AS vec
		  FROM memory_entities e
		  LEFT JOIN memory_observations o ON o.entity_id = e.id
		  WHERE e.project_id = $1
		  GROUP BY e.id, e.name, e.entity_type
		) agg
		CROSS JOIN websearch_to_tsquery('simple', $2) AS q
		WHERE agg.vec @@ q
		ORDER BY ts_rank(agg.vec, q) DESC
		LIMIT $3`, project, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		id   int
		name string
		typ  string
	}
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.typ); err != nil {
			return nil, err
		}
		found = append(found, r)
	}

	var results []SearchResult
	for _, r := range found {
		obsRows, err := pool.Query(ctx,
			`SELECT content FROM memory_observations WHERE entity_id = $1 ORDER BY created_at`, r.id)
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

		var relations []string
		outRows, err := pool.Query(ctx, `
			SELECT r.relation_type, te.name FROM memory_relations r
			JOIN memory_entities te ON te.id = r.to_entity_id
			WHERE r.from_entity_id = $1`, r.id)
		if err != nil {
			return nil, err
		}
		for outRows.Next() {
			var relType, toName string
			if err := outRows.Scan(&relType, &toName); err != nil {
				outRows.Close()
				return nil, err
			}
			relations = append(relations, fmt.Sprintf("%s --%s--> %s", r.name, relType, toName))
		}
		outRows.Close()

		inRows, err := pool.Query(ctx, `
			SELECT r.relation_type, fe.name FROM memory_relations r
			JOIN memory_entities fe ON fe.id = r.from_entity_id
			WHERE r.to_entity_id = $1`, r.id)
		if err != nil {
			return nil, err
		}
		for inRows.Next() {
			var relType, fromName string
			if err := inRows.Scan(&relType, &fromName); err != nil {
				inRows.Close()
				return nil, err
			}
			relations = append(relations, fmt.Sprintf("%s --%s--> %s", fromName, relType, r.name))
		}
		inRows.Close()

		results = append(results, SearchResult{
			Name: r.name, Type: r.typ, Observations: observations, Relations: relations,
		})
	}
	return results, nil
}

// ReadFullGraph returns the graph for one project, or all projects when project == "".
// Relations are formatted as "A --R--> B" strings (unchanged output shape for agents).
func ReadFullGraph(ctx context.Context, pool *pgxpool.Pool, project string) (*FullGraph, error) {
	entQuery := `SELECT id, name, entity_type FROM memory_entities ORDER BY name`
	var entArgs []any
	if project != "" {
		entQuery = `SELECT id, name, entity_type FROM memory_entities WHERE project_id = $1 ORDER BY name`
		entArgs = []any{project}
	}
	entRows, err := pool.Query(ctx, entQuery, entArgs...)
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
	obsRows, err := pool.Query(ctx, obsQuery, obsArgs...)
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
	relRows, err := pool.Query(ctx, relQuery, relArgs...)
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

	graph := &FullGraph{Relations: relations}
	for _, e := range entities {
		graph.Entities = append(graph.Entities, Entity{
			Name: e.name, EntityType: e.typ, Observations: obsByEntity[e.id],
		})
	}
	return graph, nil
}

// ExportGraph returns a structured payload for one project (blank = all projects),
// suitable for backup/migration and lossless re-import.
func ExportGraph(ctx context.Context, pool *pgxpool.Pool, project string) (*ExportPayload, error) {
	scope := project != ""
	entQuery := `SELECT id, name, entity_type FROM memory_entities ORDER BY name`
	var entArgs []any
	if scope {
		entQuery = `SELECT id, name, entity_type FROM memory_entities WHERE project_id = $1 ORDER BY name`
		entArgs = []any{project}
	}
	entRows, err := pool.Query(ctx, entQuery, entArgs...)
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
	obsRows, err := pool.Query(ctx, obsQuery, obsArgs...)
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
	relRows, err := pool.Query(ctx, relQuery, relArgs...)
	if err != nil {
		return nil, err
	}
	defer relRows.Close()
	var relations []ExportRelation
	for relRows.Next() {
		var from, relType, to string
		if err := relRows.Scan(&from, &relType, &to); err != nil {
			return nil, err
		}
		relations = append(relations, ExportRelation{From: from, RelationType: relType, To: to})
	}

	payload := &ExportPayload{Project: project, Relations: relations}
	for _, e := range entities {
		payload.Entities = append(payload.Entities, ExportEntity{
			Name: e.name, Type: e.typ, Observations: obsByEntity[e.id],
		})
	}
	return payload, nil
}

// ImportGraph loads a structured payload into a project. Idempotent: existing
// entities are reused (observations appended), existing relations are skipped.
func ImportGraph(ctx context.Context, pool *pgxpool.Pool, project string, g *ExportPayload) (*ImportResult, error) {
	project = defaultProject(project)
	if g == nil {
		return &ImportResult{}, nil
	}
	tx, err := pool.Begin(ctx)
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

	res := &ImportResult{}
	for _, e := range g.Entities {
		et := normalizeEntityType(e.Type)
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, e.Name).Scan(&id)
		if err != nil {
			if err := tx.QueryRow(ctx,
				`INSERT INTO memory_entities (project_id, name, entity_type) VALUES ($1, $2, $3) RETURNING id`,
				project, e.Name, et,
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
	for _, r := range g.Relations {
		relType := normalizeRelationType(r.RelationType)
		if relType == "" {
			continue
		}
		fromID, err := getOrCreate(r.From)
		if err != nil {
			return nil, err
		}
		toID, err := getOrCreate(r.To)
		if err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO memory_relations (from_entity_id, to_entity_id, relation_type)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (from_entity_id, to_entity_id, relation_type) DO NOTHING`,
			fromID, toID, relType,
		)
		if err != nil {
			return nil, err
		}
		res.RelationsCreated += int(tag.RowsAffected())
	}
	return res, tx.Commit(ctx)
}
