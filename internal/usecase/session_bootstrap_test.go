package usecase

import (
	"context"
	"fmt"
	"testing"
	"time"

	"mcp-memory-server/internal/entity"
)

func TestSessionBootstrapWarningWhenAgentLabelEmpty(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	project := "bootwarnproj"

	// 1. Nil agent label
	res1, err := h.mem.SessionBootstrap(ctx, entity.SessionBootstrapInput{
		Project: project,
	})
	if err != nil {
		t.Fatalf("SessionBootstrap failed: %v", err)
	}
	expectedWarning := "⚠️ agent_label not set. Add to env: export MCP_AGENT_LABEL='...' export MCP_SOURCE='...' Checkpoint and timeline will be recorded as 'unknown' until configured."
	if res1.Warning == nil || *res1.Warning != expectedWarning {
		t.Errorf("expected warning %q, got %+v", expectedWarning, res1.Warning)
	}

	// 2. Empty string agent label
	emptyLabel := ""
	res2, err := h.mem.SessionBootstrap(ctx, entity.SessionBootstrapInput{
		Project:    project,
		AgentLabel: &emptyLabel,
	})
	if err != nil {
		t.Fatalf("SessionBootstrap 2 failed: %v", err)
	}
	if res2.Warning == nil || *res2.Warning != expectedWarning {
		t.Errorf("expected warning %q, got %+v", expectedWarning, res2.Warning)
	}

	// 3. Set agent label -> warning should be nil
	setLabel := "Claude Code @ kantor"
	res3, err := h.mem.SessionBootstrap(ctx, entity.SessionBootstrapInput{
		Project:    project,
		AgentLabel: &setLabel,
	})
	if err != nil {
		t.Fatalf("SessionBootstrap 3 failed: %v", err)
	}
	if res3.Warning != nil {
		t.Errorf("expected nil warning, got %q", *res3.Warning)
	}
}

