// Package backup handles scheduled auto-backup of the memory database to JSON
// files on a Docker volume. Coolify (or any external tool) then syncs the
// volume to cloud storage (S3/R2/MinIO).
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"mcp-memory-server/internal/usecase"
)

// Scheduler runs periodic exports of all memory data to JSON files.
type Scheduler struct {
	uc         *usecase.MemoryUseCase
	dir        string
	retention  int
	cronSpec   string
	cronLogger *log.Logger
}

// NewScheduler builds a backup scheduler. cronSpec empty = disabled.
func NewScheduler(uc *usecase.MemoryUseCase, dir string, retention int, cronSpec string) *Scheduler {
	return &Scheduler{
		uc:        uc,
		dir:       dir,
		retention: retention,
		cronSpec:  cronSpec,
	}
}

// Start launches the cron scheduler in the background. It blocks until ctx is
// cancelled, then stops the cron gracefully. If cronSpec is empty, it's a no-op.
func (s *Scheduler) Start(ctx context.Context, runOnStart bool) {
	if s.cronSpec == "" {
		return
	}

	c := cron.New(cron.WithLogger(cron.PrintfLogger(log.New(os.Stderr, "backup-cron: ", log.LstdFlags))))

	_, err := c.AddFunc(s.cronSpec, func() {
		if err := s.RunOnce(ctx); err != nil {
			log.Printf("auto-backup failed: %v", err)
		}
	})
	if err != nil {
		log.Printf("auto-backup: invalid cron spec %q: %v (disabled)", s.cronSpec, err)
		return
	}

	c.Start()
	log.Printf("auto-backup scheduler started: %s", s.cronSpec)

	if runOnStart {
		go func() {
			if err := s.RunOnce(ctx); err != nil {
				log.Printf("startup backup failed: %v", err)
			}
		}()
	}

	<-ctx.Done()
	stopCtx := c.Stop()
	<-stopCtx.Done()
	log.Printf("auto-backup scheduler stopped")
}

// RunOnce exports all projects to a timestamped JSON file, then prunes old
// backups beyond the retention limit.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.dir, err)
	}

	payload, err := s.uc.Export(ctx, "")
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	filename := fmt.Sprintf("memory-backup-%s.json", time.Now().UTC().Format("2006-01-02-150405"))
	path := filepath.Join(s.dir, filename)

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal backup: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	log.Printf("backup written: %s (%d bytes)", path, len(data))

	if err := s.prune(); err != nil {
		log.Printf("backup retention prune failed (non-fatal): %v", err)
	}
	return nil
}

// prune deletes the oldest backup files, keeping at most s.retention.
func (s *Scheduler) prune() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}

	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "memory-backup-") && strings.HasSuffix(e.Name(), ".json") {
			backups = append(backups, filepath.Join(s.dir, e.Name()))
		}
	}

	if len(backups) <= s.retention {
		return nil
	}

	// Sort newest first (filenames are timestamp-ordered).
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))

	for _, f := range backups[s.retention:] {
		if err := os.Remove(f); err != nil {
			log.Printf("prune: remove %s: %v", f, err)
		} else {
			log.Printf("prune: removed old backup %s", filepath.Base(f))
		}
	}
	return nil
}
