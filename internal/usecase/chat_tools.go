// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"fmt"
	"strings"

	"mcp-memory-server/internal/gateway"
)

func entityTypeList() string {
	types := []string{"project", "person", "decision", "tool", "concept", "place"}
	return strings.Join(types, ", ")
}

var systemPromptTmpl = `You are an AI assistant managing a knowledge graph memory system. You can help users create, read, update, and delete memories (entities, observations, and relations).

## Entity Types
- %s

## Rules
- Relation types must be UPPER_SNAKE_CASE and active voice (e.g. DEPLOYED_VIA, USES, DEPENDS_ON)
- Entity names are case-sensitive and unique within a project
- All operations are scoped to the current project
- Observations are individual facts about an entity
- Confidence values are 0.0-1.0 (optional)
- Use memory_search before delete/update operations to find the correct entity and observation IDs

When you need to perform a memory operation, use the available tools. Explain what you're doing to the user. Keep responses concise.`

func systemPrompt() string {
	return fmt.Sprintf(systemPromptTmpl, entityTypeList())
}

func toolDefinitions() []gateway.ToolDefinition {
	t := func(name, desc string, params gateway.ToolParamSchema) gateway.ToolDefinition {
		return gateway.ToolDefinition{
			Type:     "function",
			Function: gateway.ToolFunction{Name: name, Description: desc, Parameters: params},
		}
	}
	return []gateway.ToolDefinition{
		t("memory_search", "Search entities by query text across observations, names, and relations", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"query": {Type: "string", Description: "Search query"},
				"limit": {Type: "integer", Description: "Max results (default 20)"},
			},
			Required: []string{"query"},
		}),
		t("memory_read_graph", "Read the entire knowledge graph for the current project", gateway.ToolParamSchema{
			Type:       "object",
			Properties: map[string]gateway.ToolParamProp{},
		}),
		t("memory_create_entities", "Create one or more entities with optional observations", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"entities": {
					Type:        "array",
					Description: `Array of entities. Each: {"name":"...", "entityType":"...", "observations":[...]}`,
				},
			},
			Required: []string{"entities"},
		}),
		t("memory_add_observations", "Add observations to an existing entity", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"entityName":   {Type: "string", Description: "Name of the entity"},
				"observations": {Type: "array", Description: "Facts to add"},
				"confidences":  {Type: "array", Description: "Optional confidence 0.0-1.0 per observation"},
			},
			Required: []string{"entityName", "observations"},
		}),
		t("memory_create_relations", "Create directed relations between entities", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"relations": {
					Type:        "array",
					Description: `Array: {"from":"...", "relationType":"DEPLOYS_VIA", "to":"..."}`,
				},
			},
			Required: []string{"relations"},
		}),
		t("memory_delete_entities", "Delete entities and all their observations and relations", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"names": {Type: "array", Description: "Entity names to delete"},
			},
			Required: []string{"names"},
		}),
		t("memory_rename_entity", "Rename an entity and/or change its type", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"oldName":    {Type: "string", Description: "Current entity name"},
				"newName":    {Type: "string", Description: "New entity name"},
				"entityType": {Type: "string", Description: "Optional new type: " + entityTypeList()},
			},
			Required: []string{"oldName", "newName"},
		}),
		t("memory_update_observation", "Update an observation's content by its database ID. Use memory_search first to find the observation ID.", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"id":            {Type: "integer", Description: "Observation database ID"},
				"content":       {Type: "string", Description: "New content text"},
				"newConfidence": {Type: "number", Description: "Optional new confidence 0.0-1.0"},
			},
			Required: []string{"id", "content"},
		}),
		t("memory_delete_observation", "Delete an observation by its database ID. Use memory_search first to find the observation ID.", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"entityName": {Type: "string", Description: "Entity name"},
				"id":         {Type: "integer", Description: "Observation database ID"},
			},
			Required: []string{"entityName", "id"},
		}),
		t("memory_delete_relation", "Delete a relation by its database ID or by (from, to, relationType) triple", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"from":         {Type: "string", Description: "Source entity name"},
				"to":           {Type: "string", Description: "Target entity name"},
				"relationType": {Type: "string", Description: "Relation type in UPPER_SNAKE_CASE"},
			},
		}),
		t("memory_get_history", "Get the change history/audit trail for an entity", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"entityName": {Type: "string", Description: "Entity name"},
				"limit":      {Type: "integer", Description: "Max entries"},
			},
			Required: []string{"entityName"},
		}),
		t("memory_export", "Export the entire project graph as structured JSON", gateway.ToolParamSchema{
			Type:       "object",
			Properties: map[string]gateway.ToolParamProp{},
		}),
		t("memory_import", "Import entities and relations from structured JSON", gateway.ToolParamSchema{
			Type: "object",
			Properties: map[string]gateway.ToolParamProp{
				"entities": {
					Type:        "array",
					Description: `Array: {"name":"...", "type":"...", "observations":[...]}`,
				},
				"relations": {
					Type:        "array",
					Description: `Array: {"from":"...", "relationType":"...", "to":"..."}`,
				},
			},
			Required: []string{"entities"},
		}),
	}
}
