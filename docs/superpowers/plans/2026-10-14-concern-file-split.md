# Concern-Based File Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split 8 oversized source files (and 3 test files) into concern-named files inside the same packages, with zero behavior change.

**Architecture:** Pure file-move refactor. No package, interface, signature, SQL, or error-handling changes. Each new file gets the same `package` clause as its source and only the imports its moved symbols need. Verifier per task: `go build ./... && go test ./...` green. Baseline before starting: 46 tests pass in 10 packages.

**Tech Stack:** Go, pgx, mark3labs/mcp-go. No new dependencies. `goimports` is NOT installed — fix imports manually; the compiler names missing/unused imports.

**Spec:** `docs/superpowers/specs/2026-10-14-concern-file-split-design.md`

**Global rules for every task:**
- Move symbol bodies byte-for-byte. Do not rename, reorder parameters, rewrap SQL, or "clean up" while moving.
- Line numbers below refer to the file state at the START of the task (capture them before the first edit if a prior task in the same file has run).
- Every new file starts with the correct `package` line, then blank line, then only needed imports.
- Delete nothing from tests except the function body when it moves to another test file; move test helpers only when the helper is used solely by moved tests and is defined in the file being split (`testhelper_test.go` and `ui_testhelper_test.go` are never touched).
- Commit exactly at the commit step listed; do not batch commits across tasks.

---

### Task 1: Split `internal/repository/memory.go` into concern files

**Files:**
- Modify: `internal/repository/memory.go` (1128 lines → shrinks to entity CRUD core)
- Create: `internal/repository/sql.go`
- Create: `internal/repository/search.go`
- Create: `internal/repository/history.go`
- Create: `internal/repository/graph.go`
- Create: `internal/repository/export.go`
- Create: `internal/repository/observations.go`
- Create: `internal/repository/relations.go`

- [ ] **Step 1: Record the current symbol inventory**

Run:

```bash
grep -n "^func \|^type " internal/repository/memory.go
```

Confirm it matches: `vecLiteral` :22, `MemoryRepository` :38, `postgresMemory` :63, `NewMemoryRepository` :68, `CreateEntities` :72, `AddObservations` :143, `CreateRelations` :199, `DeleteEntities` :247, `Search` :252, `searchLexical` :263, `searchHybrid` :319, `searchRow` :380, `fetchResults` :390, `TouchAccessed` :461, `GetHistory` :470, `GetTimeline` :499, `LogHistory` :547, `GetTopEntities` :565, `GetRecentEntitiesByType` :601, `ReadGraph` :655, `Export` :736, `Import` :819, `UpdateEntity` :893, `DeleteObservation` :940, `UpdateObservation` :969, `DeleteRelation` :1024, `DeleteObservationByContent` :1038, `UpdateObservationByContent` :1065, `DeleteRelationByTriple` :1113. If line numbers differ, use the grep output as truth.

- [ ] **Step 2: Create `internal/repository/sql.go`**

Move `vecLiteral` (from line 22 through the line before `MemoryRepository` interface) out of `memory.go` into this new file:

```go
package repository

import (
	"fmt"
	"strings"
)

func vecLiteral(v []float32) string {
	// body copied verbatim from memory.go
}
```

(Adjust the import block to exactly what the moved body needs — read the original imports first.)

- [ ] **Step 3: Create `internal/repository/search.go`**

`package repository`. Move, verbatim: `Search`, `searchLexical`, `searchHybrid`, `type searchRow`, `fetchResults`.

- [ ] **Step 4: Create `internal/repository/history.go`**

`package repository`. Move, verbatim: `TouchAccessed`, `GetHistory`, `GetTimeline`, `LogHistory`.

- [ ] **Step 5: Create `internal/repository/graph.go`**

`package repository`. Move, verbatim: `GetTopEntities`, `GetRecentEntitiesByType`, `ReadGraph`.

- [ ] **Step 6: Create `internal/repository/export.go`**

`package repository`. Move, verbatim: `Export`, `Import`.

- [ ] **Step 7: Create `internal/repository/observations.go`**

`package repository`. Move, verbatim: `DeleteObservation`, `UpdateObservation`, `DeleteObservationByContent`, `UpdateObservationByContent`.

- [ ] **Step 8: Create `internal/repository/relations.go`**

`package repository`. Move, verbatim: `DeleteRelation`, `DeleteRelationByTriple`.

- [ ] **Step 9: Shrink `internal/repository/memory.go` to the core**

