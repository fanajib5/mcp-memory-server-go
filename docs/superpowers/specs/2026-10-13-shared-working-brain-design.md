# Shared Working Brain — Design Document
*Milestone 1 & 2: Multi-Agent Metadata, Timeline, Checkpoint, and Session Bootstrap*

**Date**: 2026-10-13
**Status**: Approved

---

## Intent and Why

When a developer switches machines between sessions (e.g., office → home), a new agent starts completely blind to the previous session's work-in-progress — especially for uncommitted local changes that don't exist in git. The result: the developer must manually re-explain context before any productive work can begin.

This design makes the memory server the **shared working brain** across all sessions and devices. It stores enough context that a new agent can immediately resume work without any manual re-orientation from the developer.

---

## Goals

- Agent at session start can load full context of the previous session in one tool call.
- Every observation, checkpoint, and event is traceable to its source device/agent.
- Checkpoints capture uncommitted state and semantic progress that git cannot express.
- Timeline provides a human-readable, narrative chronology instead of raw git history.
- Soft warning if device identity is not configured (non-blocking, but actionable).

## Non-Goals

- No new daemons, background sync processes, or external services.
- No mandatory blocking on missing `agent_label` — everything degrades gracefully.
- No versioning database (no Dolt-style branching).
- No changes to existing MCP tool parameter names that break callers (all new fields are optional).

## Anti-Goals

- Do not build anything that requires per-session setup ritual by the developer.
- Do not make checkpoint saving a required manual step — implicit fallback must always work.

---

## Constraints

- Existing PostgreSQL schema must remain backward-compatible. All new columns use `ADD COLUMN IF NOT EXISTS` with safe defaults (`NULL` or `''`).
- All new MCP tool input fields are optional and pointer-typed (no breaking changes to existing clients).
- The server is deployed via Coolify (Docker). No new infrastructure dependencies.
- Integration test suite uses `go test ./... -p 1` (sequential, shared DB). New tests must follow this convention.

---

## Key Decisions Settled

1. **Checkpoint Save Path (Opsi A)**: `CreateEntities` usecase/repo flow will support a `replaceObs bool` mode (deleting existing observations for the active checkpoint entity before re-inserting fresh observations), keeping the codebase DRY.
2. **Breadcrumb Trail Limit (Opsi a)**: In `memory_session_bootstrap`, return the latest 20 breadcrumb entries (newest first). The trail entity accumulates them in append order.
3. **Strict Project Scoping for History**: `memory_get_history` now strictly requires `project` and filters by `project_id`, eliminating cross-project event leakage.
4. **Metadata Attribution**: Per-call level for `memory_create_entities` (via `CreateEntitiesInput.Source`, `AgentLabel`, `SessionID`), applying uniformly to initial observations created in that call.
5. **Tags Scoping**: Scoped per-project. `memory_search` with `tags_any` filters entities within the requested project matching any of the specified tags.
6. **Session Identification & History**:
   - `session_id` is an opaque, free-form string (no strict UUID validation required).
   - `memory_session_bootstrap` logs a `session_start` event into `memory_history`.

---

## Part 1: Agent Identity Metadata (Source + Agent Label)

### Schema changes

```sql
-- memory_entity_types seed new types:
INSERT INTO memory_entity_types (name) VALUES
    ('task'), ('checkpoint'), ('checkpoint_trail')
ON CONFLICT (name) DO NOTHING;

-- memory_observations
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS source TEXT;
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS session_id TEXT;
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS agent_label TEXT;
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- memory_history
ALTER TABLE memory_history ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE memory_history ADD COLUMN IF NOT EXISTS source TEXT;
ALTER TABLE memory_history ADD COLUMN IF NOT EXISTS session_id TEXT;
ALTER TABLE memory_history ADD COLUMN IF NOT EXISTS agent_label TEXT;

CREATE INDEX IF NOT EXISTS idx_history_project_happened ON memory_history (project_id, happened_at DESC);
```

### History Fix: Record `observation_created` events

Currently `memory_history` only records `observation_updated`, `observation_deleted`, and `entity_renamed`. The `AddObservations` and `CreateEntities` repository methods must write `observation_created` events to `memory_history` within the same transaction. All history writes will explicitly record `project_id`.

### MCP tool input changes (all optional, backward-compatible)

Tools affected: `memory_add_observations`, `memory_create_entities`, `memory_save_checkpoint` (new), `memory_session_bootstrap` (new).

New optional input fields:
```go
Source     *string `json:"source,omitempty"`
SessionID  *string `json:"session_id,omitempty"`
AgentLabel *string `json:"agent_label,omitempty"`
```

---

## Part 2: Active Checkpoint + Semantic Breadcrumb Chain

