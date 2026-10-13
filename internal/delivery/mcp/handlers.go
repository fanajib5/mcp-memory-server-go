// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/usecase"
)

// Handlers binds MCP tool calls to the memory usecase.
type Handlers struct {
	uc *usecase.MemoryUseCase
}

// NewHandlers builds MCP tool handlers over a memory usecase.
func NewHandlers(uc *usecase.MemoryUseCase) *Handlers {
	return &Handlers{uc: uc}
}

func textResult(s string) *mcpgo.CallToolResult {
	return &mcpgo.CallToolResult{Content: []mcpgo.Content{&mcpgo.TextContent{Text: s}}}
}

func jsonResult(v any) *mcpgo.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult("error encoding result: " + err.Error())
	}
	return textResult(string(b))
}

func (h *Handlers) handleCreateEntities(ctx context.Context, req *mcpgo.CallToolRequest, in CreateEntitiesInput) (*mcpgo.CallToolResult, any, error) {
	for i := range in.Entities {
		if in.Entities[i].Source == nil && in.Source != nil {
			in.Entities[i].Source = in.Source
		}
		if in.Entities[i].SessionID == nil && in.SessionID != nil {
			in.Entities[i].SessionID = in.SessionID
		}
		if in.Entities[i].AgentLabel == nil && in.AgentLabel != nil {
			in.Entities[i].AgentLabel = in.AgentLabel
		}
	}
	created, err := h.uc.CreateEntities(ctx, in.Project, in.Entities)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	var names strings.Builder
	for i, n := range created {
		if i > 0 {
			names.WriteString(", ")
		}
		names.WriteString(n)
	}
	return textResult("Created/updated entities: " + names.String()), nil, nil
}

func (h *Handlers) handleAddObservations(ctx context.Context, req *mcpgo.CallToolRequest, in AddObservationsInput) (*mcpgo.CallToolResult, any, error) {
	attr := entity.Attribution{
		Source:     in.Source,
		SessionID:  in.SessionID,
		AgentLabel: in.AgentLabel,
	}
	if err := h.uc.AddObservations(ctx, in.Project, in.EntityName, in.Observations, in.Confidences, attr); err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return textResult("Observations added to " + in.EntityName), nil, nil
}

func (h *Handlers) handleCreateRelations(ctx context.Context, req *mcpgo.CallToolRequest, in CreateRelationsInput) (*mcpgo.CallToolResult, any, error) {
	created, err := h.uc.CreateRelations(ctx, in.Project, in.Relations)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	out := ""
	for i, r := range created {
		if i > 0 {
			out += "\n"
		}
		out += r
	}
	if out == "" {
		out = "No new relations created."
	}
	return textResult(out), nil, nil
}

func (h *Handlers) handleDeleteEntities(ctx context.Context, req *mcpgo.CallToolRequest, in DeleteEntitiesInput) (*mcpgo.CallToolResult, any, error) {
	if err := h.uc.DeleteEntities(ctx, in.Project, in.Names); err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return textResult("Deleted entities."), nil, nil
}

func (h *Handlers) handleSearch(ctx context.Context, req *mcpgo.CallToolRequest, in SearchInput) (*mcpgo.CallToolResult, any, error) {
	results, err := h.uc.Search(ctx, in.Project, in.Query, in.Limit, in.EntityTypes, in.TagsAny)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(results), nil, nil
}

func (h *Handlers) handleReadGraph(ctx context.Context, req *mcpgo.CallToolRequest, in ReadGraphInput) (*mcpgo.CallToolResult, any, error) {
	graph, err := h.uc.ReadGraph(ctx, in.Project)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(graph), nil, nil
}

func (h *Handlers) handleExport(ctx context.Context, req *mcpgo.CallToolRequest, in ExportInput) (*mcpgo.CallToolResult, any, error) {
	payload, err := h.uc.Export(ctx, in.Project)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(payload), nil, nil
}

func (h *Handlers) handleImport(ctx context.Context, req *mcpgo.CallToolRequest, in ImportInput) (*mcpgo.CallToolResult, any, error) {
	g := &entity.ExportPayload{Entities: in.Entities, Relations: in.Relations}
	res, err := h.uc.Import(ctx, in.Project, g)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(res), nil, nil
}

