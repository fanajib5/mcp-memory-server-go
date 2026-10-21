// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package entity

// EntityInput is the normalized domain input for creating/updating an entity.
// jsonschema tags feed the MCP tool input schema (harmless outside MCP).
// Confidences is a parallel slice to Observations: confidences[i] applies to
// Observations[i]. Empty/nil = all observations get NULL confidence (netral).
type EntityInput struct {
	Name         string      `json:"name" jsonschema:"Unique entity name within its project, e.g. 'MIS-APAR' or 'Faiq'"`
	Type         string      `json:"entityType,omitempty" jsonschema:"Registered type: project, person, decision, tool, concept, place"`
	Observations []string    `json:"observations,omitempty" jsonschema:"Facts about this entity"`
	Confidences  []float64   `json:"confidences,omitempty" jsonschema:"Optional AI confidence 0.0-1.0 per observation, parallel to observations. Omit or empty = neutral."`
	Tags         []string    `json:"tags,omitempty" jsonschema:"Optional tags for semantic categorisation or filtering"`
	Source       *string     `json:"source,omitempty" jsonschema:"Optional source identifier (e.g. 'claude-code', 'antigravity')"`
	SessionID    *string     `json:"session_id,omitempty" jsonschema:"Optional session ID"`
	AgentLabel   *string     `json:"agent_label,omitempty" jsonschema:"Optional human-readable agent label, e.g. 'Claude Code @ kantor'"`
	Embeddings   [][]float32 `json:"-"` // internal: populated by usecase embedder, never from MCP input
}

// Attribution carries metadata about who or what performed a memory operation.
type Attribution struct {
	Source     *string `json:"source,omitempty"`
	SessionID  *string `json:"session_id,omitempty"`
	AgentLabel *string `json:"agent_label,omitempty"`
}

// RelationInput is the normalized domain input for creating a directed relation.
type RelationInput struct {
	From         string `json:"from"`
	To           string `json:"to"`
	RelationType string `json:"relationType" jsonschema:"Active voice, UPPER_SNAKE_CASE, e.g. DEPLOYED_VIA"`
}

// Entity is a knowledge-graph entity (name + type + its observations).
type Entity struct {
	Name         string   `json:"name"`
	EntityType   string   `json:"type"`
	Observations []string `json:"observations"`
	Tags         []string `json:"tags,omitempty"`
}

// Observation represents a stored observation fact with metadata attribution.
type Observation struct {
	ID         int      `json:"id"`
	EntityID   int      `json:"entityId"`
	Content    string   `json:"content"`
	Confidence *float64 `json:"confidence,omitempty"`
	Source     *string  `json:"source,omitempty"`
	SessionID  *string  `json:"sessionId,omitempty"`
	AgentLabel *string  `json:"agentLabel,omitempty"`
	CreatedAt  string   `json:"createdAt"`
	DeletedAt  *string  `json:"deletedAt,omitempty"`
}

// ---- Versioning ----

// HistoryEntry is one audit-trail row for an entity/observation mutation.
type HistoryEntry struct {
	ID         int64    `json:"id"`
	ProjectID  string   `json:"projectId"`
	EntityName string   `json:"entityName"`
	Action     string   `json:"action"`
	OldValue   *string  `json:"oldValue,omitempty"`
	NewValue   *string  `json:"newValue,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	Source     *string  `json:"source,omitempty"`
	SessionID  *string  `json:"sessionId,omitempty"`
	AgentLabel *string  `json:"agentLabel,omitempty"`
	HappenedAt string   `json:"happenedAt"`
}
