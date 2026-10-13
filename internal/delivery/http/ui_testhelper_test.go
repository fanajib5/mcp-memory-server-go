// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcp-memory-server/internal/config"
	"mcp-memory-server/internal/repository"
	"mcp-memory-server/internal/usecase"
)

// uiTestHarness wires a UI struct (with a parsed template) against DATABASE_URL
// after ensuring the schema and wiping data. Skipped when DATABASE_URL is unset.
func uiTestHarness(t *testing.T) *UI {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := repository.EnsureSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM memory_entities`); err != nil {
		t.Fatalf("clean: %v", err)
	}

	cfg := testConfig()
	tmpl := MustParseTemplates()
	return &UI{
		Cfg:     cfg,
		UC:      usecase.NewMemoryUseCase(repository.NewMemoryRepository(pool), nil, nil),
		SU:      usecase.NewStatsUseCase(repository.NewStatsRepository(pool)),
		Tmpl:    tmpl,
		Session: NewSession(cfg),
	}
}

var _ = config.Config{} // keep config import live if extended later
