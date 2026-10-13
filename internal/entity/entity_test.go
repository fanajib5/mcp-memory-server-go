package entity_test

import (
	"encoding/json"
	"testing"

	"mcp-memory-server/internal/entity"
)

func TestEntityStructsFields(t *testing.T) {
	// 1. entity.Observation struct
	obs := entity.Observation{
		ID:         1,
		EntityID:   2,
		Content:    "test content",
		Confidence: nil,
		Source:     ptr("agent-1"),
		SessionID:  ptr("sess-1"),
		AgentLabel: ptr("Claude Code @ kantor"),
		CreatedAt:  "2026-10-13T15:00:00Z",
		DeletedAt:  nil,
	}
	obsBytes, err := json.Marshal(obs)
	if err != nil {
		t.Fatalf("marshal Observation: %v", err)
	}
	var unmarshaledObs entity.Observation
	if err := json.Unmarshal(obsBytes, &unmarshaledObs); err != nil {
		t.Fatalf("unmarshal Observation: %v", err)
	}
	if *unmarshaledObs.Source != "agent-1" || *unmarshaledObs.SessionID != "sess-1" || *unmarshaledObs.AgentLabel != "Claude Code @ kantor" {
		t.Errorf("Observation fields mismatch: %+v", unmarshaledObs)
	}

	// 2. entity.HistoryEntry struct
	hist := entity.HistoryEntry{
		ID:         10,
		ProjectID:  "myproject",
		EntityName: "Alpha",
		Action:     "observation_created",
		OldValue:   nil,
		NewValue:   ptr("new fact"),
		Confidence: nil,
		Source:     ptr("cli"),
		SessionID:  ptr("sess-2"),
		AgentLabel: ptr("Antigravity @ rumah"),
		HappenedAt: "2026-10-13T15:01:00Z",
	}
	histBytes, err := json.Marshal(hist)
	if err != nil {
		t.Fatalf("marshal HistoryEntry: %v", err)
	}
	var unmarshaledHist entity.HistoryEntry
	if err := json.Unmarshal(histBytes, &unmarshaledHist); err != nil {
		t.Fatalf("unmarshal HistoryEntry: %v", err)
	}
	if unmarshaledHist.ProjectID != "myproject" || *unmarshaledHist.Source != "cli" || *unmarshaledHist.AgentLabel != "Antigravity @ rumah" {
		t.Errorf("HistoryEntry fields mismatch: %+v", unmarshaledHist)
	}

	// 3. entity.Entity struct (Tags)
	ent := entity.Entity{
		Name:         "Alpha",
		EntityType:   "task",
		Observations: []string{"fact 1"},
		Tags:         []string{"backend", "go"},
	}
	entBytes, err := json.Marshal(ent)
	if err != nil {
		t.Fatalf("marshal Entity: %v", err)
	}
	var unmarshaledEnt entity.Entity
	if err := json.Unmarshal(entBytes, &unmarshaledEnt); err != nil {
		t.Fatalf("unmarshal Entity: %v", err)
	}
	if len(unmarshaledEnt.Tags) != 2 || unmarshaledEnt.Tags[0] != "backend" {
		t.Errorf("Entity tags mismatch: %+v", unmarshaledEnt)
	}

	// 4. entity.EntityInput (Tags)
	entInput := entity.EntityInput{
		Name:         "Alpha",
		Type:         "task",
		Observations: []string{"fact 1"},
		Tags:         []string{"backend"},
	}
	if len(entInput.Tags) != 1 || entInput.Tags[0] != "backend" {
		t.Errorf("EntityInput tags mismatch: %+v", entInput)
	}

	// 5. entity.Checkpoint
	cp := entity.Checkpoint{
		Focus:       "implement task 1",
		Uncommitted: "schema changes in progress",
		Blockers:    "none",
		NextStep:    "task 2",
		SavedAt:     "2026-10-13T15:00:00Z",
		SavedBy:     "Claude Code @ kantor",
	}
	cpBytes, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("marshal Checkpoint: %v", err)
	}
	var unmarshaledCp entity.Checkpoint
	if err := json.Unmarshal(cpBytes, &unmarshaledCp); err != nil {
		t.Fatalf("unmarshal Checkpoint: %v", err)
	}
	if unmarshaledCp.Focus != "implement task 1" || unmarshaledCp.SavedBy != "Claude Code @ kantor" {
		t.Errorf("Checkpoint mismatch: %+v", unmarshaledCp)
	}

	// 6. entity.SessionBootstrapResult
	bootstrap := entity.SessionBootstrapResult{
		Warning:         ptr("⚠️ agent_label not set"),
		Checkpoint:      &cp,
		BreadcrumbTrail: []string{"step 1", "step 2"},
		OpenTasks:       []entity.Entity{ent},
		RecentDecisions: []entity.Entity{},
		TopEntities:     []entity.SearchResult{},
	}
	bootBytes, err := json.Marshal(bootstrap)
	if err != nil {
		t.Fatalf("marshal SessionBootstrapResult: %v", err)
	}
	var unmarshaledBoot entity.SessionBootstrapResult
	if err := json.Unmarshal(bootBytes, &unmarshaledBoot); err != nil {
		t.Fatalf("unmarshal SessionBootstrapResult: %v", err)
	}
	if *unmarshaledBoot.Warning != "⚠️ agent_label not set" || unmarshaledBoot.Checkpoint.Focus != "implement task 1" {
		t.Errorf("SessionBootstrapResult mismatch: %+v", unmarshaledBoot)
	}

	// 7. entity.CheckpointResult
	cpRes := entity.CheckpointResult{
		Message: "Checkpoint saved. Trail updated.",
	}
	if cpRes.Message == "" {
		t.Errorf("CheckpointResult message empty")
	}
}

func ptr(s string) *string {
	return &s
}
