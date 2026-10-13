# Shared Working Brain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Milestone 1 & 2 of Shared Working Brain: Multi-agent metadata attribution, active checkpoints with narrative breadcrumb trails, session bootstrap with device warning, project-scoped timeline, and tag/entity_type filtering.

**Architecture:** Extend existing Clean Architecture (`internal/{entity, repository, usecase, delivery/mcp}`). PostgreSQL schema migrations are idempotent via `schema.sql`. Business logic stays in usecase layer; DB mutations stay in repository layer with atomic transactions. MCP tools are exposed via `github.com/modelcontextprotocol/go-sdk/mcp`.

**Tech Stack:** Go 1.24, PostgreSQL 17 + pgvector, pgx/v5, Model Context Protocol Go SDK.

**Spec:** `docs/superpowers/specs/2026-10-13-shared-working-brain-design.md`

## Global Constraints

- Database migrations must remain fully idempotent in `internal/repository/schema.sql` (`IF NOT EXISTS`).
- All new MCP tool input fields must be optional pointers (`*string`, `[]string`) to guarantee backward-compatibility.
- All tests must run cleanly via `go test ./... -p 1`.
- Code comments and git commit messages in English.

## Review Focus

- Missing `agent_label` on bootstrap must return warning in payload without erroring out.
- `memory_save_checkpoint` with `replaceObs=true` must completely replace previous observations of `<project>:checkpoint:active` without deleting other entities.
- `memory_get_history` must reject or filter strictly by `project_id` so project events never leak across projects.
- `memory_session_bootstrap` must handle empty/fresh project gracefully (empty checkpoint, empty trail, no panic).
- Adding observations in `AddObservations` and `CreateEntities` must reliably log `observation_created` history records with correct `project_id`.

---

### Task 1: Database Schema & Entity Model Updates

**Files:**
- Modify: `internal/repository/schema.sql:1-154`
- Modify: `internal/entity/memory.go:1-120`
- Test: `internal/usecase/crud_test.go` (or `schema_test.go`)

**Interfaces:**
- Produces:
  - New entity types: `'task'`, `'checkpoint'`, `'checkpoint_trail'` in `memory_entity_types`.
  - Columns in `memory_observations`: `source TEXT`, `session_id TEXT`, `agent_label TEXT`, `deleted_at TIMESTAMPTZ`.
  - Columns in `memory_history`: `project_id TEXT NOT NULL DEFAULT 'default'`, `source TEXT`, `session_id TEXT`, `agent_label TEXT`.
  - Column in `memory_entities`: `tags TEXT[] DEFAULT '{}'`, with GIN index `idx_entities_tags`.
  - Entity structs updated: `entity.Observation`, `entity.HistoryEntry`, `entity.Entity`, `entity.Checkpoint`, `entity.SessionBootstrapResult`.

- [ ] **Step 1: Write tests verifying schema migration and struct fields**

Add test in `internal/repository/memory_test.go` or integration test asserting that `EnsureSchema` succeeds on a test DB and that table columns exist.

- [ ] **Step 2: Run test to verify it fails before schema edit**

Run: `go test ./internal/usecase/... -run TestSchema -v`

- [ ] **Step 3: Update `schema.sql` and `entity/memory.go`**

Apply SQL migrations in `schema.sql`:
- Seed types `task`, `checkpoint`, `checkpoint_trail`.
- Add columns to `memory_observations`, `memory_history`, `memory_entities`.
- Add index on `memory_history (project_id, happened_at DESC)`.
- Update Go structs in `internal/entity/memory.go` with json tags.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/entity/... ./internal/repository/... -v`

- [ ] **Step 5: Commit**

```bash
git add internal/repository/schema.sql internal/entity/memory.go
git commit -m "feat(schema): add multi-agent identity, tags, and checkpoint types"
```

---

### Task 2: Repository Layer - History Scoping, Creation Audit, and Timeline Query

**Files:**
- Modify: `internal/repository/memory.go:100-450`
- Test: `internal/usecase/versioning_test.go`

**Interfaces:**
- Consumes: Updated `entity.HistoryEntry` and schema columns.
- Produces:
  - `AddObservations` and `CreateEntities` write `observation_created` to `memory_history` with `project_id`, `source`, `agent_label`, `session_id`.
  - `GetHistory(ctx, project, entityName, limit)` filtered by `project_id = $1 AND entity_name = $2`.
  - `GetTimeline(ctx, project, since time.Time, limit int, entityName, source *string) ([]entity.HistoryEntry, error)`.
  - `CreateEntities` supports `replaceObs bool`.

- [ ] **Step 1: Write failing tests for history creation and timeline query**

In `internal/usecase/versioning_test.go`, test that adding observation logs `observation_created`, project-scoped `GetHistory` returns only current project's events, and `GetTimeline` returns events ordered by `happened_at DESC`.

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/usecase -run TestVersioning -v`

- [ ] **Step 3: Implement repository updates**

