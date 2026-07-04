package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UpdateEntity renames and/or changes the type of an entity within a project.
// Rejects a rename if the new name already exists in the same project.
func UpdateEntity(ctx context.Context, pool *pgxpool.Pool, project, oldName, newName, entityType string) error {
	project = defaultProject(project)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("entity name is required")
	}
	et := normalizeEntityType(entityType)

	tx, err := pool.Begin(ctx)
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
		// err != nil here means no collision row found — expected, proceed.
	}

	if _, err := tx.Exec(ctx,
		`UPDATE memory_entities SET name = $2, entity_type = $3 WHERE id = $1`, id, newName, et,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteObservation deletes one observation by id, scoped to project.
func DeleteObservation(ctx context.Context, pool *pgxpool.Pool, project string, id int) error {
	project = defaultProject(project)
	tag, err := pool.Exec(ctx, `
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

// UpdateObservation replaces an observation's content, scoped to project.
func UpdateObservation(ctx context.Context, pool *pgxpool.Pool, project string, id int, content string) error {
	project = defaultProject(project)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("observation content is required")
	}
	tag, err := pool.Exec(ctx, `
		UPDATE memory_observations o SET content = $3
		FROM memory_entities e
		WHERE o.entity_id = e.id AND e.project_id = $1 AND o.id = $2`, project, id, content)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation not found in project %q", project)
	}
	return nil
}

// DeleteRelation deletes one relation by id, scoped to the from-entity's project.
func DeleteRelation(ctx context.Context, pool *pgxpool.Pool, project string, id int) error {
	project = defaultProject(project)
	tag, err := pool.Exec(ctx, `
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

// DeleteObservationByContent deletes observation(s) matching exact content within
// an entity. Agent-friendly: no observation IDs are surfaced by read/search tools.
func DeleteObservationByContent(ctx context.Context, pool *pgxpool.Pool, project, entityName, content string) error {
	project = defaultProject(project)
	tag, err := pool.Exec(ctx, `
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

// UpdateObservationByContent replaces content of matching observation(s) within an entity.
func UpdateObservationByContent(ctx context.Context, pool *pgxpool.Pool, project, entityName, oldContent, newContent string) error {
	project = defaultProject(project)
	if strings.TrimSpace(newContent) == "" {
		return fmt.Errorf("new content is required")
	}
	tag, err := pool.Exec(ctx, `
		UPDATE memory_observations o SET content = $4
		FROM memory_entities e
		WHERE o.entity_id = e.id AND e.project_id = $1 AND e.name = $2 AND o.content = $3`,
		project, entityName, oldContent, newContent)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation %q not found on entity %q in project %q", oldContent, entityName, project)
	}
	return nil
}

// DeleteRelationByTriple deletes the relation matching (from, to, relationType) in project.
// relationType is normalized to UPPER_SNAKE_CASE before matching.
func DeleteRelationByTriple(ctx context.Context, pool *pgxpool.Pool, project, from, to, relationType string) error {
	project = defaultProject(project)
	relType := normalizeRelationType(relationType)
	if relType == "" {
		return fmt.Errorf("relation type is required")
	}
	tag, err := pool.Exec(ctx, `
		DELETE FROM memory_relations r
		USING memory_entities fe, memory_entities te
		WHERE r.from_entity_id = fe.id AND r.to_entity_id = te.id
		  AND fe.project_id = $1 AND te.project_id = $1
		  AND fe.name = $2 AND te.name = $3 AND r.relation_type = $4`,
		project, from, to, relType)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("relation %q --%s--> %q not found in project %q", from, relType, to, project)
	}
	return nil
}