After moves, `memory.go` must contain ONLY: file doc/imports as needed, `MemoryRepository` interface, `postgresMemory` struct, `NewMemoryRepository`, `CreateEntities`, `AddObservations`, `CreateRelations`, `DeleteEntities`, `UpdateEntity`.

- [ ] **Step 10: Fix imports until build passes**

Run:

```bash
go build ./internal/repository/
```

Expected: possible errors naming missing imports (`context`, `fmt`, `strings`, `time`, `github.com/jackc/pgx/v5`, `github.com/jackc/pgx/v5/pgxpool`, `github.com/fanajib5/mcp-memory-server-go/internal/entity`). Add exactly those imports to the file the compiler names. Re-run until:

```bash
go build ./...
go test ./...
```

Expected: `ok` for all packages, same as baseline.

- [ ] **Step 11: Commit**

```bash
git add internal/repository/
git commit -m "refactor(repository): split memory.go into concern-based files"
```

---

### Task 2: Split `internal/repository/stats.go` into concern files

**Files:**
- Modify: `internal/repository/stats.go` (355 lines → core + detail/list)
- Create: `internal/repository/dashboard.go`

- [ ] **Step 1: Record inventory**

Run: `grep -n "^func \|^type " internal/repository/stats.go`. Expected: `StatsRepository` :20, `postgresStats` :28, `NewStatsRepository` :33, `GetEntityDetail` :37, `ListEntities` :104, `DashboardMetrics` :138, `GraphData` :311, `ObservationByID` :348.

- [ ] **Step 2: Create `internal/repository/dashboard.go`**

`package repository`. Move, verbatim: `DashboardMetrics`, `GraphData`.

- [ ] **Step 3: Shrink `stats.go`**

`stats.go` keeps: `StatsRepository`, `postgresStats`, `NewStatsRepository`, `GetEntityDetail`, `ListEntities`, `ObservationByID`.

- [ ] **Step 4: Build + test**

