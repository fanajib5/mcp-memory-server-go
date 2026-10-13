package repository

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchemaMigration(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping schema migration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := EnsureSchema(ctx, pool); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	// 1. Verify new entity types exist in memory_entity_types
	expectedTypes := []string{"task", "checkpoint", "checkpoint_trail"}
	for _, typ := range expectedTypes {
		var exists bool
		err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memory_entity_types WHERE name = $1)", typ).Scan(&exists)
		if err != nil {
			t.Fatalf("query entity type %s: %v", typ, err)
		}
		if !exists {
			t.Errorf("entity type %q was not seeded in memory_entity_types", typ)
		}
	}

	// 2. Verify columns in memory_observations: source, session_id, agent_label, deleted_at
	obsColumns := []string{"source", "session_id", "agent_label", "deleted_at"}
	for _, col := range obsColumns {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM information_schema.columns 
				WHERE table_name = 'memory_observations' AND column_name = $1
			)`, col).Scan(&exists)
		if err != nil {
			t.Fatalf("query observation column %s: %v", col, err)
		}
		if !exists {
			t.Errorf("column %q does not exist in memory_observations", col)
		}
	}

	// 3. Verify columns in memory_history: project_id, source, session_id, agent_label
	histColumns := []string{"project_id", "source", "session_id", "agent_label"}
	for _, col := range histColumns {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM information_schema.columns 
				WHERE table_name = 'memory_history' AND column_name = $1
			)`, col).Scan(&exists)
		if err != nil {
			t.Fatalf("query history column %s: %v", col, err)
		}
		if !exists {
			t.Errorf("column %q does not exist in memory_history", col)
		}
	}

	// 4. Verify column in memory_entities: tags
	var tagsExists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns 
			WHERE table_name = 'memory_entities' AND column_name = 'tags'
		)`).Scan(&tagsExists)
	if err != nil {
		t.Fatalf("query entities tags column: %v", err)
	}
	if !tagsExists {
		t.Errorf("column 'tags' does not exist in memory_entities")
	}

	// 5. Verify index idx_entities_tags exists
	var idxTagsExists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_indexes 
			WHERE tablename = 'memory_entities' AND indexname = 'idx_entities_tags'
		)`).Scan(&idxTagsExists)
	if err != nil {
		t.Fatalf("query idx_entities_tags: %v", err)
	}
	if !idxTagsExists {
		t.Errorf("index 'idx_entities_tags' does not exist")
	}

	// 6. Verify index idx_history_project_happened exists
	var idxHistExists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_indexes 
			WHERE tablename = 'memory_history' AND indexname = 'idx_history_project_happened'
		)`).Scan(&idxHistExists)
	if err != nil {
		t.Fatalf("query idx_history_project_happened: %v", err)
	}
	if !idxHistExists {
		t.Errorf("index 'idx_history_project_happened' does not exist")
	}
}
