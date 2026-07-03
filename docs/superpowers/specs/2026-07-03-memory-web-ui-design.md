# Memory Server Web UI — Design Spec

- **Date:** 2026-07-03
- **Status:** Approved — pending implementation plan
- **Owner:** Faiq Najib

## Goal

Add a lightweight, browser-based admin UI to `mcp-memory-server` (same binary, same port, same Postgres pool) for:

1. **Monitoring** — statistics/metrics dashboard over the knowledge graph.
2. **Browsing** — list/filter/search entities; view an entity's observations and relations.
3. **Editing** — full CRUD so the user can correct/augment memory directly from the browser.

The MCP API (`/mcp`) and OAuth endpoints are unchanged. The UI is an additional, **separately-authenticated** surface.

## Non-goals (YAGNI for v1)

- Multi-user accounts / RBAC (single shared password).
- CSRF synchronizer token (SameSite=Lax cookie is the v1 mitigation; documented as future hardening).
- Graph visualization (force-directed, etc.) — tables/lists/counts only.
- Pagination beyond a fixed LIMIT; infinite scroll.
- OAuth for the UI (UI uses password+cookie; `/mcp` keeps token/JWT).
- Exposing new CRUD operations as MCP tools (follow-up).

## Architecture

Same Go binary, same `PORT` (default 3000), same `*pgxpool.Pool`. New routes are registered on the existing `http.ServeMux`. Go 1.22+ ServeMux pattern routing is used for method-scoped routes and `{id}` wildcards (`DELETE /ui/observation/{id}` + `r.PathValue("id")`) — no external router needed (go.mod is 1.23).

New middleware `sessionAuth` gates `/ui/*` (except `/ui/login` and `/static`). `/mcp`, `/oauth/*`, `/.well-known/*`, `/health` are untouched and keep their existing auth.

Rendering: Go `html/template` + **htmx** (vendored JS) for partial updates. All templates and static assets are embedded via `go:embed`, so the binary stays single-file and distroless-static-compatible.

## Authentication — login form + stateless signed cookie

- `GET /ui/login` renders a form with a single **password** field (single-user personal tool; no username).
- `POST /ui/login`:
  - Read `password` form value.
  - Expected = `UI_PASSWORD` env; if empty, fallback to `MEMORY_API_TOKEN`.
  - Compare with `subtle.ConstantTimeCompare`. On success → set `mem_session` cookie, redirect `/ui`. On failure → re-render login with generic error (constant-time; tiny fixed delay to blunt guessing).
- **Cookie value (stateless, no server-side store):**
  `base64url(expiryUnix) || "." || base64url(HMAC-SHA256(secret, expiryUnix))`
  - `secret` reuses `jwtSecret` (already derived in `main` from `JWT_SECRET`/`MEMORY_API_TOKEN`).
  - Verify: split on `.`, recompute HMAC, `subtle.ConstantTimeCompare`, and check `expiry > now`.
- **Cookie attributes:** `HttpOnly`; `Secure` (unless `UI_COOKIE_INSECURE=true` for local http dev); `SameSite=Lax`; `Path=/ui`; `Max-Age` 30 days.
- `POST /ui/logout`: clear cookie (`Max-Age=0`), redirect to `/ui/login`.
- `sessionAuth`: allow `/ui/login` + `/static/*`; otherwise require a valid cookie, else `302` → `/ui/login`.

### CSRF

`SameSite=Lax` prevents the browser from sending the session cookie on cross-site POSTs, which blocks the dominant CSRF vector for state-changing routes. v1 ships this. A synchronizer CSRF token is a documented future hardening.

## Data-layer changes

**New file `crud.go`** (uses existing pool + `defaultProject`):

| Function | Purpose | Safety |
|---|---|---|
| `UpdateEntity(ctx, pool, project, oldName, newName, entityType)` | Rename and/or change type | FK validates type; reject if `(project,newName)` collides with an existing entity |
| `DeleteObservation(ctx, pool, project, id)` | Delete one observation by `memory_observations.id` | Verify the observation's entity belongs to `project` (JOIN check) |
| `UpdateObservation(ctx, pool, project, id, content)` | Replace observation text | Same ownership check |
| `DeleteRelation(ctx, pool, project, id)` | Delete one relation by `memory_relations.id` | Verify the `from`-entity belongs to `project` |

The project-ownership checks prevent a UI action scoped to project A from touching rows in project B.

**New in `stats.go`:**

- `GetEntityDetail(ctx, pool, project, name) (*EntityDetail, error)` — entity `{id,name,type}`, observations `[{id, content, created_at}]` (ordered), relations `[{id, type, otherName, direction}]` for both incoming and outgoing (`direction = "out"|"in"`).
- `ListEntities(ctx, pool, project, typeFilter, query string, limit) ([]EntitySummary, error)` — browse page; reuses `SearchMemory` when `query` is present, else a type/all listing; returns `{name, type, obsCount, relCount}`.
- `DashboardMetrics(ctx, pool, project) (*Metrics, error)` — the metric set below.

