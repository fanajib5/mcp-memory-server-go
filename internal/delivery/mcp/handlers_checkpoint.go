// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"strings"
	"time"

	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp-memory-server/internal/entity"
)

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
