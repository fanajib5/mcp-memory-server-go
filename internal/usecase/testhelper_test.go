package usecase

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcp-memory-server/internal/gateway"
	"mcp-memory-server/internal/repository"
)

// testHarness builds memory + stats usecases against DATABASE_URL after ensuring
// the schema and wiping data, so tests start from a clean slate. Skipped when
// DATABASE_URL is unset (keeps `go test` green without a database).
type testHarness struct {
	mem   *MemoryUseCase
	stats *StatsUseCase
	pool  *pgxpool.Pool
}

func newTestHarness(t *testing.T) *testHarness {
	return newTestHarnessWithEmbedder(t, nil)
}

// newTestHarnessWithEmbedder builds a harness with a custom embedder (for
// semantic search tests). nil embedder = lexical-only mode.
func newTestHarnessWithEmbedder(t *testing.T, emb gateway.Embedder) *testHarness {
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
		t.Fatalf("clean: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM memory_history`); err != nil {
		t.Fatalf("clean history: %v", err)
	}
	return &testHarness{
		mem:   NewMemoryUseCase(repository.NewMemoryRepository(pool), emb, nil),
		stats: NewStatsUseCase(repository.NewStatsRepository(pool)),
		pool:  pool,
	}
}

func (h *testHarness) close() { h.pool.Close() }
