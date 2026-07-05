# Design: Observation Versioning

Tanggal: 2026-07-05
Status: VALIDATED

## Tujuan

Audit trail untuk perubahan observation content + entity rename/type-change.
Append-only history table, application-level triggers (in repo), akses via
MCP tool + UI page.

## Keputusan design (validated)

1. **Scope**: observation edits/deletes + entity renames/type-changes.
   Relations tidak (jarang di-edit).
2. **Storage**: append-only `memory_history` table.
3. **Trigger**: application-level (repo methods INSERT history dalam tx yang sama).
4. **Access**: MCP tool `memory_get_history` + UI page `/ui/history` dengan diff view.

## Schema (idempotent)

```sql
CREATE TABLE IF NOT EXISTS memory_history (
    id BIGSERIAL PRIMARY KEY,
    entity_id INT,
    entity_name TEXT NOT NULL,
    observation_id INT,
    action TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    confidence REAL,
    happened_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_history_entity ON memory_history (entity_name, happened_at DESC);
```

Actions: `observation_updated`, `observation_deleted`, `entity_renamed`, `entity_type_changed`.

## Layer changes

### Repository
- `UpdateObservation`, `DeleteObservation`, `UpdateObservationByContent`,
  `DeleteObservationByContent`: INSERT history row THEN mutate (same tx).
- `UpdateEntity`: INSERT history (renamed/type_changed) THEN UPDATE.
- New method `GetHistory(ctx, project, entityName, limit)`.

### UseCase
- Passthrough to repo GetHistory (no domain rules needed).

### Delivery MCP
- New tool `memory_get_history` (input: project, entityName, limit).

### Delivery HTTP UI
- New page `/ui/history?name=X&p=project` — list events with old→new diff.
- Link from entity detail page.

## Testing
- Integration: edit → history row, delete → history row, rename → history row.
- MCP tool returns history.
