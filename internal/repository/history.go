// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package repository

import (
	"context"
	"fmt"
	"time"

	"mcp-memory-server/internal/entity"
)

// TouchAccessed bumps last_accessed_at for the given entity IDs. Best-effort:
// the caller (usecase) should ignore errors so a search never fails due to this.
func (r *postgresMemory) TouchAccessed(ctx context.Context, entityIDs []int) error {
	if len(entityIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `UPDATE memory_entities SET last_accessed_at = now() WHERE id = ANY($1::int[])`, entityIDs)
	return err
}

// GetHistory returns audit-trail entries for one entity, strictly scoped by project_id and entity_name.
func (r *postgresMemory) GetHistory(ctx context.Context, project, entityName string, limit int) ([]entity.HistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT h.id, h.project_id, h.entity_name, h.action, h.old_value, h.new_value, h.confidence,
		       h.source, h.session_id, h.agent_label,
		       to_char(h.happened_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM memory_history h
		WHERE h.project_id = $1 AND h.entity_name = $2
		ORDER BY h.happened_at DESC, h.id DESC
		LIMIT $3`, project, entityName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.HistoryEntry
	for rows.Next() {
		var h entity.HistoryEntry
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.EntityName, &h.Action, &h.OldValue, &h.NewValue, &h.Confidence,
			&h.Source, &h.SessionID, &h.AgentLabel, &h.HappenedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// GetTimeline queries memory_history for a project ordered by happened_at DESC, h.id DESC.
func (r *postgresMemory) GetTimeline(ctx context.Context, project string, since time.Time, limit int, entityName *string, source *string) ([]entity.HistoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	query := `
		SELECT h.id, h.project_id, h.entity_name, h.action, h.old_value, h.new_value, h.confidence,
		       h.source, h.session_id, h.agent_label,
		       to_char(h.happened_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM memory_history h
		WHERE h.project_id = $1`
	args := []any{project}

	if !since.IsZero() {
		query += fmt.Sprintf(" AND h.happened_at >= $%d", len(args)+1)
		args = append(args, since)
	}
	if entityName != nil && *entityName != "" {
		query += fmt.Sprintf(" AND h.entity_name = $%d", len(args)+1)
		args = append(args, *entityName)
	}
	if source != nil && *source != "" {
		query += fmt.Sprintf(" AND h.source = $%d", len(args)+1)
		args = append(args, *source)
	}

	query += fmt.Sprintf(" ORDER BY h.happened_at DESC, h.id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []entity.HistoryEntry
	for rows.Next() {
		var h entity.HistoryEntry
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.EntityName, &h.Action, &h.OldValue, &h.NewValue, &h.Confidence,
			&h.Source, &h.SessionID, &h.AgentLabel, &h.HappenedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// LogHistory writes an explicit history event (such as checkpoint_saved or session_start).
func (r *postgresMemory) LogHistory(ctx context.Context, project, entityName, action string, attr entity.Attribution) error {
	var entityID *int
	if entityName != "" {
		var id int
		err := r.pool.QueryRow(ctx, `SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, entityName).Scan(&id)
		if err == nil {
			entityID = &id
		}
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO memory_history (project_id, entity_id, entity_name, action, source, session_id, agent_label)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		project, entityID, entityName, action, attr.Source, attr.SessionID, attr.AgentLabel,
	)
	return err
}
