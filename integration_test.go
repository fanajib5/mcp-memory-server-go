package main

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationPool returns a pool against DATABASE_URL after ensuring the schema
// and wiping data, so tests start from a clean slate. Skipped when DATABASE_URL
// is unset (keeps `go test` green in environments without a database).
func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := EnsureSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	// Clean slate (cascades to observations + relations).
	if _, err := pool.Exec(ctx, `DELETE FROM memory_entities`); err != nil {
		t.Fatalf("clean: %v", err)
	}
	return pool
}

// TestProjectIsolationAndRoundTrip covers the three new features end-to-end:
// project isolation (#1), relation-type normalization (#3), and export/import (#2).
func TestProjectIsolationAndRoundTrip(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	// The same entity name must coexist in two different projects.
	if _, err := CreateEntities(ctx, pool, "projA", []EntityInput{
		{Name: "X", EntityType: "project", Observations: []string{"uses pg"}},
		{Name: "Y", EntityType: "tool"},
	}); err != nil {
		t.Fatalf("create projA: %v", err)
	}
	if _, err := CreateEntities(ctx, pool, "projB", []EntityInput{
		{Name: "X", EntityType: "project"},
	}); err != nil {
		t.Fatalf("create projB: %v", err)
	}

	// Search is scoped per project.
	aHits, err := SearchMemory(ctx, pool, "projA", "pg", 10)
	if err != nil {
		t.Fatalf("search projA: %v", err)
	}
	if len(aHits) != 1 || aHits[0].Name != "X" {
		t.Fatalf("search projA = %+v, want single hit X", aHits)
	}
	bHits, err := SearchMemory(ctx, pool, "projB", "pg", 10)
	if err != nil {
		t.Fatalf("search projB: %v", err)
	}
	if len(bHits) != 0 {
		t.Fatalf("search projB = %+v, want no hits (isolation)", bHits)
	}

	// Relation type is normalized: "depends on" -> DEPENDS_ON.
	if _, err := CreateRelations(ctx, pool, "projA", []RelationInput{
		{From: "X", To: "Y", RelationType: "depends on"},
	}); err != nil {
		t.Fatalf("relations: %v", err)
	}
	var rt string
	if err := pool.QueryRow(ctx, `SELECT relation_type FROM memory_relations`).Scan(&rt); err != nil {
		t.Fatalf("read relation: %v", err)
	}
	if rt != "DEPENDS_ON" {
		t.Fatalf("relation_type = %q, want DEPENDS_ON", rt)
	}

	// Export -> delete -> import restores everything (round-trip).
	payload, err := ExportGraph(ctx, pool, "projA")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(payload.Entities) != 2 || len(payload.Relations) != 1 {
		t.Fatalf("export = %+v, want 2 entities + 1 relation", payload)
	}
	if err := DeleteEntities(ctx, pool, "projA", []string{"X", "Y"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	res, err := ImportGraph(ctx, pool, "projA", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.EntitiesCreated != 2 || res.RelationsCreated != 1 {
		t.Fatalf("import result = %+v, want 2 entities + 1 relation", res)
	}
	restored, err := SearchMemory(ctx, pool, "projA", "pg", 10)
	if err != nil {
		t.Fatalf("search after import: %v", err)
	}
	if len(restored) != 1 || restored[0].Name != "X" {
		t.Fatalf("search after import = %+v, want X restored", restored)
	}
}

// TestSearchAggregatesAcrossObservations locks in the fix for multi-word search:
// the query's terms live in DIFFERENT observations of the same entity (and not in
// its name). The old per-row `@@` match ANDed the terms within a single
// observation, so it returned nothing here. The aggregated per-entity vector must
// match.
func TestSearchAggregatesAcrossObservations(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	if _, err := CreateEntities(ctx, pool, "default", []EntityInput{
		{
			Name:       "Toko Contoh",
			EntityType: "project",
			Observations: []string{
				"sebuah proyek rintisan",
				"bergerak di bidang bisnis kuliner",
			},
		},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Multi-word query whose words are spread across two separate observations.
	hits, err := SearchMemory(ctx, pool, "default", "proyek bisnis", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Name != "Toko Contoh" {
		t.Fatalf("search 'proyek bisnis' = %+v, want single hit 'Toko Contoh' (terms span observations)", hits)
	}

	// Single-token search still works.
	hits2, err := SearchMemory(ctx, pool, "default", "kuliner", 10)
	if err != nil {
		t.Fatalf("search2: %v", err)
	}
	if len(hits2) != 1 || hits2[0].Name != "Toko Contoh" {
		t.Fatalf("search 'kuliner' = %+v, want single hit 'Toko Contoh'", hits2)
	}
}
