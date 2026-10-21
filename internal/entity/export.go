// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package entity

// SearchResult is one entity matched by a search, with its observations and
// relations formatted as "A --R--> B" strings.
// Project is the namespace the entity lives in — always populated, so results
// from a cross-namespace (blank project) search remain attributable.
// Confidence is the entity-level average of observation confidences (nil if all neutral).
// Score is the combined re-rank score (text relevance × confidence × recency) for debugging.
type SearchResult struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Project      string   `json:"project"`
	Observations []string `json:"observations"`
	Relations    []string `json:"relations"`
	Confidence   *float64 `json:"confidence,omitempty"`
	Score        *float64 `json:"score,omitempty"`
}

// FullGraph is the whole graph for a project (or all projects), with relations
// as "A --R--> B" strings.
type FullGraph struct {
	Entities  []Entity `json:"entities"`
	Relations []string `json:"relations"`
}

// ExportEntity / ExportRelation / ExportPayload form the structured (lossless)
// export/import shape: relations carry their parts explicitly so import needs
// no fragile string parsing.
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

// ImportResult counts what an import created.
type ImportResult struct {
	EntitiesCreated  int `json:"entitiesCreated"`
	RelationsCreated int `json:"relationsCreated"`
}
