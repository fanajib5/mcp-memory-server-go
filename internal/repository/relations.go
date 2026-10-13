// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package repository

import (
	"context"
	"fmt"
)

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
