// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package entity

// Checkpoint represents uncommitted context and current active progress.
type Checkpoint struct {
	Focus       string `json:"focus"`
	Uncommitted string `json:"uncommitted,omitempty"`
	Blockers    string `json:"blockers,omitempty"`
	NextStep    string `json:"next_step,omitempty"`
	SavedAt     string `json:"saved_at,omitempty"`
	SavedBy     string `json:"saved_by,omitempty"`
}

// SaveCheckpointInput is the domain input for saving a checkpoint.
type SaveCheckpointInput struct {
	Project     string  `json:"project"`
	Focus       string  `json:"focus"`
	Uncommitted string  `json:"uncommitted,omitempty"`
	Blockers    string  `json:"blockers,omitempty"`
	NextStep    string  `json:"next_step,omitempty"`
	Breadcrumb  string  `json:"breadcrumb,omitempty"`
	Source      *string `json:"source,omitempty"`
	SessionID   *string `json:"session_id,omitempty"`
	AgentLabel  *string `json:"agent_label,omitempty"`
}

// CheckpointResult is the confirmation response when a checkpoint is saved.
type CheckpointResult struct {
	Message string `json:"message"`
}

// SessionBootstrapInput is the domain input for bootstrapping a session.
type SessionBootstrapInput struct {
	Project    string  `json:"project"`
	Focus      *string `json:"focus,omitempty"`
	Source     *string `json:"source,omitempty"`
	SessionID  *string `json:"session_id,omitempty"`
	AgentLabel *string `json:"agent_label,omitempty"`
}

// SessionBootstrapResult is the full payload returned to an agent starting a session.
type SessionBootstrapResult struct {
	Warning         *string        `json:"warning,omitempty"`
	Checkpoint      *Checkpoint    `json:"checkpoint,omitempty"`
	BreadcrumbTrail []string       `json:"breadcrumb_trail"`
	OpenTasks       []Entity       `json:"open_tasks"`
	RecentDecisions []Entity       `json:"recent_decisions"`
	TopEntities     []SearchResult `json:"top_entities"`
}
