// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package repository

import (
	"context"
	"fmt"
)

func (r *postgresMemory) DeleteObservation(ctx context.Context, project string, id int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Read old content + entity name before delete (for history).
	var entityID int
	var entityName, oldContent string
	err = tx.QueryRow(ctx, `
		SELECT o.entity_id, e.name, o.content FROM memory_observations o
		JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND o.id = $2`, project, id,
	).Scan(&entityID, &entityName, &oldContent)
	if err != nil {
		return fmt.Errorf("observation not found in project %q: %w", project, err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM memory_observations WHERE id = $1`, id); err != nil {
		return err
	}

	tx.Exec(ctx, `INSERT INTO memory_history (project_id, entity_id, entity_name, observation_id, action, old_value) VALUES ($1, $2, $3, $4, 'observation_deleted', $5)`,
		project, entityID, entityName, id, oldContent)

	return tx.Commit(ctx)
}

func (r *postgresMemory) UpdateObservation(ctx context.Context, project string, id int, content string, newConfidence *float64, newEmbedding []float32) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Read old content + entity for history.
	var entityID int
	var entityName, oldContent string
	err = tx.QueryRow(ctx, `
		SELECT o.entity_id, e.name, o.content FROM memory_observations o
		JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND o.id = $2`, project, id,
	).Scan(&entityID, &entityName, &oldContent)
	if err != nil {
		return fmt.Errorf("observation not found in project %q: %w", project, err)
	}

	confExpr := "confidence"
	embExpr := "embedding"
	setClauses := "content = $3"
	var args []any
	args = append(args, id, project, content)
	if newConfidence != nil {
		confExpr = fmt.Sprintf("$%d", len(args)+1)
		args = append(args, *newConfidence)
		setClauses += ", confidence = " + confExpr
	}
	if newEmbedding != nil {
		embExpr = fmt.Sprintf("$%d::vector", len(args)+1)
		args = append(args, vecLiteral(newEmbedding))
		setClauses += ", embedding = " + embExpr
	}

	tag, err := tx.Exec(ctx, fmt.Sprintf(`
		UPDATE memory_observations SET %s
		FROM memory_entities e
		WHERE memory_observations.entity_id = e.id AND e.project_id = $2 AND memory_observations.id = $1`, setClauses), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation not found in project %q", project)
	}

	// History: capture content change.
	if oldContent != content {
		tx.Exec(ctx, `INSERT INTO memory_history (project_id, entity_id, entity_name, observation_id, action, old_value, new_value) VALUES ($1, $2, $3, $4, 'observation_updated', $5, $6)`,
			project, entityID, entityName, id, oldContent, content)
	}

	return tx.Commit(ctx)
}

func (r *postgresMemory) DeleteObservationByContent(ctx context.Context, project, entityName, content string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var entityID, obsID int
	err = tx.QueryRow(ctx, `
		SELECT o.entity_id, o.id FROM memory_observations o
		JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND e.name = $2 AND o.content = $3`,
		project, entityName, content).Scan(&entityID, &obsID)
	if err != nil {
		return fmt.Errorf("observation %q not found on entity %q in project %q", content, entityName, project)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM memory_observations WHERE id = $1`, obsID); err != nil {
		return err
	}

	tx.Exec(ctx, `INSERT INTO memory_history (project_id, entity_id, entity_name, observation_id, action, old_value) VALUES ($1, $2, $3, $4, 'observation_deleted', $5)`,
		project, entityID, entityName, obsID, content)

	return tx.Commit(ctx)
}

func (r *postgresMemory) UpdateObservationByContent(ctx context.Context, project, entityName, oldContent, newContent string, newConfidence *float64, newEmbedding []float32) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var entityID, obsID int
	err = tx.QueryRow(ctx, `
		SELECT o.entity_id, o.id FROM memory_observations o
		JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND e.name = $2 AND o.content = $3`,
		project, entityName, oldContent).Scan(&entityID, &obsID)
	if err != nil {
		return fmt.Errorf("observation %q not found on entity %q in project %q", oldContent, entityName, project)
	}

	confExpr := "confidence"
	embExpr := "embedding"
	setClauses := "content = $3"
	var args []any
	args = append(args, obsID, project, newContent)
	if newConfidence != nil {
		confExpr = fmt.Sprintf("$%d", len(args)+1)
		args = append(args, *newConfidence)
		setClauses += ", confidence = " + confExpr
	}
	if newEmbedding != nil {
		embExpr = fmt.Sprintf("$%d::vector", len(args)+1)
		args = append(args, vecLiteral(newEmbedding))
		setClauses += ", embedding = " + embExpr
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		UPDATE memory_observations SET %s
		FROM memory_entities e
		WHERE memory_observations.entity_id = e.id AND e.project_id = $2 AND memory_observations.id = $1`, setClauses), args...); err != nil {
		return err
	}

	if oldContent != newContent {
		tx.Exec(ctx, `INSERT INTO memory_history (project_id, entity_id, entity_name, observation_id, action, old_value, new_value) VALUES ($1, $2, $3, $4, 'observation_updated', $5, $6)`,
			project, entityID, entityName, obsID, oldContent, newContent)
	}

	return tx.Commit(ctx)
}