func TestSessionBootstrapFullPayload(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	project := "bootfullproj"
	agentLabel := "Claude Code @ kantor"
	source := "claude-code"
	sessionID := "sess-boot"

	// Setup:
	// 1. Save checkpoint (which creates active checkpoint and trail)
	_, err := h.mem.SaveCheckpoint(ctx, entity.SaveCheckpointInput{
		Project:     project,
		Focus:       "Bootstrap feature work",
		Uncommitted: "bootstrap_test.go uncommitted",
		Blockers:    "need db",
		NextStep:    "finish implementation",
		Breadcrumb:  "Trail 1",
		Source:      &source,
		SessionID:   &sessionID,
		AgentLabel:  &agentLabel,
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint 1: %v", err)
	}

	// Add more trail entries (e.g. 25 total) to test max 20, newest first
	for i := 2; i <= 25; i++ {
		_, err := h.mem.SaveCheckpoint(ctx, entity.SaveCheckpointInput{
			Project:    project,
			Focus:      fmt.Sprintf("Focus %d", i),
			Breadcrumb: fmt.Sprintf("Trail %d", i),
			AgentLabel: &agentLabel,
			Source:     &source,
		})
		if err != nil {
			t.Fatalf("SaveCheckpoint %d: %v", i, err)
		}
	}

	// 2. Open tasks vs done tasks
	_, err = h.mem.CreateEntities(ctx, project, []entity.EntityInput{
		{
			Name:         "OpenTask1",
			Type:         "task",
			Observations: []string{"task description", "status: in_progress"},
		},
		{
			Name:         "DoneTask",
			Type:         "task",
			Observations: []string{"task completed", "status: done"},
		},
		{
			Name:         "OpenTask2",
			Type:         "task",
			Observations: []string{"another task to do"},
		},
	})
	if err != nil {
		t.Fatalf("create tasks: %v", err)
	}

	// 3. Recent decisions (within 14 days) vs old decision (>14 days)
	_, err = h.mem.CreateEntities(ctx, project, []entity.EntityInput{
		{
			Name:         "DecisionRecent",
			Type:         "decision",
			Observations: []string{"use postgres over redis for persistence"},
		},
		{
			Name:         "DecisionOld",
			Type:         "decision",
			Observations: []string{"old decision from 20 days ago"},
		},
	})
	if err != nil {
		t.Fatalf("create decisions: %v", err)
	}

	// Backdate DecisionOld in DB (disable trigger briefly or execute with trigger disabled)
	twentyDaysAgo := time.Now().Add(-20 * 24 * time.Hour)
	_, err = h.pool.Exec(ctx, `ALTER TABLE memory_entities DISABLE TRIGGER trg_touch_entities`)
	if err != nil {
		t.Fatalf("disable trigger: %v", err)
	}
	_, err = h.pool.Exec(ctx, `UPDATE memory_entities SET updated_at = $1 WHERE project_id = $2 AND name = 'DecisionOld'`, twentyDaysAgo, project)
	if err != nil {
		t.Fatalf("backdate DecisionOld: %v", err)
	}
	_, err = h.pool.Exec(ctx, `ALTER TABLE memory_entities ENABLE TRIGGER trg_touch_entities`)
	if err != nil {
		t.Fatalf("enable trigger: %v", err)
	}

	// Now run SessionBootstrap
	res, err := h.mem.SessionBootstrap(ctx, entity.SessionBootstrapInput{
		Project:    project,
		AgentLabel: &agentLabel,
		Source:     &source,
		SessionID:  &sessionID,
	})
	if err != nil {
		t.Fatalf("SessionBootstrap failed: %v", err)
	}

	// Verify Checkpoint
	if res.Checkpoint == nil {
		t.Fatalf("expected active checkpoint, got nil")
	}
	if res.Checkpoint.Focus != "Focus 25" {
		t.Errorf("expected checkpoint focus 'Focus 25', got %q", res.Checkpoint.Focus)
	}
	if res.Checkpoint.SavedBy != agentLabel {
		t.Errorf("expected checkpoint saved_by %q, got %q", agentLabel, res.Checkpoint.SavedBy)
	}

	// Verify BreadcrumbTrail: max 20, newest first
	if len(res.BreadcrumbTrail) != 20 {
		t.Fatalf("expected 20 breadcrumb entries, got %d", len(res.BreadcrumbTrail))
	}
	// Newest should be Trail 25
	if !containsSubstring(res.BreadcrumbTrail[0], "Trail 25") {
		t.Errorf("expected newest trail entry at index 0 to contain 'Trail 25', got %q", res.BreadcrumbTrail[0])
	}
	// Oldest in the 20 should be Trail 6 (since 25 down to 6 is 20 entries)
	if !containsSubstring(res.BreadcrumbTrail[19], "Trail 6") {
		t.Errorf("expected oldest trail entry at index 19 to contain 'Trail 6', got %q", res.BreadcrumbTrail[19])
	}

	// Verify OpenTasks: OpenTask1 and OpenTask2, NOT DoneTask
	if len(res.OpenTasks) != 2 {
		t.Fatalf("expected 2 open tasks, got %d", len(res.OpenTasks))
	}
	taskNames := make(map[string]bool)
	for _, task := range res.OpenTasks {
		taskNames[task.Name] = true
	}
	if !taskNames["OpenTask1"] || !taskNames["OpenTask2"] {
		t.Errorf("expected OpenTask1 and OpenTask2 in open tasks, got %+v", res.OpenTasks)
	}
	if taskNames["DoneTask"] {
		t.Errorf("DoneTask should not be in open tasks")
	}

	// Verify RecentDecisions: DecisionRecent should be present, DecisionOld should NOT
	if len(res.RecentDecisions) != 1 || res.RecentDecisions[0].Name != "DecisionRecent" {
		t.Errorf("expected only DecisionRecent in recent decisions, got %+v", res.RecentDecisions)
	}

	// Verify TopEntities (top 5 by recency / confidence)
	if len(res.TopEntities) == 0 || len(res.TopEntities) > 5 {
		t.Errorf("expected between 1 and 5 top entities, got %d", len(res.TopEntities))
	}

	// Verify history log: session_start
	timeline, err := h.mem.repo.GetTimeline(ctx, project, time.Time{}, 50, nil, nil)
	if err != nil {
		t.Fatalf("GetTimeline failed: %v", err)
	}
	foundSessionStart := false
	for _, entry := range timeline {
		if entry.Action == "session_start" {
			foundSessionStart = true
			if entry.AgentLabel == nil || *entry.AgentLabel != agentLabel {
				t.Errorf("expected session_start agent_label %q, got %+v", agentLabel, entry.AgentLabel)
			}
			if entry.Source == nil || *entry.Source != source {
				t.Errorf("expected session_start source %q, got %+v", source, entry.Source)
			}
			break
		}
	}
	if !foundSessionStart {
		t.Errorf("session_start event not found in timeline: %+v", timeline)
	}
}