### Storage model

Both checkpoint and breadcrumb chain are stored as entities in the knowledge graph:

```
Entity: "<project>:checkpoint:active"
  type: checkpoint
  observations:
    - "focus: <current focus>"
    - "uncommitted: <description of local changes>"
    - "blockers: <known blockers>"
    - "next_step: <what to do next>"
    - "source: <agent_label>"
    - "saved_at: <ISO timestamp>"

Entity: "<project>:checkpoint:trail"
  type: checkpoint_trail
  observations (appended):
    - "[2026-10-13T14:00+07:00] [Claude Code @ kantor] Mengubah layout form login"
    - "[2026-10-13T15:30+07:00] [Claude Code @ kantor] Wiring auth token ke localStorage"
```

### New MCP tool: `memory_save_checkpoint`

```
Input:
  project       string  (required)
  focus         string  (required) — what is being worked on right now
  uncommitted   string  (optional) — description of local changes not yet in git
  blockers      string  (optional) — known blockers or unresolved questions
  next_step     string  (optional) — recommended action for the next session
  breadcrumb    string  (optional) — short summary for the trail (≤80 chars)
                                     defaults to first 80 chars of focus if omitted
  source        string  (optional)
  session_id    string  (optional)
  agent_label   string  (optional)

Behavior:
  1. Upsert entity "<project>:checkpoint:active" with replaceObs=true (deletes old observations, inserts fresh).
  2. Append observation to "<project>:checkpoint:trail" with formatted breadcrumb.
  3. Record history event "checkpoint_saved".
  4. Return: "Checkpoint saved. Trail updated."
```

---

## Part 3: `memory_session_bootstrap`

### New MCP tool: `memory_session_bootstrap`

```
Input:
  project       string  (required)
  focus         string  (optional) — keyword hint to bias semantic relevance
  source        string  (optional)
  agent_label   string  (optional)
  session_id    string  (optional)

Output:
  {
    "warning": "⚠️ agent_label not set. Add to env: export MCP_AGENT_LABEL='...' export MCP_SOURCE='...' Checkpoint and timeline will be recorded as 'unknown' until configured.",
    "checkpoint": {
      "focus": "...",
      "uncommitted": "...",
      "blockers": "...",
      "next_step": "...",
      "saved_at": "...",
      "saved_by": "..."
    },
    "breadcrumb_trail": [
      "[2026-10-13T15:30] [Claude Code @ kantor] Wiring auth token ke localStorage",
      "[2026-10-13T14:00] [Claude Code @ kantor] Mengubah layout form login"
    ], // max 20 latest, newest first
    "open_tasks": [...],       // entities of type "task" whose observations do not have "status: done"
    "recent_decisions": [...], // entities of type "decision" updated in the last 14 days
    "top_entities": [...]      // top 5 by recency/confidence
  }
```

Behavior:
- Logs `session_start` to `memory_history`.
- Warning included if `agent_label` is omitted/empty.

---

## Part 4: `memory_timeline`

### New MCP tool: `memory_timeline`

```
Input:
  project       string  (required)
  since         string  (optional) — ISO date or shorthand like "7d", "24h" (default "7d")
  limit         int     (optional) — max events to return (default 50)
  entity_name   string  (optional) — filter to one entity
  source        string  (optional) — filter by agent/tool source

Output: chronological list of events, formatted for agent consumption:
  [
    "2026-10-13 15:30 [Claude Code @ kantor] +obs 'Wiring auth token' on arabikids-admin",
    "2026-10-13 14:00 [Claude Code @ kantor] checkpoint_saved on arabikids-admin",
    "2026-10-13 09:15 [Antigravity @ rumah] entity_renamed 'OldAuth' → 'OAuthFlow'"
  ]
```

---

## Part 5: `memory_search` Filter & Tags

### Entity Types Filter
Add `entity_types []string` (optional) to `SearchInput`. Appends `AND e.entity_type = ANY($x)` to SQL queries.

### Tags on Entities
Add `tags TEXT[]` to `memory_entities` with GIN index.
- Add `tags []string` to `EntityInput`.
- Add `tags_any []string` to `SearchInput` using PostgreSQL array overlap operator `&&` (scoped per project).

---

## AGENTS.md / CLAUDE.md Updates

Rules to document:
1. Always call `memory_session_bootstrap` at the beginning of any session.
2. If `warning` is returned, remind the user to configure `MCP_AGENT_LABEL` and `MCP_SOURCE`.
3. Include `source` and `agent_label` when invoking `memory_save_checkpoint` and `memory_add_observations`.
4. Call `memory_save_checkpoint` when wrapping up a task or before session ends.
