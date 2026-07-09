// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package mcp

import (
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

// BuildServer creates the MCP server and registers all memory tools.
func BuildServer(h *Handlers) *mcpgo.Server {
	server := mcpgo.NewServer(&mcpgo.Implementation{Name: "mcp-memory-server", Version: "1.0.0"}, nil)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_create_entities",
		Description: "Create one or more entities in a project's knowledge graph, optionally with initial observations. Reuses an entity if (project, name) already exists.",
	}, h.handleCreateEntities)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_add_observations",
		Description: "Add new observations (facts) to an existing entity in a project. Creates the entity if it doesn't exist yet.",
	}, h.handleAddObservations)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_create_relations",
		Description: "Create directed relations between entities in a project, e.g. 'MIS-APAR --DEPLOYED_VIA--> Coolify'. Relation type should be active voice, UPPER_SNAKE_CASE.",
	}, h.handleCreateRelations)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_delete_entities",
		Description: "Delete entities (and their observations/relations) by name within a project.",
	}, h.handleDeleteEntities)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_search",
		Description: "Search a project's knowledge graph for entities matching a query (matches entity names and observation content). Prefer this over memory_read_graph.",
	}, h.handleSearch)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_read_graph",
		Description: "Read a project's entire knowledge graph (blank project = all projects). Expensive — prefer memory_search for targeted lookups. Use only for discovery/debugging.",
	}, h.handleReadGraph)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_export",
		Description: "Export a project's knowledge graph (entities + relations) as structured JSON for backup or migration. Blank project = all projects.",
	}, h.handleExport)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_import",
		Description: "Import entities + relations from structured JSON into a project. Idempotent (skips existing entities/relations). Useful for restoring a backup or migrating data.",
	}, h.handleImport)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_rename_entity",
		Description: "Rename and/or change the type of an entity. Use this to fix entity name typos (e.g. Sabgya -> Sabagya). Rejects a rename if the new name already exists in the project.",
	}, h.handleRenameEntity)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_update_observation",
		Description: "Edit an observation's text by matching its exact current content within an entity. Provide the verbatim old text (as returned by memory_search/read_graph) and the new text.",
	}, h.handleUpdateObservation)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_delete_observation",
		Description: "Delete one observation by matching its exact content within an entity. Prefer this over deleting the whole entity when only one fact is wrong.",
	}, h.handleDeleteObservation)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_delete_relation",
		Description: "Delete a relation by its from/to/relationType triple (relationType normalized to UPPER_SNAKE_CASE).",
	}, h.handleDeleteRelation)

	mcpgo.AddTool(server, &mcpgo.Tool{
		Name:        "memory_get_history",
		Description: "Retrieve the change history (audit trail) for one entity — observation edits, deletes, and entity renames/type changes.",
	}, h.handleGetHistory)

	return server
}
