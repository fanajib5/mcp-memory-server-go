package usecase

import (
	"context"
	"testing"
	"time"

	"mcp-memory-server/internal/entity"
)

func TestSaveCheckpointOverwritesActiveAndAppendsTrail(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()
	ctx := context.Background()

	project := "checkproj"
	agentLabel := "Claude Code @ kantor"
	source := "claude-code"
	sessionID := "sess-1"

	// 1. Save first checkpoint
	res1, err := h.mem.SaveCheckpoint(ctx, entity.SaveCheckpointInput{
		Project:     project,
		Focus:       "Initial focus setup",
		Uncommitted: "modified file a.go",
		Blockers:    "none",
		NextStep:    "run tests",
		Breadcrumb:  "First breadcrumb",
		Source:      &source,
		SessionID:   &sessionID,
		AgentLabel:  &agentLabel,
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint 1 failed: %v", err)
	}
	if res1.Message != "Checkpoint saved. Trail updated." {
		t.Errorf("unexpected message: %q", res1.Message)
	}

	// Verify active checkpoint entity
	activeName := project + ":checkpoint:active"
	detail1, err := h.stats.GetEntityDetail(ctx, project, activeName)
	if err != nil {
		t.Fatalf("get active checkpoint entity: %v", err)
	}
	if detail1.Type != "checkpoint" {
		t.Errorf("expected entity_type 'checkpoint', got %q", detail1.Type)
	}
	// Observations should include focus, uncommitted, blockers, next_step, source, saved_at
	obsMap1 := make(map[string]string)
	for _, obs := range detail1.Observations {
		for _, prefix := range []string{"focus: ", "uncommitted: ", "blockers: ", "next_step: ", "source: ", "saved_at: "} {
			if len(obs.Content) >= len(prefix) && obs.Content[:len(prefix)] == prefix {
				obsMap1[prefix[:len(prefix)-2]] = obs.Content[len(prefix):]
			}
		}
	}
	if obsMap1["focus"] != "Initial focus setup" {
		t.Errorf("expected focus 'Initial focus setup', got %q", obsMap1["focus"])
	}
	if obsMap1["source"] != agentLabel {
		t.Errorf("expected source %q, got %q", agentLabel, obsMap1["source"])
	}

	// Verify trail entity
	trailName := project + ":checkpoint:trail"
	trailDetail1, err := h.stats.GetEntityDetail(ctx, project, trailName)
	if err != nil {
		t.Fatalf("get trail entity: %v", err)
	}
	if trailDetail1.Type != "checkpoint_trail" {
		t.Errorf("expected entity_type 'checkpoint_trail', got %q", trailDetail1.Type)
	}
	if len(trailDetail1.Observations) != 1 {
		t.Fatalf("expected 1 trail observation, got %d", len(trailDetail1.Observations))
	}
	// Check breadcrumb format: [<ISO_TIMESTAMP>] [<AGENT_LABEL>] <breadcrumb>
	trailContent1 := trailDetail1.Observations[0].Content
	if !containsSubstring(trailContent1, "["+agentLabel+"] First breadcrumb") {
		t.Errorf("trail observation does not match expected format: %q", trailContent1)
	}

	// Check history event checkpoint_saved was logged
	hist, err := h.mem.GetHistory(ctx, project, activeName, 10)
	if err != nil {
		t.Fatalf("get history for active checkpoint: %v", err)
	}
	foundSaved := false
	for _, entry := range hist {
		if entry.Action == "checkpoint_saved" {
			foundSaved = true
			if entry.AgentLabel == nil || *entry.AgentLabel != agentLabel {
				t.Errorf("expected history agent_label %q, got %+v", agentLabel, entry.AgentLabel)
			}
			if entry.Source == nil || *entry.Source != source {
				t.Errorf("expected history source %q, got %+v", source, entry.Source)
			}
			break
		}
	}
	if !foundSaved {
		t.Errorf("expected history action 'checkpoint_saved' not found in %+v", hist)
	}

	// 2. Save second checkpoint - should overwrite active observations and append to trail
	time.Sleep(10 * time.Millisecond)
	res2, err := h.mem.SaveCheckpoint(ctx, entity.SaveCheckpointInput{
		Project:     project,
		Focus:       "Second focus update with long focus that needs default breadcrumb truncation because breadcrumb is omitted",
		NextStep:    "verify build",
		Source:      &source,
		SessionID:   &sessionID,
		AgentLabel:  &agentLabel,
	})
	if err != nil {
		t.Fatalf("SaveCheckpoint 2 failed: %v", err)
	}
	if res2.Message != "Checkpoint saved. Trail updated." {
		t.Errorf("unexpected message 2: %q", res2.Message)
	}

	detail2, err := h.stats.GetEntityDetail(ctx, project, activeName)
	if err != nil {
		t.Fatalf("get active checkpoint detail 2: %v", err)
	}
	obsMap2 := make(map[string]string)
	for _, obs := range detail2.Observations {
		for _, prefix := range []string{"focus: ", "uncommitted: ", "blockers: ", "next_step: ", "source: ", "saved_at: "} {
			if len(obs.Content) >= len(prefix) && obs.Content[:len(prefix)] == prefix {
				obsMap2[prefix[:len(prefix)-2]] = obs.Content[len(prefix):]
			}
		}
	}
	if obsMap2["focus"] != "Second focus update with long focus that needs default breadcrumb truncation because breadcrumb is omitted" {
		t.Errorf("active checkpoint observations not overwritten: %+v", obsMap2)
	}
	// uncommitted was empty in input 2, so it shouldn't have the old one
	if _, ok := obsMap2["uncommitted"]; ok {
		t.Errorf("expected uncommitted not to be present, got %q", obsMap2["uncommitted"])
	}

	// Trail should now have 2 observations
	trailDetail2, err := h.stats.GetEntityDetail(ctx, project, trailName)
	if err != nil {
		t.Fatalf("get trail detail 2: %v", err)
	}
	if len(trailDetail2.Observations) != 2 {
		t.Fatalf("expected 2 trail observations, got %d", len(trailDetail2.Observations))
	}
	// Verify default breadcrumb truncation to 80 chars
	expectedBreadcrumb := ("Second focus update with long focus that needs default breadcrumb truncation because breadcrumb is omitted")[:80]
	if !containsSubstring(trailDetail2.Observations[1].Content, "["+agentLabel+"] "+expectedBreadcrumb) {
		t.Errorf("second trail observation does not match expected truncated breadcrumb: %q", trailDetail2.Observations[1].Content)
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
