// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-memory-server/internal/entity"
)

// SaveCheckpoint saves the current progress into <project>:checkpoint:active and appends to <project>:checkpoint:trail.
func (u *MemoryUseCase) SaveCheckpoint(ctx context.Context, in entity.SaveCheckpointInput) (*entity.CheckpointResult, error) {
	project := defaultProject(in.Project)

	focus := strings.TrimSpace(in.Focus)
	if focus == "" {
		return nil, fmt.Errorf("focus is required")
	}

	savedAt := time.Now().UTC().Format(time.RFC3339)

	var activeObs []string
	activeObs = append(activeObs, "focus: "+focus)
	if strings.TrimSpace(in.Uncommitted) != "" {
		activeObs = append(activeObs, "uncommitted: "+strings.TrimSpace(in.Uncommitted))
	}
	if strings.TrimSpace(in.Blockers) != "" {
		activeObs = append(activeObs, "blockers: "+strings.TrimSpace(in.Blockers))
	}
	if strings.TrimSpace(in.NextStep) != "" {
		activeObs = append(activeObs, "next_step: "+strings.TrimSpace(in.NextStep))
	}

	savedBy := "unknown"
	if in.AgentLabel != nil && strings.TrimSpace(*in.AgentLabel) != "" {
		savedBy = strings.TrimSpace(*in.AgentLabel)
	}
	activeObs = append(activeObs, "source: "+savedBy)
	activeObs = append(activeObs, "saved_at: "+savedAt)

	activeEntity := entity.EntityInput{
		Name:         project + ":checkpoint:active",
		Type:         "checkpoint",
		Observations: activeObs,
		Source:       in.Source,
		SessionID:    in.SessionID,
		AgentLabel:   in.AgentLabel,
	}

	// 1. Create or replace active checkpoint
	if _, err := u.repo.CreateEntities(ctx, project, []entity.EntityInput{activeEntity}, true); err != nil {
		return nil, fmt.Errorf("save active checkpoint: %w", err)
	}

	// 2. Format breadcrumb and append to trail
	breadcrumb := strings.TrimSpace(in.Breadcrumb)
	if breadcrumb == "" {
		runes := []rune(focus)
		if len(runes) > 80 {
			breadcrumb = string(runes[:80])
		} else {
			breadcrumb = string(runes)
		}
	}
	formattedTrail := fmt.Sprintf("[%s] [%s] %s", savedAt, savedBy, breadcrumb)

	attr := entity.Attribution{
		Source:     in.Source,
		SessionID:  in.SessionID,
		AgentLabel: in.AgentLabel,
	}
	trailEntityName := project + ":checkpoint:trail"
	// Ensure trail entity exists as checkpoint_trail type if new
	if _, err := u.repo.CreateEntities(ctx, project, []entity.EntityInput{
		{
			Name:       trailEntityName,
			Type:       "checkpoint_trail",
			Source:     in.Source,
			SessionID:  in.SessionID,
			AgentLabel: in.AgentLabel,
		},
	}); err != nil {
		return nil, fmt.Errorf("ensure trail entity: %w", err)
	}
	if err := u.repo.AddObservations(ctx, project, trailEntityName, []string{formattedTrail}, nil, nil, attr); err != nil {
		return nil, fmt.Errorf("append checkpoint trail: %w", err)
	}

	// 3. Log checkpoint_saved history event
	if err := u.repo.LogHistory(ctx, project, activeEntity.Name, "checkpoint_saved", attr); err != nil {
		return nil, fmt.Errorf("log checkpoint_saved history: %w", err)
	}

	u.publish(project, activeEntity.Name, "checkpoint_saved")

	return &entity.CheckpointResult{
		Message: "Checkpoint saved. Trail updated.",
	}, nil
}

