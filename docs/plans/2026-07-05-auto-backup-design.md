# Design: Auto-Backup Cloud

Tanggal: 2026-07-05
Status: VALIDATED

## Tujuan

Scheduled auto-backup semua memory ke file JSON di Docker volume.
Coolify handle cloud upload (S3/R2/etc). Cron Go native jalan in-process.

## Arsitektur

```
Cron Go (robfig/cron, background goroutine)
  → Export usecase (all projects) → JSON payload
  → Write ke BACKUP_DIR (default /data/backups)
  → Retention: keep N file terbaru (delete older)
Coolify scheduled backup: volume → cloud (S3/R2/MinIO)
```

## Yang diimplement (Go)
1. `internal/backup/scheduler.go` — cron scheduler + export + write file + retention.
2. Config: `BACKUP_CRON` (default `@daily`), `BACKUP_DIR` (default `/data/backups`),
   `BACKUP_RETENTION` (default `7`), `BACKUP_ON_START` (default `false`).
3. Wire di `cmd/server/main.go`: start scheduler, graceful shutdown via ctx.

## Yang TIDAK diimplement (Coolify handles)
- S3/R2 upload, encryption, cloud credentials.

## Dependencies
- `github.com/robfig/cron/v3` (lightweight cron parser + scheduler).

## Testing
- Unit: cron expr parse, retention cleanup logic, filename format.
- Integration: export → file written → content valid JSON.
