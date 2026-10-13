# Design: Concern-Based File Split (Behavior-Preserving)

Date: 2026-10-14
Status: Approved
Scope: refactor file-level only — no package, API, signature, or import-path changes.

## Goal

Large files (up to 1128 lines) hold many unrelated concerns in one file. Split each
source file into cohesive, concern-named files inside the same package, and split
test files to mirror them. Behavior must not change.

## Constraints

- Same package for every split; no new packages, no sub-packages.
- Zero changes to exported API, method signatures, SQL, error handling, or data flow.
- Dedup is strict behavior-preserving: extract a shared helper only when logic is
  byte-for-byte identical. Never merge methods (e.g. `...ByContent` variants stay
  separate). Test assertions unchanged.
- Verifier per step: `go build ./... && go test ./...` must stay green.

## Source file mapping

| Source file | Resulting files (concern) |
|---|---|
| `internal/repository/memory.go` (1128) | `memory.go` (interface, struct, `New`, entity CRUD: CreateEntities, AddObservations, CreateRelations, DeleteEntities, UpdateEntity), `search.go` (Search, searchLexical, searchHybrid, searchRow, fetchResults), `history.go` (GetHistory, GetTimeline, LogHistory, TouchAccessed), `graph.go` (ReadGraph, GetTopEntities, GetRecentEntitiesByType), `export.go` (Export, Import), `observations.go` (all Delete/Update Observation by ID and by Content), `relations.go` (DeleteRelation, DeleteRelationByTriple), `sql.go` (`vecLiteral` and shared SQL helpers) |
| `internal/usecase/memory.go` (514) | `memory.go` (type, New, publish, embedTexts, clamp helpers), `crud.go` (CreateEntities, AddObservations, CreateRelations, DeleteEntities, UpdateEntity, Update/Delete Observation, Delete Relation, incl. ByContent/ByTriple variants), `search.go` (Search, ReadGraph, Export, Import, GetHistory, GetTimeline), `checkpoint.go` (SaveCheckpoint, SessionBootstrap) |
| `internal/usecase/chat.go` (543) | `chat.go` (Conversation, ChatUseCase, New, session store, SendMessage, ClearConversation, GetHistory, ListModels, evictStaleSessions), `tools.go` (systemPromptTmpl, systemPrompt, toolDefinitions, entityTypeList), `execute.go` (executeTool) |
| `internal/repository/stats.go` (355) | `stats.go` (interface, struct, New, GetEntityDetail, ListEntities, ObservationByID), `dashboard.go` (DashboardMetrics, GraphData) |
| `internal/gateway/llm.go` (343) | `llm.go` (all types, interface, struct, New), `chat.go` (Chat), `stream.go` (ChatStream), `models.go` (ListModels) |
| `internal/entity/memory.go` (247) | `memory.go` (EntityInput, Entity, Relation, Observation, HistoryEntry, Attribution), `stats.go` (Metrics, GraphNode/Edge/Payload, EntityDetail*, EntitySummary, DayCount, TypeCount), `checkpoint.go` (Checkpoint, SaveCheckpointInput, CheckpointResult, SessionBootstrapInput/Result), `export.go` (ExportPayload, ExportEntity, ExportRelation, ImportResult, FullGraph, SearchResult) |
| `internal/delivery/http/oauth.go` (229) | `oauth.go` (AuthCode, Claims, OAuthMetadata, OAuthService, New, randomState, HandleMetadata, HandleRegister, parseBasicAuth), `authorize.go` (HandleAuthorize), `token.go` (HandleToken, tokenFromAuthCode, tokenFromClientCredentials, issueToken) |
| `internal/delivery/mcp/handlers.go` (226) | `handlers.go` (Handlers, New, textResult, jsonResult), `handlers_memory.go` (CRUD/search/import/export handlers), `handlers_checkpoint.go` (SaveCheckpoint, SessionBootstrap, Timeline) |

## Test file mapping

Test splits mirror the source concerns. Existing test function bodies and assertions
are copied unchanged; only file placement changes.

| Source test | Resulting files |
|---|---|
| `internal/usecase/versioning_test.go` (429) | split each test into the concern file matching its subject: crud tests into `crud_test.go`, search/export tests into `search_test.go`, checkpoint/bootstrap tests into `checkpoint_test.go` / `session_bootstrap_test.go`. If the concern test file already exists, append the moved tests to it |
| `internal/usecase/bootstrap_test.go` (234) | `session_bootstrap_test.go` (bootstrap concern) |
| `internal/usecase/checkpoint_test.go` (229) | stays; may absorb checkpoint-concern tests from versioning_test if they belong |
| `internal/delivery/http/oauth_test.go` (262) | `oauth_test.go` (metadata/register), `authorize_test.go`, `token_test.go` mirroring source split |
| `internal/delivery/http/ui_integration_test.go` (241) | split only if it exceeds the target after oauth split; otherwise leave as-is (integration test, one concern: UI) |
| `internal/delivery/http/middleware_test.go` (224) | leave as-is (single concern: middleware) |
| `internal/usecase/testhelper_test.go` | untouched |

Note: `ui_integration_test.go` and `middleware_test.go` are single-concern integration /
middleware tests — splitting them has no cohesion benefit. They stay.

## Dedup policy

- Extract shared helper only when two code paths are identical (e.g. repeated SQL
  fragment builders). Helper goes in the same package file as its primary consumer
  or in `sql.go` for repository.
- No method merging. `DeleteObservationByContent` / `UpdateObservationByContent` /
  `DeleteRelationByTriple` remain separate methods even if they resemble by-ID paths.
- If a candidate dedup is ambiguous (even a one-line difference), skip it.

## Error handling / data flow

Unchanged. Every moved block keeps its original error paths, query text, and return
values.

## Verification

Per package after each split step:

```bash
go build ./...
go test ./...
```

Order: repository → entity → usecase → gateway → delivery. Commit per package
(or single commit if preferred at implementation time).
