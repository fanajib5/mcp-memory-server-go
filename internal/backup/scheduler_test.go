package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneKeepsNewest(t *testing.T) {
	dir := t.TempDir()

	// Create 5 backup files with sequential timestamps.
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, "memory-backup-2026-01-0"+string(rune('1'+i))+"-120000.json")
		os.WriteFile(name, []byte("{}"), 0o644)
		// Small delay so modification order is deterministic across filesystems.
		time.Sleep(5 * time.Millisecond)
	}

	s := &Scheduler{dir: dir, retention: 3}
	if err := s.prune(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("after prune: %d files, want 3", len(entries))
	}
}

func TestPruneNoopUnderRetention(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "memory-backup-2026-01-01-120000.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "memory-backup-2026-01-02-120000.json"), []byte("{}"), 0o644)

	s := &Scheduler{dir: dir, retention: 7}
	if err := s.prune(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("prune deleted files under retention: %d left", len(entries))
	}
}

func TestPruneIgnoresNonBackups(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "memory-backup-2026-01-01-120000.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "random.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644)

	s := &Scheduler{dir: dir, retention: 0}
	if err := s.prune(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	// Non-backup files must survive even with retention=0.
	entries, _ := os.ReadDir(dir)
	var hasRandom, hasReadme bool
	for _, e := range entries {
		if e.Name() == "random.json" {
			hasRandom = true
		}
		if e.Name() == "readme.txt" {
			hasReadme = true
		}
	}
	if !hasRandom || !hasReadme {
		t.Fatalf("prune deleted non-backup files: %+v", entries)
	}
}

func TestStartNoopEmptyCron(t *testing.T) {
	s := &Scheduler{cronSpec: ""}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// Should return immediately without blocking.
	s.Start(ctx, false)
}

func TestStartInvalidCronNoCrash(t *testing.T) {
	s := &Scheduler{cronSpec: "not-a-cron-expr"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// Should log error and return without panicking.
	s.Start(ctx, false)
}