In `internal/repository/memory.go`:
- Thread `project` into all `memory_history` insert queries.
- Add `observation_created` write in `AddObservations` and `CreateEntities`.
- Add `replaceObs bool` parameter to `CreateEntities` (deletes existing observations if entity exists and `replaceObs` is true).
- Implement `GetTimeline(ctx, project string, since time.Time, limit int, entityName, source *string) ([]entity.HistoryEntry, error)`.
- Update `GetHistory` to filter by `project_id`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/usecase -run TestVersioning -v`

- [ ] **Step 5: Commit**

```bash
git add internal/repository/memory.go internal/usecase/versioning_test.go
git commit -m "feat(repo): add timeline query, project-scoped history, and creation audit"
```

---

### Task 3: Checkpoint & Session Bootstrap Repository & Usecase Logic

**Files:**
- Modify: `internal/usecase/memory.go`
- Create/Modify: `internal/usecase/checkpoint_test.go`
- Create/Modify: `internal/usecase/bootstrap_test.go`

**Interfaces:**
- Consumes: Repository methods `CreateEntities`, `AddObservations`, `GetTimeline`, and `GetHistory`.
- Produces:
  - `SaveCheckpoint(ctx context.Context, in entity.SaveCheckpointInput) (*entity.CheckpointResult, error)`
  - `SessionBootstrap(ctx context.Context, in entity.SessionBootstrapInput) (*entity.SessionBootstrapResult, error)`

- [ ] **Step 1: Write failing unit/integration tests for Checkpoint & Bootstrap**

In `internal/usecase/checkpoint_test.go` and `bootstrap_test.go`:
- Test saving a checkpoint overwrites active checkpoint observations and appends to trail.
- Test bootstrap returns warning when `agent_label` is empty.
- Test bootstrap returns active checkpoint, max 20 trail entries, recent decisions, open tasks, and logs `session_start` to history.

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/usecase -run "TestCheckpoint|TestBootstrap" -v`

- [ ] **Step 3: Implement usecase logic**

In `internal/usecase/memory.go`:
- Implement `SaveCheckpoint`: constructs `<project>:checkpoint:active` observations (focus, uncommitted, blockers, next_step, source, saved_at), calls repo with `replaceObs=true`, appends breadcrumb observation to `<project>:checkpoint:trail`, and logs `checkpoint_saved` to history.
- Implement `SessionBootstrap`: checks `agent_label` for warning, queries active checkpoint entity, queries trail entity (takes latest 20, newest first), queries open tasks (entity_type='task' without 'status: done'), queries recent decisions, queries top entities, logs `session_start` history event, and returns `SessionBootstrapResult`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/usecase -run "TestCheckpoint|TestBootstrap" -v`

- [ ] **Step 5: Commit**

```bash
git add internal/usecase/memory.go internal/usecase/checkpoint_test.go internal/usecase/bootstrap_test.go
git commit -m "feat(usecase): implement save checkpoint and session bootstrap"
```

---

### Task 4: Tags and Entity Types Filter in Search

**Files:**
- Modify: `internal/repository/memory.go`
- Modify: `internal/usecase/memory.go`
- Test: `internal/usecase/search_test.go` or `integration_test.go`

**Interfaces:**
- Consumes: `tags TEXT[]` on `memory_entities`.
- Produces:
  - `Search(ctx, query, project, limit, entityTypes []string, tagsAny []string)` filtering by `e.entity_type = ANY($x)` and `e.tags && $y`.

- [ ] **Step 1: Write failing test for entity_types and tags filter in search**

Test searching with `entity_types: ["decision"]` returns only decision entities; test `tags_any: ["backend"]` returns only tagged entities.

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/usecase -run TestSearchFilters -v`

- [ ] **Step 3: Implement repository and usecase search filters**

Update lexical and hybrid queries in `internal/repository/memory.go` to include dynamic `AND e.entity_type = ANY(...)` and `AND e.tags && ...`. Pass through usecase.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/usecase -run TestSearchFilters -v`

- [ ] **Step 5: Commit**

```bash
git add internal/repository/memory.go internal/usecase/memory.go
git commit -m "feat(search): add entity_types and tags_any filtering"
```

---

### Task 5: MCP Delivery Layer & Tool Registration

**Files:**
- Modify: `internal/delivery/mcp/inputs.go`
- Modify: `internal/delivery/mcp/handlers.go`
- Modify: `internal/delivery/mcp/server.go`
- Test: `internal/delivery/mcp/mcp_test.go` (or integration test)

**Interfaces:**
- Produces MCP Tools:
  - `memory_save_checkpoint`
  - `memory_session_bootstrap`
  - `memory_timeline`
  - Updates input schema of `memory_create_entities`, `memory_add_observations`, `memory_search`.

- [ ] **Step 1: Write failing test for new MCP tool handlers**

Test calling `memory_save_checkpoint`, `memory_session_bootstrap`, and `memory_timeline` via MCP tool handler.

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/delivery/mcp/... -v`

- [ ] **Step 3: Implement MCP inputs, handlers, and register tools in server**

In `internal/delivery/mcp/`:
- Add inputs: `SaveCheckpointInput`, `SessionBootstrapInput`, `TimelineInput`, updated `SearchInput`, `CreateEntitiesInput`.
- Add handlers: `handleSaveCheckpoint`, `handleSessionBootstrap`, `handleTimeline`, and updated handlers.
- In `server.go`: Register `memory_save_checkpoint`, `memory_session_bootstrap`, `memory_timeline`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/delivery/mcp/... -v`

- [ ] **Step 5: Commit**

```bash
git add internal/delivery/mcp/
git commit -m "feat(mcp): register save_checkpoint, session_bootstrap, and timeline tools"
```

---

### Task 6: Documentation & Agent Protocol Updates

**Files:**
- Modify: `AGENTS.md`
- Modify: `README.md`

- [ ] **Step 1: Update AGENTS.md & README.md**
Document the new protocol:
- Set `MCP_AGENT_LABEL` and `MCP_SOURCE` in machine env.
- Always call `memory_session_bootstrap` at session start.
- Handle bootstrap warning if `agent_label` is missing.
- Save checkpoint when completing session/tasks.
- Tool documentation for `memory_save_checkpoint`, `memory_session_bootstrap`, `memory_timeline`.

- [ ] **Step 2: Run full test suite verification**

Run: `go test ./... -p 1`
Run: `go build ./...`

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md README.md
git commit -m "docs: update agent protocol for shared working brain and bootstrap"
```