## Metrics set (dashboard, read-only)

Scoped to the selected project (plus a small "all projects" totals strip):

1. **Overview counts:** entities, observations, relations, distinct relation types.
2. **Distribution:** entities per `entity_type`.
3. **Graph health:** avg observations/entity, avg relations/entity, **orphan** entities (0 relations), **sparse** entities (0 observations).
4. **Top entities** by observation count (top 10); top by relation count — hubs (top 10).
5. **Growth:** entities + observations created per day, last 30 days (small bar/sparkline).
6. **Relation-type popularity** (top types).
7. **Recent activity:** latest 10 entities, latest 10 observations (by `created_at`).

All via SQL aggregations on existing columns (`created_at` exists on entities, observations, relations).

## UI pages & behavior

- **Dashboard** (`GET /ui`): metric cards + tables + 30-day growth bar; `hx-get` partial to refresh on project switch.
- **Entities** (`GET /ui/entities?type=&q=`): type-filter dropdown + search box; `hx-get` returns partial `<tr>` rows (no full reload). Columns: name, type, #obs, #rel. Row links to detail.
- **Entity detail** (`GET /ui/entity?name=&p=`): observations (each with inline edit + delete via htmx), relations in/out (each with delete), and forms to add observation, add relation (target = existing entity name + relation type), edit entity (name/type), and delete entity. Destructive deletes use a native `confirm()`.
- **Project selector**: dropdown in the layout; sets the `mem_project` cookie (non-sensitive); default `"default"`.

htmx is progressive enhancement: list filter/search returns partials; mutations return the refreshed region (e.g., deleting an observation returns the updated observation list). Pages degrade to plain forms without JS.

## File structure

```
stats.go      metric + detail + list queries (+ types)
crud.go       UpdateEntity, DeleteObservation, UpdateObservation, DeleteRelation
web.go        handlers, sessionAuth, login/logout, cookie sign/verify, template wiring
templates/    _layout.html, login.html, dashboard.html, entities.html, entity.html, _partials.html
static/       htmx.min.js (vendored, pinned), app.css
main.go       register UI routes, resolve UI_PASSWORD, go:embed(fs)
Dockerfile    add: COPY templates/ static/
.env.example  document UI_PASSWORD (fallback MEMORY_API_TOKEN), UI_COOKIE_INSECURE
```

## Security checklist

- HTTPS via Coolify TLS (cookie `Secure` valid in prod; `UI_COOKIE_INSECURE` for local http).
- Password compared with `subtle.ConstantTimeCompare`.
- Cookie `HttpOnly` + `Secure` + `SameSite=Lax` + `Path=/ui`.
- All mutations require a valid session cookie (`sessionAuth`).
- Cross-project safety: obs/relation mutations verify the row's entity belongs to the active project.
- All SQL parameterized (`$1…`) — no injection.
- Binary remains distroless static (no shell) — attack surface unchanged.

## Testing

- `crud_test.go` (integration, throwaway DB, existing pattern): create entity → `UpdateEntity` (rename + retype) → add observation → `UpdateObservation` → `DeleteObservation` → add relation → `DeleteRelation`; assert intermediate states and that cross-project mutations are rejected.
- `web_test.go`: cookie sign/verify round-trip; tampered and expired cookies rejected; `sessionAuth` redirects unauthenticated requests to `/ui/login`; wrong password sets no cookie.
- `go vet ./...` + `go build ./...`.
- Manual smoke after deploy (`/ui` login, dashboard, browse, one create/edit/delete of each kind).

## Environment variables

- `UI_PASSWORD` (optional) — login password. Fallback: `MEMORY_API_TOKEN`. **Fail-closed:** if neither is set, the server logs a warning and does not register UI routes (no open access).
- `UI_COOKIE_INSECURE` (optional, `true` for local http dev) — omit the cookie `Secure` flag.
- Cookie HMAC secret reuses `jwtSecret`.

## Deployment

1. `git push` → Coolify rebuilds (Dockerfile now COPYs `templates/` and `static/`).
2. Set `UI_PASSWORD` in Coolify env (optional — falls back to `MEMORY_API_TOKEN`).
3. Access `https://memory.najib.id/ui`.

## Follow-ups (out of v1)

- Expose `DeleteObservation` / `DeleteRelation` / `UpdateEntity` / `UpdateObservation` as MCP tools for parity.
- CSRF synchronizer token.
- Login brute-force throttle / lockout.
- Graph visualization.
