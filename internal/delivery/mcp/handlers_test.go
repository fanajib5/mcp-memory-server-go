package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/repository"
	"mcp-memory-server/internal/usecase"
)

func newTestHandlers(t *testing.T) (*Handlers, *pgxpool.Pool) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := repository.EnsureSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM memory_entities`); err != nil {
		t.Fatalf("clean entities: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM memory_history`); err != nil {
		t.Fatalf("clean history: %v", err)
	}

	repo := repository.NewMemoryRepository(pool)
	uc := usecase.NewMemoryUseCase(repo, nil, nil)
	return NewHandlers(uc), pool
}

func TestNewMCPToolsHandlers(t *testing.T) {
	h, pool := newTestHandlers(t)
	defer pool.Close()
	ctx := context.Background()
	project := "mcptestproj"

	// 1. Test handleCreateEntities with attribution and tags
	src := "claude-code"
	sess := "session-123"
	agent := "Claude Code @ kantor"
	cRes, _, err := h.handleCreateEntities(ctx, nil, CreateEntitiesInput{
		Project: project,
		Entities: []entity.EntityInput{
			{
				Name:         "TaskOne",
				Type:         "task",
				Observations: []string{"initial observation"},
				Tags:         []string{"backend", "critical"},
			},
		},
		Source:     &src,
		SessionID:  &sess,
		AgentLabel: &agent,
	})
	if err != nil {
		t.Fatalf("handleCreateEntities failed: %v", err)
	}
	if len(cRes.Content) == 0 {
		t.Fatalf("expected content in cRes")
	}

	// 2. Test handleAddObservations with attribution
	aRes, _, err := h.handleAddObservations(ctx, nil, AddObservationsInput{
		Project:      project,
		EntityName:   "TaskOne",
		Observations: []string{"status: working"},
		Source:       &src,
		SessionID:    &sess,
		AgentLabel:   &agent,
	})
	if err != nil {
		t.Fatalf("handleAddObservations failed: %v", err)
	}
	if len(aRes.Content) == 0 {
		t.Fatalf("expected content in aRes")
	}

	// 3. Test handleSearch with EntityTypes and TagsAny
	sRes, _, err := h.handleSearch(ctx, nil, SearchInput{
		Project:     project,
		Query:       "TaskOne",
		EntityTypes: []string{"task"},
		TagsAny:     []string{"backend"},
	})
	if err != nil {
		t.Fatalf("handleSearch failed: %v", err)
	}
	if len(sRes.Content) == 0 {
		t.Fatalf("expected content in sRes")
	}

	// 4. Test handleSaveCheckpoint
	saveRes, _, err := h.handleSaveCheckpoint(ctx, nil, SaveCheckpointInput{
		Project:     project,
		Focus:       "Implementing MCP tools",
		Uncommitted: "inputs.go, handlers.go",
		Blockers:    "none",
		NextStep:    "run tests",
		Breadcrumb:  "Working on MCP delivery layer",
		Source:      &src,
		SessionID:   &sess,
		AgentLabel:  &agent,
	})
	if err != nil {
		t.Fatalf("handleSaveCheckpoint failed: %v", err)
	}
	if len(saveRes.Content) == 0 {
		t.Fatalf("expected content in saveRes")
	}

	// 5. Test handleSessionBootstrap
	bootRes, _, err := h.handleSessionBootstrap(ctx, nil, SessionBootstrapInput{
		Project:    project,
		Source:     &src,
		SessionID:  &sess,
		AgentLabel: &agent,
	})
	if err != nil {
		t.Fatalf("handleSessionBootstrap failed: %v", err)
	}
	if len(bootRes.Content) == 0 {
		t.Fatalf("expected content in bootRes")
	}

	// 6. Test handleTimeline
	timeRes, _, err := h.handleTimeline(ctx, nil, TimelineInput{
		Project: project,
		Since:   time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("handleTimeline failed: %v", err)
	}
	if len(timeRes.Content) == 0 {
		t.Fatalf("expected content in timeRes")
	}
}

func TestServerToolRegistration(t *testing.T) {
	h, pool := newTestHandlers(t)
	defer pool.Close()

	server := BuildServer(h)
	if server == nil {
		t.Fatalf("BuildServer returned nil")
	}
}
