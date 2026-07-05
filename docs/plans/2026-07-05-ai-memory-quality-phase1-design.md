# Design: AI Memory Quality — Phase 1 (Confidence + Decay)

Tanggal: 2026-07-05
Status: VALIDATED
Phase: 1 of 2 (Phase 2 = pgvector semantic search, terpisah)

## Tujuan

Tingkatkan kualitas retrieval AI: (1) AI bisa menandai keyakinan per fakta,
(2) memory yang jarang diakses turun prioritasnya otomatis. Keduanya bekerja
sebagai re-ranking signal pada search — tidak ada informasi yang dihilangkan.

## Keputusan design (validated)

1. **Granularity**: confidence per-observation (fakta), decay per-entity (topik).
2. **Retrieval**: re-rank semua hasil by `final_score = ts_rank × avg_confidence × recency_factor`. Tidak ada hard filter.
3. **Decay storage**: computed-on-read (no scheduler). Simpan `last_accessed_at` saja.
4. **Confidence model**: `float nullable`, opsional. NULL = netral (×1.0).
5. **Access tracking**: `last_accessed_at` update HANYA saat entity muncul di `memory_search`. `read_graph` (bulk debug dump) tidak update.
6. **MCP inputs**: parallel slice `Confidences []float64` (opsional), backward-compat.

## Schema changes (idempotent)

```sql
-- Confidence per observation (nullable: NULL = netral, scoring treat as 1.0)
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS confidence REAL;

-- Decay tracking per entity (nullable: NULL = belum pernah di-access,
-- fallback ke created_at untuk hitung umur)
ALTER TABLE memory_entities ADD COLUMN IF NOT EXISTS last_accessed_at TIMESTAMPTZ;
```

Backward compat: existing rows → NULL (netral). No data migration, no rebuild.

## Scoring formula (computed-on-read in SQL)

```
recency_factor = exp(-age_days / 30)         -- half-life 30 hari
age_days       = (now() - COALESCE(last_accessed_at, created_at)) / interval '1 day'
avg_confidence = avg(COALESCE(confidence, 1.0))   -- per entity
final_score    = ts_rank × avg_confidence × recency_factor
```

Decay curve: 30 hari → ×0.37, 60 hari → ×0.14, 90 hari → ×0.05.
Entity baru di-access (last_accessed_at ≈ now) → factor ≈ 1.0.

## Layer changes

### Repository (`internal/repository/memory.go`)
- `Search()`: rewrite SELECT untuk hitung `avg_confidence`, `recency_factor`, `final_score`.
  ORDER BY `final_score DESC` (bukan `ts_rank` saja). Tetap satu query, no N+1.
- `TouchAccessed(ctx, entityIDs []int)`: UPDATE last_accessed_at = now() WHERE id = ANY.
- `AddObservations()`, `Import()`, `CreateEntities()`: terima confidences parallel slice.

### UseCase (`internal/usecase/memory.go`)
- `Search()`: setelah results, best-effort `repo.TouchAccessed(ctx, ids)`. Error di-log,
  tidak gagalkan search.
- `AddObservations()` / `Import()` / `CreateEntities()`: pass confidences.
- `UpdateObservationByContent()`: + param `newConfidence *float64` opsional.

### Delivery MCP (`internal/delivery/mcp/inputs.go`)
- `AddObservationsInput.Confidences []float64` (parallel ke Observations).
- `CreateEntitiesInput`: EntityInput dapat `Confidences []float64` parallel ke Observations.
- `UpdateObservationInput.NewConfidence *float64` opsional.
- `SearchResult` (entity package): + `Confidence *float64` (entity avg), `Score *float64` (debug).

### Delivery HTTP UI
- Entity detail: badge confidence per-obs (hijau ≥0.7, kuning 0.3-0.7, merah <0.3, abu NULL).
- Entity list: "last accessed" relatif + indikator stale.
- Add observation form: optional confidence slider.

## Testing
- Unit: normalize confidence (clamp 0-1), parallel-slice alignment validation.
- Integration: search re-rank order (high-conf entity outranks low-conf for same query),
  decay (old entity sinks), access-touch (search bumps last_accessed_at).
- Reuse testHarness helper yang sudah ada.

## Yang TIDAK berubah
- Behavior existing (semua field opsional, NULL = netral).
- Dependencies (no new libs — pgx supports exp() natively via pg_math / SQL).
- Phase 2 (pgvector) terpisah, tidak disentuh di sini.