Run: `go build ./... && go test ./...` — all green. Fix imports as in Task 1 Step 10.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/
git commit -m "refactor(repository): split stats.go into stats.go and dashboard.go"
```

---

### Task 3: Split `internal/entity/memory.go` into concern files

**Files:**
- Modify: `internal/entity/memory.go` (247 lines → core types)
- Create: `internal/entity/stats.go`
- Create: `internal/entity/checkpoint.go`
- Create: `internal/entity/export.go`

- [ ] **Step 1: Record inventory**

Run: `grep -n "^type " internal/entity/memory.go`. Expected types at: EntityInput :12, Attribution :25, RelationInput :32, Entity :39, SearchResult :50, FullGraph :61, ExportEntity :69, ExportRelation :75, ExportPayload :81, ImportResult :88, EntityDetailObservation :95, EntityDetailRelation :101, EntityDetail :108, EntitySummary :115, DayCount :123, TypeCount :129, Metrics :134, GraphNode :155, GraphEdge :161, GraphPayload :167, Observation :173, Checkpoint :186, SaveCheckpointInput :196, CheckpointResult :209, SessionBootstrapInput :214, SessionBootstrapResult :223, HistoryEntry :235.

- [ ] **Step 2: Create `internal/entity/stats.go`**

`package entity`. Move, verbatim: `EntityDetailObservation`, `EntityDetailRelation`, `EntityDetail`, `EntitySummary`, `DayCount`, `TypeCount`, `Metrics`, `GraphNode`, `GraphEdge`, `GraphPayload`.

- [ ] **Step 3: Create `internal/entity/checkpoint.go`**

`package entity`. Move, verbatim: `Checkpoint`, `SaveCheckpointInput`, `CheckpointResult`, `SessionBootstrapInput`, `SessionBootstrapResult`.

- [ ] **Step 4: Create `internal/entity/export.go`**

`package entity`. Move, verbatim: `FullGraph`, `ExportEntity`, `ExportRelation`, `ExportPayload`, `ImportResult`, `SearchResult`.

- [ ] **Step 5: Shrink `memory.go`**

`memory.go` keeps: `EntityInput`, `Attribution`, `RelationInput`, `Entity`, `Observation`, `HistoryEntry`.

- [ ] **Step 6: Build + test**

Run: `go build ./... && go test ./...` — all green. Types have no receivers; failures will be unused-import errors in files that no longer need `time` etc. Add/remove imports accordingly.

- [ ] **Step 7: Commit**

```bash
git add internal/entity/
git commit -m "refactor(entity): split memory.go into concern-based type files"
```

---

### Task 4: Split `internal/usecase/memory.go` into concern files

**Files:**
- Modify: `internal/usecase/memory.go` (514 lines → core + helpers)
- Create: `internal/usecase/crud.go`
- Create: `internal/usecase/search.go`
- Create: `internal/usecase/checkpoint.go`

- [ ] **Step 1: Record inventory**

Run: `grep -n "^func \|^type \|^const " internal/usecase/memory.go`. Expected: `embedTimeout` :24, `clampConfidence` :27, `clampConfidences` :39, `MemoryUseCase` :50, `NewMemoryUseCase` :59, `publish` :64, `embedTexts` :74, `CreateEntities` :88, `AddObservations` :107, `CreateRelations` :117, `DeleteEntities` :135, `Search` :150, `ReadGraph` :182, `Export` :187, `Import` :191, `UpdateEntity` :204, `DeleteObservation` :222, `UpdateObservation` :231, `DeleteRelation` :253, `DeleteObservationByContent` :262, `UpdateObservationByContent` :271, `DeleteRelationByTriple` :293, `GetHistory` :307, `GetTimeline` :312, `SaveCheckpoint` :317, `SessionBootstrap` :407.

NOTE: `crud_test.go` already exists; `crud.go` + `crud_test.go` coexist fine in Go.

- [ ] **Step 2: Create `internal/usecase/crud.go`**

`package usecase`. Move, verbatim: `CreateEntities`, `AddObservations`, `CreateRelations`, `DeleteEntities`, `UpdateEntity`, `DeleteObservation`, `UpdateObservation`, `DeleteRelation`, `DeleteObservationByContent`, `UpdateObservationByContent`, `DeleteRelationByTriple`.

- [ ] **Step 3: Create `internal/usecase/search.go`**

`package usecase`. Move, verbatim: `Search`, `ReadGraph`, `Export`, `Import`, `GetHistory`, `GetTimeline`.

- [ ] **Step 4: Create `internal/usecase/checkpoint.go`**

`package usecase`. Move, verbatim: `SaveCheckpoint`, `SessionBootstrap`.

- [ ] **Step 5: Shrink `memory.go`**

`memory.go` keeps: `embedTimeout`, `clampConfidence`, `clampConfidences`, `MemoryUseCase`, `NewMemoryUseCase`, `publish`, `embedTexts`.

- [ ] **Step 6: Build + test**

Run: `go build ./... && go test ./...` — all green. Fix imports.

- [ ] **Step 7: Commit**

```bash
git add internal/usecase/
git commit -m "refactor(usecase): split memory.go into concern-based files"
```

---

### Task 5: Split `internal/usecase/chat.go` into concern files

**Files:**
- Modify: `internal/usecase/chat.go` (543 lines → session + SendMessage)
- Create: `internal/usecase/chat_tools.go`
- Create: `internal/usecase/chat_execute.go`

- [ ] **Step 1: Record inventory**

Run: `grep -n "^func \|^type \|^const \|^var " internal/usecase/chat.go`. Expected: `Conversation` :21, `ChatUseCase` :28, `NewChatUseCase` :35, `sessionTTL` :45, `cleanupInterval` :46, `evictStaleSessions` :48, `entityTypeList` :61, `systemPromptTmpl` :66, `systemPrompt` :81, `toolDefinitions` :85, `getOrCreateConversation` :204, `SendMessage` :217, `executeTool` :277, `ClearConversation` :507, `GetHistory` :514, `ListModels` :536.

- [ ] **Step 2: Create `internal/usecase/chat_tools.go`**

`package usecase`. Move, verbatim: `entityTypeList`, `systemPromptTmpl`, `systemPrompt`, `toolDefinitions`.

- [ ] **Step 3: Create `internal/usecase/chat_execute.go`**

`package usecase`. Move, verbatim: `executeTool`.

- [ ] **Step 4: Shrink `chat.go`**

`chat.go` keeps: `Conversation`, `ChatUseCase`, `NewChatUseCase`, `sessionTTL`, `cleanupInterval`, `evictStaleSessions`, `getOrCreateConversation`, `SendMessage`, `ClearConversation`, `GetHistory`, `ListModels`.

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./...` — all green. Fix imports (the prompt template file needs only `strings`/`fmt` per its body).

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/
git commit -m "refactor(usecase): split chat.go into session, tools, and execute files"
```

---

### Task 6: Split usecase test files to mirror concerns

**Files:**
- Modify: `internal/usecase/versioning_test.go` (429 lines → deleted after move)
- Modify: `internal/usecase/bootstrap_test.go` (234 lines → renamed)
- Create: `internal/usecase/history_test.go`
- Create: `internal/usecase/timeline_test.go`
- Create: `internal/usecase/session_bootstrap_test.go`

- [ ] **Step 1: Create `internal/usecase/history_test.go`**

`package usecase`. Move, verbatim, from `versioning_test.go`: `TestHistoryObservationEdit`, `TestHistoryObservationDelete`, `TestHistoryEntityRename`, `TestHistoryEntityTypeChange`, `TestHistoryEmptyForUnchangedEdit`, `TestHistoryObservationCreatedLogging`, `TestHistoryAttributionPersistence`, `TestHistoryProjectIsolation`.

- [ ] **Step 2: Create `internal/usecase/timeline_test.go`**

`package usecase`. Move, verbatim: `TestGetTimeline`.

- [ ] **Step 2b: Append crud test to existing `internal/usecase/crud_test.go`**

Move, verbatim, from `versioning_test.go` into `crud_test.go` (which already exists — append the function, do not create a new file): `TestCreateEntitiesReplaceObs`.

- [ ] **Step 3: Delete `internal/usecase/versioning_test.go`**

After both moves the file must be empty of test functions. Remove the file. Any package-level test vars/helpers defined in it move to the file that uses them; if used by both new files, put the helper in `history_test.go` (single definition — Go forbids duplicates).

- [ ] **Step 4: Rename bootstrap tests to match source concern**

`git mv internal/usecase/bootstrap_test.go internal/usecase/session_bootstrap_test.go`. No body changes (`TestSessionBootstrapWarningWhenAgentLabelEmpty`, `TestSessionBootstrapFullPayload` already match `SessionBootstrap` in `checkpoint.go` concern; the spec names this file `session_bootstrap_test.go`).

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./internal/usecase/ -count=1` — all usecase tests pass, same names as before.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/
git commit -m "refactor(usecase): split versioning tests by concern"
```

---

### Task 7: Split `internal/gateway/llm.go` into concern files

**Files:**
- Modify: `internal/gateway/llm.go` (343 lines → types + client core)
- Create: `internal/gateway/chat.go`
- Create: `internal/gateway/stream.go`
- Create: `internal/gateway/models.go`

- [ ] **Step 1: Record inventory**

Run: `grep -n "^func \|^type " internal/gateway/llm.go`. Expected types :20–150 (`ToolParamSchema` … `StreamEvent`), `KiloGatewayClient` :150, `NewKiloGatewayClient` :157, `Chat` :172, `ChatStream` :232, `ListModels` :315.

- [ ] **Step 2: Create `internal/gateway/chat.go`**

`package gateway`. Move, verbatim: `Chat`.

- [ ] **Step 3: Create `internal/gateway/stream.go`**

`package gateway`. Move, verbatim: `ChatStream`.

- [ ] **Step 4: Create `internal/gateway/models.go`**

`package gateway`. Move, verbatim: `ListModels`.

- [ ] **Step 5: Shrink `llm.go`**

`llm.go` keeps all types (`ToolParamSchema` through `StreamEvent`), `LLMClient` interface, `KiloGatewayClient`, `NewKiloGatewayClient`.

- [ ] **Step 6: Build + test**

Run: `go build ./... && go test ./...` — green. Fix imports (request/response wire types like `chatRequest`, `chatResponse`, `streamChunk*`, `modelsResponse` stay in `llm.go`; if a moved method references an unexported type from `llm.go` that is fine — same package).

- [ ] **Step 7: Commit**

```bash
git add internal/gateway/
git commit -m "refactor(gateway): split llm.go into chat, stream, and models files"
```

---

### Task 8: Split `internal/delivery/http/oauth.go` and its test

**Files:**
- Modify: `internal/delivery/http/oauth.go` (229 lines → service core)
- Create: `internal/delivery/http/authorize.go`
- Create: `internal/delivery/http/token.go`
- Modify: `internal/delivery/http/oauth_test.go` (262 lines → shrinks)
- Create: `internal/delivery/http/authorize_test.go`
- Create: `internal/delivery/http/token_test.go`

- [ ] **Step 1: Record inventory**

Run: `grep -n "^func \|^type " internal/delivery/http/oauth.go`. Expected: `AuthCode` :22, `Claims` :30, `OAuthMetadata` :35, `OAuthService` :47, `NewOAuthService` :53, `randomState` :57, `HandleMetadata` :63, `HandleRegister` :79, `HandleAuthorize` :97, `HandleToken` :139, `tokenFromAuthCode` :150, `tokenFromClientCredentials` :183, `issueToken` :199, `parseBasicAuth` :220.

- [ ] **Step 2: Create `internal/delivery/http/authorize.go`**

`package http`. Move, verbatim: `HandleAuthorize`.

NOTE: package name of this directory — check with `head -1 internal/delivery/http/oauth.go`; it is NOT literally `http` if it collides with stdlib in that file's imports. Use the exact existing package clause.

- [ ] **Step 3: Create `internal/delivery/http/token.go`**

Same package clause as oauth.go. Move, verbatim: `HandleToken`, `tokenFromAuthCode`, `tokenFromClientCredentials`, `issueToken`.

- [ ] **Step 4: Shrink `oauth.go`**

Keeps: `AuthCode`, `Claims`, `OAuthMetadata`, `OAuthService`, `NewOAuthService`, `randomState`, `HandleMetadata`, `HandleRegister`, `parseBasicAuth`.

- [ ] **Step 5: Split the test file**

From `oauth_test.go`, move verbatim: `TestHandleOAuthAuthorize` → `authorize_test.go`; `TestHandleTokenAuthorizationCode` and `TestHandleTokenClientCredentials` → `token_test.go`. Both new files use the same package clause as `oauth_test.go`. Shared test helpers defined inside `oauth_test.go` and used by both destinations move to `oauth_test.go` (which retains `TestHandleOAuthMetadata`, `TestHandleRegister`). Delete moved bodies from `oauth_test.go`.

- [ ] **Step 6: Build + test**

Run: `go build ./... && go test ./internal/delivery/http/ -count=1` — all oauth tests pass with unchanged names.

- [ ] **Step 7: Commit**

```bash
git add internal/delivery/http/
git commit -m "refactor(delivery/http): split oauth into authorize and token files"
```

---

### Task 9: Split `internal/delivery/mcp/handlers.go` into concern files

**Files:**
- Modify: `internal/delivery/mcp/handlers.go` (226 lines → core + helpers)
- Create: `internal/delivery/mcp/handlers_memory.go`
- Create: `internal/delivery/mcp/handlers_checkpoint.go`

- [ ] **Step 1: Record inventory**

Run: `grep -n "^func " internal/delivery/mcp/handlers.go`. Expected: `Handlers` :21(type), `NewHandlers` :26, `textResult` :30, `jsonResult` :34, then handlers `handleCreateEntities` :42, `handleAddObservations` :68, `handleCreateRelations` :80, `handleDeleteEntities` :98, `handleSearch` :105, `handleReadGraph` :113, `handleExport` :121, `handleImport` :129, `handleRenameEntity` :138, `handleUpdateObservation` :145, `handleDeleteObservation` :152, `handleDeleteRelation` :159, `handleGetHistory` :166, `handleSaveCheckpoint` :174, `handleSessionBootstrap` :192, `handleTimeline` :206.

- [ ] **Step 2: Create `internal/delivery/mcp/handlers_memory.go`**

Same package clause. Move, verbatim: `handleCreateEntities`, `handleAddObservations`, `handleCreateRelations`, `handleDeleteEntities`, `handleSearch`, `handleReadGraph`, `handleExport`, `handleImport`, `handleRenameEntity`, `handleUpdateObservation`, `handleDeleteObservation`, `handleDeleteRelation`, `handleGetHistory`.

- [ ] **Step 3: Create `internal/delivery/mcp/handlers_checkpoint.go`**

Same package clause. Move, verbatim: `handleSaveCheckpoint`, `handleSessionBootstrap`, `handleTimeline`.

- [ ] **Step 4: Shrink `handlers.go`**

Keeps: `Handlers`, `NewHandlers`, `textResult`, `jsonResult`.

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./...` — green.

- [ ] **Step 6: Commit**

```bash
git add internal/delivery/mcp/
git commit -m "refactor(delivery/mcp): split handlers into memory and checkpoint files"
```

---

### Task 10: Final verification and graph update

**Files:** none modified.

- [ ] **Step 1: Full verification**

Run:

```bash
go build ./...
go vet ./...
go test ./... -count=1
```

Expected: build OK, vet clean, all packages `ok` (46 tests, same as baseline).

- [ ] **Step 2: Confirm no file exceeds 300 lines**

Run:

```bash
find internal -name "*.go" -not -name "*_test.go" | xargs wc -l | sort -rn | head -10
```

Expected: no non-test file above 300 lines.

- [ ] **Step 3: Update knowledge graph**

```bash
graphify update .
```

- [ ] **Step 4: Final commit if graph output changed**

```bash
git status --porcelain
# only commit graphify-out/ changes if the user's workflow tracks them (check gitignore first)
```

If `graphify-out/` is gitignored, skip the commit.
