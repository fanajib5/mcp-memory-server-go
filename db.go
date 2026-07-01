package main

import (
	"context"
	"embed"
	"fmt"

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

type Entity struct {
	Name         string   `json:"name"`
	EntityType   string   `json:"type"`
	Observations []string `json:"observations"`
}

type EntityInput struct {
	Name         string   `json:"name" jsonschema:"Unique entity name, e.g. 'MIS-APAR' or 'Faiq'"`
	EntityType   string   `json:"entityType,omitempty" jsonschema:"e.g. project, person, decision, tool"`
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

func CreateEntities(ctx context.Context, pool *pgxpool.Pool, entities []EntityInput) ([]string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var created []string
	for _, e := range entities {
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE name = $1`, e.Name).Scan(&id)
		if err != nil {
			et := e.EntityType
			if et == "" {
				et = "concept"
			}
			if err := tx.QueryRow(ctx,
				`INSERT INTO memory_entities (name, entity_type) VALUES ($1, $2) RETURNING id`,
				e.Name, et,
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

func AddObservations(ctx context.Context, pool *pgxpool.Pool, entityName string, observations []string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var id int
	err = tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE name = $1`, entityName).Scan(&id)
	if err != nil {
		if err := tx.QueryRow(ctx,
			`INSERT INTO memory_entities (name, entity_type) VALUES ($1, 'concept') RETURNING id`,
			entityName,
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

func CreateRelations(ctx context.Context, pool *pgxpool.Pool, relations []RelationInput) ([]string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	getID := func(name string) (int, error) {
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM memory_entities WHERE name = $1`, name).Scan(&id)
		if err == nil {
			return id, nil
		}
		err = tx.QueryRow(ctx,
			`INSERT INTO memory_entities (name, entity_type) VALUES ($1, 'concept') RETURNING id`,
			name,
		).Scan(&id)
		return id, err
	}

	var created []string
	for _, r := range relations {
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
			fromID, toID, r.RelationType,
		)
		if err != nil {
			return nil, err
		}
		created = append(created, fmt.Sprintf("%s --%s--> %s", r.From, r.RelationType, r.To))
	}
	return created, tx.Commit(ctx)
}

func DeleteEntities(ctx context.Context, pool *pgxpool.Pool, names []string) error {
	_, err := pool.Exec(ctx, `DELETE FROM memory_entities WHERE name = ANY($1::text[])`, names)
	return err
}

func SearchMemory(ctx context.Context, pool *pgxpool.Pool, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT e.id, e.name, e.entity_type
		FROM memory_entities e
		LEFT JOIN memory_observations o ON o.entity_id = e.id
		WHERE to_tsvector('simple', e.name) @@ plainto_tsquery('simple', $1)
		   OR to_tsvector('simple', coalesce(o.content, '')) @@ plainto_tsquery('simple', $1)
		LIMIT $2`, query, limit)
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

func ReadFullGraph(ctx context.Context, pool *pgxpool.Pool) (*FullGraph, error) {
	entRows, err := pool.Query(ctx, `SELECT id, name, entity_type FROM memory_entities ORDER BY name`)
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

	obsRows, err := pool.Query(ctx, `SELECT entity_id, content FROM memory_observations`)
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

	relRows, err := pool.Query(ctx, `
		SELECT fe.name, r.relation_type, te.name
		FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		JOIN memory_entities te ON te.id = r.to_entity_id`)
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