// SessionBootstrap initializes a session by gathering the active checkpoint, breadcrumb trail, open tasks, recent decisions, and top entities.
func (u *MemoryUseCase) SessionBootstrap(ctx context.Context, in entity.SessionBootstrapInput) (*entity.SessionBootstrapResult, error) {
	project := defaultProject(in.Project)

	result := &entity.SessionBootstrapResult{
		BreadcrumbTrail: make([]string, 0),
		OpenTasks:       make([]entity.Entity, 0),
		RecentDecisions: make([]entity.Entity, 0),
		TopEntities:     make([]entity.SearchResult, 0),
	}

	// Soft warning if agent_label is missing/empty
	if in.AgentLabel == nil || strings.TrimSpace(*in.AgentLabel) == "" {
		warn := "⚠️ agent_label not set. Add to env: export MCP_AGENT_LABEL='...' export MCP_SOURCE='...' Checkpoint and timeline will be recorded as 'unknown' until configured."
		result.Warning = &warn
	}

	// Read graph for project
	graph, err := u.repo.ReadGraph(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("read graph: %w", err)
	}

	activeName := project + ":checkpoint:active"
	trailName := project + ":checkpoint:trail"

	for _, ent := range graph.Entities {
		switch ent.Name {
		case activeName:
			cp := &entity.Checkpoint{}
			for _, obs := range ent.Observations {
				if after, ok := strings.CutPrefix(obs, "focus: "); ok {
					cp.Focus = after
				} else if after, ok := strings.CutPrefix(obs, "uncommitted: "); ok {
					cp.Uncommitted = after
				} else if after, ok := strings.CutPrefix(obs, "blockers: "); ok {
					cp.Blockers = after
				} else if after, ok := strings.CutPrefix(obs, "next_step: "); ok {
					cp.NextStep = after
				} else if after, ok := strings.CutPrefix(obs, "source: "); ok {
					cp.SavedBy = after
				} else if after, ok := strings.CutPrefix(obs, "saved_at: "); ok {
					cp.SavedAt = after
				}
			}
			result.Checkpoint = cp

		case trailName:
			total := len(ent.Observations)
			count := min(total, 20)
			trail := make([]string, 0, count)
			for i := total - 1; i >= total-count; i-- {
				trail = append(trail, ent.Observations[i])
			}
			result.BreadcrumbTrail = trail
		}

		// Open tasks: entity_type='task' and observations do not contain 'status: done'
		if ent.EntityType == "task" {
			isDone := false
			for _, obs := range ent.Observations {
				if strings.Contains(strings.ToLower(obs), "status: done") {
					isDone = true
					break
				}
			}
			if !isDone {
				result.OpenTasks = append(result.OpenTasks, ent)
			}
		}
	}

	// Recent decisions: entities of type 'decision' updated in the last 14 days
	decisionsCutoff := time.Now().Add(-14 * 24 * time.Hour)
	if recentDecisions, err := u.repo.GetRecentEntitiesByType(ctx, project, "decision", decisionsCutoff); err == nil {
		result.RecentDecisions = recentDecisions
	}

	// Top entities: top 5 by recency / confidence (biased by focus query if provided)
	if in.Focus != nil && strings.TrimSpace(*in.Focus) != "" {
		topResults, err := u.Search(ctx, project, strings.TrimSpace(*in.Focus), 5)
		if err == nil && len(topResults) > 0 {
			result.TopEntities = topResults
		}
	}
	if len(result.TopEntities) == 0 {
		topResults, err := u.repo.GetTopEntities(ctx, project, 5)
		if err == nil {
			result.TopEntities = topResults
		}
	}

	// Log session_start history event
	attr := entity.Attribution{
		Source:     in.Source,
		SessionID:  in.SessionID,
		AgentLabel: in.AgentLabel,
	}
	if err := u.repo.LogHistory(ctx, project, "", "session_start", attr); err != nil {
		return nil, fmt.Errorf("log session_start history: %w", err)
	}

	u.publish(project, "", "session_start")

	return result, nil
}