func (h *Handlers) handleRenameEntity(ctx context.Context, req *mcpgo.CallToolRequest, in RenameEntityInput) (*mcpgo.CallToolResult, any, error) {
	if err := h.uc.UpdateEntity(ctx, in.Project, in.OldName, in.NewName, in.EntityType); err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return textResult("Renamed/updated entity: " + in.OldName + " -> " + in.NewName), nil, nil
}

func (h *Handlers) handleUpdateObservation(ctx context.Context, req *mcpgo.CallToolRequest, in UpdateObservationInput) (*mcpgo.CallToolResult, any, error) {
	if err := h.uc.UpdateObservationByContent(ctx, in.Project, in.EntityName, in.OldContent, in.NewContent, in.NewConfidence); err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return textResult("Updated observation on " + in.EntityName), nil, nil
}

func (h *Handlers) handleDeleteObservation(ctx context.Context, req *mcpgo.CallToolRequest, in DeleteObservationInput) (*mcpgo.CallToolResult, any, error) {
	if err := h.uc.DeleteObservationByContent(ctx, in.Project, in.EntityName, in.Content); err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return textResult("Deleted observation from " + in.EntityName), nil, nil
}

func (h *Handlers) handleDeleteRelation(ctx context.Context, req *mcpgo.CallToolRequest, in DeleteRelationInput) (*mcpgo.CallToolResult, any, error) {
	if err := h.uc.DeleteRelationByTriple(ctx, in.Project, in.From, in.To, in.RelationType); err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return textResult("Deleted relation " + in.From + " --" + in.RelationType + "--> " + in.To), nil, nil
}

func (h *Handlers) handleGetHistory(ctx context.Context, req *mcpgo.CallToolRequest, in GetHistoryInput) (*mcpgo.CallToolResult, any, error) {
	entries, err := h.uc.GetHistory(ctx, in.Project, in.EntityName, in.Limit)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(entries), nil, nil
}

func (h *Handlers) handleSaveCheckpoint(ctx context.Context, req *mcpgo.CallToolRequest, in SaveCheckpointInput) (*mcpgo.CallToolResult, any, error) {
	res, err := h.uc.SaveCheckpoint(ctx, entity.SaveCheckpointInput{
		Project:     in.Project,
		Focus:       in.Focus,
		Uncommitted: in.Uncommitted,
		Blockers:    in.Blockers,
		NextStep:    in.NextStep,
		Breadcrumb:  in.Breadcrumb,
		Source:      in.Source,
		SessionID:   in.SessionID,
		AgentLabel:  in.AgentLabel,
	})
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(res), nil, nil
}

func (h *Handlers) handleSessionBootstrap(ctx context.Context, req *mcpgo.CallToolRequest, in SessionBootstrapInput) (*mcpgo.CallToolResult, any, error) {
	res, err := h.uc.SessionBootstrap(ctx, entity.SessionBootstrapInput{
		Project:    in.Project,
		Focus:      in.Focus,
		Source:     in.Source,
		SessionID:  in.SessionID,
		AgentLabel: in.AgentLabel,
	})
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(res), nil, nil
}

func (h *Handlers) handleTimeline(ctx context.Context, req *mcpgo.CallToolRequest, in TimelineInput) (*mcpgo.CallToolResult, any, error) {
	var since time.Time
	if strings.TrimSpace(in.Since) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(in.Since))
		if err != nil {
			// Also support date format if user passed date without time
			if parsedDate, errDate := time.Parse("2006-01-02", strings.TrimSpace(in.Since)); errDate == nil {
				since = parsedDate
			} else {
				return textResult("error parsing since timestamp (expected RFC3339, e.g. 2026-10-13T00:00:00Z): " + err.Error()), nil, nil
			}
		} else {
			since = parsed
		}
	}
	entries, err := h.uc.GetTimeline(ctx, in.Project, since, in.Limit, in.EntityName, in.Source)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(entries), nil, nil
}
