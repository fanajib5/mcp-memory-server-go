# Memory Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a browser admin UI (dashboard + browse + CRUD) to `mcp-memory-server`, in the same Go binary, behind login-form cookie auth.

**Architecture:** New routes on the existing `http.ServeMux` (Go 1.22+ method patterns). New `sessionAuth` middleware (HMAC-signed cookie) gates `/ui/*`; `/mcp` and OAuth are untouched. Go `html/template` + htmx + `go:embed` keep it a single static binary. New granular edit ops live in `crud.go`; read/metric queries in `stats.go`; all HTTP UI in `web.go`.

**Tech Stack:** Go 1.23 (stdlib `net/http`, `html/template`, `crypto/hmac`, `crypto/sha256`, `crypto/subtle`, `embed`), pgx/v5, htmx 2.0.4 (vendored JS). Zero new Go dependencies.

## Global Constraints

- Package is `main`. **Reuse existing package globals** `pool *pgxpool.Pool` and `jwtSecret string` (defined in `db.go`/`main.go`) — do not redeclare.
- **Reuse existing helpers** in `db.go`: `defaultProject(p)`, `normalizeEntityType(s)`, `normalizeRelationType(s)`.
- Registered entity types (FK-enforced): `project, person, decision, tool, concept, place`. Nothing else.
- All SQL parameterized (`$1…`). No string interpolation of user input into SQL.
- Mutations on observations/relations MUST verify the row's entity belongs to the active project (cross-project safety).
- **Tests need Postgres.** Local postgres on `localhost:5432` (user `postgres`, pass `root`) is available. Use a throwaway DB per run. Create+drop:
  ```bash
  PGPASSWORD=root psql -h localhost -p 5432 -U postgres -d postgres -c "DROP DATABASE IF EXISTS memory_web_test; CREATE DATABASE memory_web_test OWNER postgres;"
  ```
  Run DB tests with `DATABASE_URL="postgres://postgres:root@localhost:5432/memory_web_test" go test -run <Name> -v ./...`. Integration tests auto-skip if `DATABASE_URL` unset (existing `integrationPool` helper).
- TDD: failing test → run (fail) → implement → run (pass) → commit, per task.
- Commit message prefix `feat(ui):` for this feature.

## File Structure

```
crud.go         NEW  granular edit ops (UpdateEntity, DeleteObservation, UpdateObservation, DeleteRelation)
crud_test.go    NEW  integration tests for the above
stats.go        NEW  read/metric queries + types (GetEntityDetail, ListEntities, DashboardMetrics)
stats_test.go   NEW  integration tests for the above
web.go          NEW  cookie sign/verify, sessionAuth, login/logout, all UI handlers, render helpers, embeds
web_test.go     NEW  unit tests for cookie + sessionAuth (no DB)
templates/      NEW  _partials.html, login.html, dashboard.html, entities.html, entity.html, _fragments.html
static/         NEW  htmx.min.js (vendored 2.0.4), app.css
main.go         MOD  parse UI env, register UI routes
Dockerfile      MOD  COPY templates/ static/
.env.example    MOD  document UI_PASSWORD, UI_COOKIE_INSECURE
```

---

### Task 1: Granular CRUD data layer (`crud.go`)

**Files:**
- Create: `crud.go`
- Create: `crud_test.go`

**Interfaces:**
- Consumes: package globals `pool` (via param), helpers `defaultProject`, `normalizeEntityType` from `db.go`; `integrationPool(t)` from `integration_test.go`; existing `CreateEntities`/`CreateRelations`/`AddObservations` from `db.go` (for test setup).
- Produces (used by Task 5 handlers):
  - `UpdateEntity(ctx context.Context, pool *pgxpool.Pool, project, oldName, newName, entityType string) error`
  - `DeleteObservation(ctx context.Context, pool *pgxpool.Pool, project string, id int) error`
  - `UpdateObservation(ctx context.Context, pool *pgxpool.Pool, project string, id int, content string) error`
  - `DeleteRelation(ctx context.Context, pool *pgxpool.Pool, project string, id int) error`

- [ ] **Step 1: Write the failing test** — create `crud_test.go`:

```go
package main

import (
	"context"
	"testing"
)

func TestCRUDOperations(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Setup: entity with an observation and a relation to another entity.
	if _, err := CreateEntities(ctx, pool, "crudproj", []EntityInput{
		{Name: "Alpha", EntityType: "project", Observations: []string{"obs one"}},
		{Name: "Beta", EntityType: "concept"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := CreateRelations(ctx, pool, "crudproj", []RelationInput{
		{From: "Alpha", To: "Beta", RelationType: "depends on"},
	}); err != nil {
		t.Fatalf("rel: %v", err)
	}
	detail, err := GetEntityDetail(ctx, pool, "crudproj", "Alpha")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	obsID := detail.Observations[0].ID
	relID := detail.Relations[0].ID

	// UpdateEntity: rename + retype.
	if err := UpdateEntity(ctx, pool, "crudproj", "Alpha", "AlphaRenamed", "tool"); err != nil {
		t.Fatalf("update entity: %v", err)
	}
	d2, _ := GetEntityDetail(ctx, pool, "crudproj", "AlphaRenamed")
	if d2 == nil || d2.Type != "tool" {
		t.Fatalf("after update, got %+v", d2)
	}

	// Rename collision is rejected.
	if err := UpdateEntity(ctx, pool, "crudproj", "AlphaRenamed", "Beta", "tool"); err == nil {
		t.Fatal("expected collision error, got nil")
	}

	// UpdateObservation.
	if err := UpdateObservation(ctx, pool, "crudproj", obsID, "edited obs"); err != nil {
		t.Fatalf("update obs: %v", err)
	}
	d3, _ := GetEntityDetail(ctx, pool, "crudproj", "AlphaRenamed")
	if d3.Observations[0].Content != "edited obs" {
		t.Fatalf("obs content = %q", d3.Observations[0].Content)
	}

	// DeleteObservation.
	if err := DeleteObservation(ctx, pool, "crudproj", obsID); err != nil {
		t.Fatalf("delete obs: %v", err)
	}
	d4, _ := GetEntityDetail(ctx, pool, "crudproj", "AlphaRenamed")
	if len(d4.Observations) != 0 {
		t.Fatalf("obs not deleted: %+v", d4.Observations)
	}

	// DeleteRelation.
	if err := DeleteRelation(ctx, pool, "crudproj", relID); err != nil {
		t.Fatalf("delete rel: %v", err)
	}
	d5, _ := GetEntityDetail(ctx, pool, "crudproj", "AlphaRenamed")
	if len(d5.Relations) != 0 {
		t.Fatalf("rel not deleted: %+v", d5.Relations)
	}

	// Cross-project safety: DeleteObservation from another project must fail.
	if _, err := CreateEntities(ctx, pool, "otherproj", []EntityInput{
		{Name: "Gamma", EntityType: "tool", Observations: []string{"other obs"}},
	}); err != nil {
		t.Fatalf("create gamma: %v", err)
	}
	g, _ := GetEntityDetail(ctx, pool, "otherproj", "Gamma")
	otherObsID := g.Observations[0].ID
	if err := DeleteObservation(ctx, pool, "crudproj", otherObsID); err == nil {
		t.Fatal("cross-project observation delete must fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
PGPASSWORD=root psql -h localhost -p 5432 -U postgres -d postgres -c "DROP DATABASE IF EXISTS memory_web_test; CREATE DATABASE memory_web_test OWNER postgres;"
DATABASE_URL="postgres://postgres:root@localhost:5432/memory_web_test" go test -run TestCRUDOperations -v ./...
```
Expected: FAIL — `UpdateEntity` (and `GetEntityDetail`) undefined. (GetEntityDetail is implemented in Task 2; for this task, also add a minimal stub? No — Task 1 depends on GetEntityDetail from Task 2. **Run order: implement Task 2's GetEntityDetail first, OR temporarily inline a lookup.** Per dependency, do Task 2 before Task 1's verification. Simpler: implement Task 2 first.) **Reorder: execute Task 2 before Task 1.** See Task 2.

- [ ] **Step 3: Implement `crud.go`** (depends on `GetEntityDetail` from Task 2 being present):

```go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UpdateEntity renames and/or changes the type of an entity within a project.
// Rejects rename if the new name already exists in the same project.
func UpdateEntity(ctx context.Context, pool *pgxpool.Pool, project, oldName, newName, entityType string) error {
	project = defaultProject(project)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("entity name is required")
	}
	et := normalizeEntityType(entityType)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var id int
	err = tx.QueryRow(ctx,
		`SELECT id FROM memory_entities WHERE project_id = $1 AND name = $2`, project, oldName,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("entity %q not found in project %q: %w", oldName, project, err)
	}

	if newName != oldName {
		var collision int
		err = tx.QueryRow(ctx,
			`SELECT 1 FROM memory_entities WHERE project_id = $1 AND name = $2 AND id <> $3`,
			project, newName, id,
		).Scan(&collision)
		if err == nil {
			return fmt.Errorf("name %q already exists in project %q", newName, project)
		}
		// err != nil here means no collision row — expected, proceed.
	}

	if _, err := tx.Exec(ctx,
		`UPDATE memory_entities SET name = $2, entity_type = $3 WHERE id = $1`, id, newName, et,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteObservation deletes one observation by id, scoped to project.
func DeleteObservation(ctx context.Context, pool *pgxpool.Pool, project string, id int) error {
	project = defaultProject(project)
	tag, err := pool.Exec(ctx, `
		DELETE FROM memory_observations o
		USING memory_entities e
		WHERE o.entity_id = e.id AND e.project_id = $1 AND o.id = $2`, project, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation not found in project %q", project)
	}
	return nil
}

// UpdateObservation replaces an observation's content, scoped to project.
func UpdateObservation(ctx context.Context, pool *pgxpool.Pool, project string, id int, content string) error {
	project = defaultProject(project)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("observation content is required")
	}
	tag, err := pool.Exec(ctx, `
		UPDATE memory_observations o SET content = $3
		FROM memory_entities e
		WHERE o.entity_id = e.id AND e.project_id = $1 AND o.id = $2`, project, id, content)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("observation not found in project %q", project)
	}
	return nil
}

// DeleteRelation deletes one relation by id, scoped to the from-entity's project.
func DeleteRelation(ctx context.Context, pool *pgxpool.Pool, project string, id int) error {
	project = defaultProject(project)
	tag, err := pool.Exec(ctx, `
		DELETE FROM memory_relations r
		USING memory_entities fe
		WHERE r.from_entity_id = fe.id AND fe.project_id = $1 AND r.id = $2`, project, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("relation not found in project %q", project)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify pass** (after Task 2 implemented so `GetEntityDetail` exists):

```bash
DATABASE_URL="postgres://postgres:root@localhost:5432/memory_web_test" go test -run 'TestCRUDOperations|TestStatsQueries' -v ./...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add crud.go crud_test.go
git commit -m "feat(ui): add granular CRUD data layer (update entity, delete/update observation, delete relation)"
```

---

### Task 2: Read & metric queries (`stats.go`)

**Files:**
- Create: `stats.go`
- Create: `stats_test.go`

**Interfaces:**
- Consumes: `defaultProject`, `SearchMemory` (optional) from `db.go`; `integrationPool`.
- Produces (used by Task 1 test and Task 5 handlers):
  - Types: `EntityDetail`, `EntityDetailObservation`, `EntityDetailRelation`, `EntitySummary`, `Metrics`, `DayCount`, `TypeCount`
  - `GetEntityDetail(ctx, pool, project, name string) (*EntityDetail, error)`
  - `ListEntities(ctx, pool, project, typeFilter, query string, limit int) ([]EntitySummary, error)`
  - `DashboardMetrics(ctx, pool, project string) (*Metrics, error)`

- [ ] **Step 1: Write the failing test** — create `stats_test.go`:

```go
package main

import (
	"context"
	"testing"
)

func TestStatsQueries(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	CreateEntities(ctx, pool, "statsproj", []EntityInput{
		{Name: "Hub", EntityType: "project", Observations: []string{"a", "b"}},
		{Name: "Leaf", EntityType: "concept"},
	})
	CreateRelations(ctx, pool, "statsproj", []RelationInput{{From: "Hub", To: "Leaf", RelationType: "uses"}})

	// GetEntityDetail
	d, err := GetEntityDetail(ctx, pool, "statsproj", "Hub")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(d.Observations) != 2 {
		t.Fatalf("obs count = %d", len(d.Observations))
	}
	if d.Observations[0].ID == 0 {
		t.Fatal("observation ID not populated")
	}
	if len(d.Relations) != 1 || d.Relations[0].Other != "Leaf" || d.Relations[0].Direction != "out" {
		t.Fatalf("relations = %+v", d.Relations)
	}

	// Incoming relation on Leaf
	dl, _ := GetEntityDetail(ctx, pool, "statsproj", "Leaf")
	if len(dl.Relations) != 1 || dl.Relations[0].Direction != "in" || dl.Relations[0].Other != "Hub" {
		t.Fatalf("incoming relations = %+v", dl.Relations)
	}

	// ListEntities with type filter + search
	rows, err := ListEntities(ctx, pool, "statsproj", "project", "", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Hub" || rows[0].ObsCount != 2 || rows[0].RelCount != 1 {
		t.Fatalf("list rows = %+v", rows)
	}
	rows2, _ := ListEntities(ctx, pool, "statsproj", "", "hub", 50)
	if len(rows2) != 1 {
		t.Fatalf("search rows = %+v", rows2)
	}

	// DashboardMetrics
	m, err := DashboardMetrics(ctx, pool, "statsproj")
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if m.Entities != 2 || m.Observations != 2 || m.Relations != 1 {
		t.Fatalf("counts = %+v", m)
	}
	if m.Orphans != 1 { // Leaf has no outgoing relation
		t.Fatalf("orphans = %d", m.Orphans)
	}
	if m.Sparse != 1 { // Leaf has no observations
		t.Fatalf("sparse = %d", m.Sparse)
	}
	if len(m.TopByObs) == 0 || m.TopByObs[0].Name != "Hub" {
		t.Fatalf("topByObs = %+v", m.TopByObs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
PGPASSWORD=root psql -h localhost -p 5432 -U postgres -d postgres -c "DROP DATABASE IF EXISTS memory_web_test; CREATE DATABASE memory_web_test OWNER postgres;"
DATABASE_URL="postgres://postgres:root@localhost:5432/memory_web_test" go test -run TestStatsQueries -v ./...
```
Expected: FAIL — `GetEntityDetail` undefined.

- [ ] **Step 3: Implement `stats.go`**:

```go
package main

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type EntityDetailObservation struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
}

type EntityDetailRelation struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Other     string `json:"other"`
	Direction string `json:"direction"` // "out" or "in"
}

type EntityDetail struct {
	Name         string                    `json:"name"`
	Type         string                    `json:"type"`
	Observations []EntityDetailObservation `json:"observations"`
	Relations    []EntityDetailRelation    `json:"relations"`
}

type EntitySummary struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	ObsCount int    `json:"obsCount"`
	RelCount int    `json:"relCount"`
}

type DayCount struct {
	Day          string `json:"day"`
	Entities     int    `json:"entities"`
	Observations int    `json:"observations"`
}

type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type Metrics struct {
	Entities       int            `json:"entities"`
	Observations   int            `json:"observations"`
	Relations      int            `json:"relations"`
	RelationTypes  int            `json:"relationTypes"`
	AllEntities    int            `json:"allEntities"`
	AllObservations int           `json:"allObservations"`
	ByType         []TypeCount    `json:"byType"`
	AvgObs         float64        `json:"avgObs"`
	AvgRel         float64        `json:"avgRel"`
	Orphans        int            `json:"orphans"`
	Sparse         int            `json:"sparse"`
	TopByObs       []EntitySummary `json:"topByObs"`
	TopByRel       []EntitySummary `json:"topByRel"`
	Growth         []DayCount     `json:"growth"`
	RelationTypes_ []TypeCount    `json:"relationTypeCounts"`
	RecentEntities []EntitySummary `json:"recentEntities"`
}

func GetEntityDetail(ctx context.Context, pool *pgxpool.Pool, project, name string) (*EntityDetail, error) {
	project = defaultProject(project)
	d := &EntityDetail{}
	err := pool.QueryRow(ctx,
		`SELECT name, entity_type FROM memory_entities WHERE project_id = $1 AND name = $2`, project, name,
	).Scan(&d.Name, &d.Type)
	if err != nil {
		return nil, err
	}

	oRows, err := pool.Query(ctx, `
		SELECT o.id, o.content FROM memory_observations o
		JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND e.name = $2 ORDER BY o.created_at`, project, name)
	if err != nil {
		return nil, err
	}
	for oRows.Next() {
		var o EntityDetailObservation
		if err := oRows.Scan(&o.ID, &o.Content); err != nil {
			oRows.Close()
			return nil, err
		}
		d.Observations = append(d.Observations, o)
	}
	oRows.Close()

	outRows, err := pool.Query(ctx, `
		SELECT r.id, r.relation_type, te.name FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		JOIN memory_entities te ON te.id = r.to_entity_id
		WHERE fe.project_id = $1 AND fe.name = $2`, project, name)
	if err != nil {
		return nil, err
	}
	for outRows.Next() {
		var rel EntityDetailRelation
		if err := outRows.Scan(&rel.ID, &rel.Type, &rel.Other); err != nil {
			outRows.Close()
			return nil, err
		}
		rel.Direction = "out"
		d.Relations = append(d.Relations, rel)
	}
	outRows.Close()

	inRows, err := pool.Query(ctx, `
		SELECT r.id, r.relation_type, fe.name FROM memory_relations r
		JOIN memory_entities fe ON fe.id = r.from_entity_id
		JOIN memory_entities te ON te.id = r.to_entity_id
		WHERE te.project_id = $1 AND te.name = $2`, project, name)
	if err != nil {
		return nil, err
	}
	for inRows.Next() {
		var rel EntityDetailRelation
		if err := inRows.Scan(&rel.ID, &rel.Type, &rel.Other); err != nil {
			inRows.Close()
			return nil, err
		}
		rel.Direction = "in"
		d.Relations = append(d.Relations, rel)
	}
	inRows.Close()

	return d, nil
}

func ListEntities(ctx context.Context, pool *pgxpool.Pool, project, typeFilter, query string, limit int) ([]EntitySummary, error) {
	project = defaultProject(project)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		SELECT e.name, e.entity_type,
		       COUNT(DISTINCT o.id) AS obs, COUNT(DISTINCT r.id) AS rel
		FROM memory_entities e
		LEFT JOIN memory_observations o ON o.entity_id = e.id
		LEFT JOIN memory_relations r ON r.from_entity_id = e.id
		WHERE e.project_id = $1
		  AND ($2 = '' OR e.entity_type = $2)
		  AND ($3 = '' OR e.name ILIKE '%' || $3 || '%')
		GROUP BY e.id, e.name, e.entity_type
		ORDER BY e.name
		LIMIT $4`, project, typeFilter, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntitySummary
	for rows.Next() {
		var s EntitySummary
		if err := rows.Scan(&s.Name, &s.Type, &s.ObsCount, &s.RelCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func DashboardMetrics(ctx context.Context, pool *pgxpool.Pool, project string) (*Metrics, error) {
	project = defaultProject(project)
	m := &Metrics{}

	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM memory_entities WHERE project_id = $1),
		  (SELECT COUNT(*) FROM memory_observations o JOIN memory_entities e ON e.id = o.entity_id WHERE e.project_id = $1),
		  (SELECT COUNT(*) FROM memory_relations r JOIN memory_entities e ON e.id = r.from_entity_id WHERE e.project_id = $1),
		  (SELECT COUNT(DISTINCT r.relation_type) FROM memory_relations r JOIN memory_entities e ON e.id = r.from_entity_id WHERE e.project_id = $1),
		  (SELECT COUNT(*) FROM memory_entities),
		  (SELECT COUNT(*) FROM memory_observations)`, project,
	).Scan(&m.Entities, &m.Observations, &m.Relations, &m.RelationTypes, &m.AllEntities, &m.AllObservations); err != nil {
		return nil, err
	}
	if m.Entities > 0 {
		m.AvgObs = float64(m.Observations) / float64(m.Entities)
		m.AvgRel = float64(m.Relations) / float64(m.Entities)
	}

	// byType
	r, err := pool.Query(ctx, `SELECT entity_type, COUNT(*) FROM memory_entities WHERE project_id = $1 GROUP BY entity_type ORDER BY COUNT(*) DESC`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var tc TypeCount
		if err := r.Scan(&tc.Type, &tc.Count); err != nil {
			r.Close()
			return nil, err
		}
		m.ByType = append(m.ByType, tc)
	}
	r.Close()

	// orphans (no outgoing AND no incoming relation)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM memory_entities e
		WHERE e.project_id = $1
		  AND NOT EXISTS (SELECT 1 FROM memory_relations r WHERE r.from_entity_id = e.id)
		  AND NOT EXISTS (SELECT 1 FROM memory_relations r WHERE r.to_entity_id = e.id)`, project,
	).Scan(&m.Orphans); err != nil {
		return nil, err
	}
	// sparse (no observations)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM memory_entities e
		WHERE e.project_id = $1
		  AND NOT EXISTS (SELECT 1 FROM memory_observations o WHERE o.entity_id = e.id)`, project,
	).Scan(&m.Sparse); err != nil {
		return nil, err
	}

	// topByObs
	r, err = pool.Query(ctx, `
		SELECT e.name, e.entity_type, COUNT(o.id) c FROM memory_entities e
		LEFT JOIN memory_observations o ON o.entity_id = e.id
		WHERE e.project_id = $1
		GROUP BY e.id, e.name, e.entity_type
		ORDER BY c DESC, e.name LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var s EntitySummary
		if err := r.Scan(&s.Name, &s.Type, &s.ObsCount); err != nil {
			r.Close()
			return nil, err
		}
		m.TopByObs = append(m.TopByObs, s)
	}
	r.Close()

	// topByRel (degree = out + in)
	r, err = pool.Query(ctx, `
		SELECT e.name, e.entity_type, x.cnt FROM memory_entities e
		JOIN LATERAL (
		  SELECT ((SELECT COUNT(*) FROM memory_relations r WHERE r.from_entity_id = e.id)
		        + (SELECT COUNT(*) FROM memory_relations r WHERE r.to_entity_id = e.id)) AS cnt
		) x ON true
		WHERE e.project_id = $1
		ORDER BY x.cnt DESC, e.name LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var s EntitySummary
		if err := r.Scan(&s.Name, &s.Type, &s.RelCount); err != nil {
			r.Close()
			return nil, err
		}
		m.TopByRel = append(m.TopByRel, s)
	}
	r.Close()

	// relationTypeCounts
	r, err = pool.Query(ctx, `
		SELECT r.relation_type, COUNT(*) FROM memory_relations r
		JOIN memory_entities e ON e.id = r.from_entity_id
		WHERE e.project_id = $1 GROUP BY r.relation_type ORDER BY COUNT(*) DESC LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var tc TypeCount
		if err := r.Scan(&tc.Type, &tc.Count); err != nil {
			r.Close()
			return nil, err
		}
		m.RelationTypes_ = append(m.RelationTypes_, tc)
	}
	r.Close()

	// recentEntities
	r, err = pool.Query(ctx, `
		SELECT e.name, e.entity_type FROM memory_entities e
		WHERE e.project_id = $1 ORDER BY e.created_at DESC LIMIT 10`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var s EntitySummary
		if err := r.Scan(&s.Name, &s.Type); err != nil {
			r.Close()
			return nil, err
		}
		m.RecentEntities = append(m.RecentEntities, s)
	}
	r.Close()

	// growth: last 30 days (entities + observations per day)
	entByDay := map[string]int{}
	r, err = pool.Query(ctx, `
		SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') d, COUNT(*)
		FROM memory_entities WHERE project_id = $1 AND created_at >= now() - interval '30 day'
		GROUP BY 1`, project)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var d string
		var c int
		if err := r.Scan(&d, &c); err != nil {
			r.Close()
			return nil, err
		}
		entByDay[d] = c
	}
	r.Close()
	r, err = pool.Query(ctx, `
		SELECT to_char(date_trunc('day', o.created_at), 'YYYY-MM-DD') d, COUNT(*)
		FROM memory_observations o JOIN memory_entities e ON e.id = o.entity_id
		WHERE e.project_id = $1 AND o.created_at >= now() - interval '30 day'
		GROUP BY 1`, project)
	if err != nil {
		return nil, err
	}
	obsByDay := map[string]int{}
	for r.Next() {
		var d string
		var c int
		if err := r.Scan(&d, &c); err != nil {
			r.Close()
			return nil, err
		}
		obsByDay[d] = c
	}
	r.Close()
	allDays := map[string]bool{}
	for d := range entByDay {
		allDays[d] = true
	}
	for d := range obsByDay {
		allDays[d] = true
	}
	for d := range allDays {
		m.Growth = append(m.Growth, DayCount{Day: d, Entities: entByDay[d], Observations: obsByDay[d]})
	}

	return m, nil
}
```

- [ ] **Step 4: Run test to verify pass**

```bash
DATABASE_URL="postgres://postgres:root@localhost:5432/memory_web_test" go test -run TestStatsQueries -v ./...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add stats.go stats_test.go
git commit -m "feat(ui): add read/metric queries (entity detail, list, dashboard metrics)"
```

---

### Task 3: Templates, static assets, embed + render helper

**Files:**
- Create: `templates/_partials.html`, `templates/login.html`, `templates/dashboard.html`, `templates/entities.html`, `templates/entity.html`, `templates/_fragments.html`
- Create: `static/app.css`
- Download: `static/htmx.min.js`
- Create: `assets.go` (embed + template parse + `render`/`renderFragment`)

**Interfaces:**
- Produces (used by Task 4 & 5):
  - `var tmpl *template.Template` (parsed at init)
  - `initTemplates()` — call once at startup
  - `render(w http.ResponseWriter, page string, data any)` — executes a full-page template named `page`
  - `renderFragment(w http.ResponseWriter, name string, data any)` — executes a standalone fragment (no layout)
  - `staticFS` served at `/static/`

- [ ] **Step 1: Create `assets.go`**:

```go
package main

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var tmpl *template.Template

func initTemplates() {
	tmpl = template.Must(template.New("").Funcs(template.FuncMap{
		"truncate": func(s string, n int) string {
			if len(s) > n {
				return s[:n] + "…"
			}
			return s
		},
	}).ParseFS(templateFS, "templates/*.html"))
}

// render executes a full-page template (each page pulls in shared head/nav partials).
func render(w http.ResponseWriter, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, page, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderFragment executes a standalone fragment (for htmx responses, no <html>).
func renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

- [ ] **Step 2: Create `templates/_partials.html`** (shared head + nav):

```html
{{define "head"}}
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/static/app.css">
<script src="/static/htmx.min.js"></script>
<title>Memory Admin</title>
{{end}}

{{define "nav"}}
<nav class="topnav">
  <a href="/ui" class="brand">🧠 Memory</a>
  <a href="/ui/entities">Entities</a>
  <form method="post" action="/ui/project" class="proj">
    <label>Project
      <input name="project" value="{{.Project}}" list="projlist">
    </label>
    <button>Set</button>
  </form>
  <form method="post" action="/ui/logout"><button>Logout</button></form>
</nav>
{{end}}
```

- [ ] **Step 3: Create `templates/login.html`**:

```html
{{define "login"}}
<!doctype html>
<html lang="id">
<head>{{template "head"}}</head>
<body>
<div class="login">
  <h1>🧠 Memory Admin</h1>
  <form method="post" action="/ui/login">
    <input type="password" name="password" placeholder="password" autofocus required>
    <button>Login</button>
    {{if .Error}}<p class="err">{{.Error}}</p>{{end}}
  </form>
</div>
</body>
</html>
{{end}}
```

- [ ] **Step 4: Create `templates/dashboard.html`**:

```html
{{define "dashboard"}}
{{$p := .Project}}
{{$m := .Metrics}}
<!doctype html>
<html lang="id">
<head>{{template "head"}}</head>
<body>
{{template "nav" .}}
<main class="wrap">
  <h1>Dashboard <small>(project: {{$p}})</small></h1>
  <section class="cards">
    <div class="card"><b>{{$m.Entities}}</b><span>entities</span></div>
    <div class="card"><b>{{$m.Observations}}</b><span>observations</span></div>
    <div class="card"><b>{{$m.Relations}}</b><span>relations</span></div>
    <div class="card"><b>{{$m.RelationTypes}}</b><span>rel types</span></div>
    <div class="card"><b>{{printf "%.1f" $m.AvgObs}}</b><span>avg obs/entity</span></div>
    <div class="card"><b>{{printf "%.1f" $m.AvgRel}}</b><span>avg rel/entity</span></div>
    <div class="card warn"><b>{{$m.Orphans}}</b><span>orphans</span></div>
    <div class="card warn"><b>{{$m.Sparse}}</b><span>sparse (no obs)</span></div>
  </section>
  <section class="grid">
    <div>
      <h2>By type</h2>
      <table>{{range $m.ByType}}<tr><td>{{.Type}}</td><td>{{.Count}}</td></tr>{{else}}<tr><td><em>none</em></td></tr>{{end}}</table>
    </div>
    <div>
      <h2>Top by observations</h2>
      <table>{{range $m.TopByObs}}<tr><td><a href="/ui/entity?name={{.Name | queryenc}}&p={{$p}}">{{.Name}}</a></td><td>{{.ObsCount}}</td></tr>{{else}}<tr><td><em>none</em></td></tr>{{end}}</table>
    </div>
    <div>
      <h2>Top by relations (hubs)</h2>
      <table>{{range $m.TopByRel}}<tr><td><a href="/ui/entity?name={{.Name | queryenc}}&p={{$p}}">{{.Name}}</a></td><td>{{.RelCount}}</td></tr>{{else}}<tr><td><em>none</em></td></tr>{{end}}</table>
    </div>
    <div>
      <h2>Relation types</h2>
      <table>{{range $m.RelationTypes_}}<tr><td>{{.Type}}</td><td>{{.Count}}</td></tr>{{else}}<tr><td><em>none</em></td></tr>{{end}}</table>
    </div>
    <div>
      <h2>Recent entities</h2>
      <table>{{range $m.RecentEntities}}<tr><td><a href="/ui/entity?name={{.Name | queryenc}}&p={{$p}}">{{.Name}}</a></td><td>{{.Type}}</td></tr>{{else}}<tr><td><em>none</em></td></tr>{{end}}</table>
    </div>
    <div>
      <h2>Growth (30d)</h2>
      <table>{{range $m.Growth}}<tr><td>{{.Day}}</td><td>{{.Entities}}e / {{.Observations}}o</td></tr>{{else}}<tr><td><em>none</em></td></tr>{{end}}</table>
    </div>
  </section>
</main>
</body>
</html>
{{end}}
```

- [ ] **Step 5: Create `templates/entities.html`**:

```html
{{define "entities"}}
{{$p := .Project}}
<!doctype html>
<html lang="id">
<head>{{template "head"}}</head>
<body>
{{template "nav" .}}
<main class="wrap">
  <h1>Entities <small>(project: {{$p}})</small></h1>
  <div class="filters">
    <select name="type" hx-get="/ui/entities" hx-target="#rows" hx-include="[name='q']" hx-vals='{"p":"{{$p}}"}' hx-trigger="change">
      <option value="">all types</option>
      <option value="project"{{if eq .TypeFilter "project"}} selected{{end}}>project</option>
      <option value="person"{{if eq .TypeFilter "person"}} selected{{end}}>person</option>
      <option value="decision"{{if eq .TypeFilter "decision"}} selected{{end}}>decision</option>
      <option value="tool"{{if eq .TypeFilter "tool"}} selected{{end}}>tool</option>
      <option value="concept"{{if eq .TypeFilter "concept"}} selected{{end}}>concept</option>
      <option value="place"{{if eq .TypeFilter "place"}} selected{{end}}>place</option>
    </select>
    <input name="q" type="search" placeholder="search name…" value="{{.Query}}"
      hx-get="/ui/entities" hx-target="#rows" hx-trigger="keyup changed delay:300ms" hx-include="[name='type']" hx-vals='{"p":"{{$p}}"}'>
    <a href="/ui/entity?p={{$p}}" class="btn">+ New entity</a>
  </div>
  <table class="grid">
    <thead><tr><th>Name</th><th>Type</th><th>#obs</th><th>#rel</th></tr></thead>
    <tbody id="rows">
    {{template "entities_rows" .}}
    </tbody>
  </table>
</main>
</body>
</html>
{{end}}

{{define "entities_rows"}}
{{$p := .Project}}
{{range .Rows}}
<tr>
  <td><a href="/ui/entity?name={{.Name | queryenc}}&p={{$p}}">{{.Name}}</a></td>
  <td>{{.Type}}</td><td>{{.ObsCount}}</td><td>{{.RelCount}}</td>
</tr>
{{else}}
<tr><td colspan="4"><em>no entities</em></td></tr>
{{end}}
{{end}}
```

- [ ] **Step 6: Create `templates/entity.html`** (detail + CRUD forms):

```html
{{define "entity"}}
{{$p := .Project}}
{{$d := .Detail}}
<!doctype html>
<html lang="id">
<head>{{template "head"}}</head>
<body>
{{template "nav" .}}
<main class="wrap">
  {{if $d}}
  <h1>{{$d.Name}} <small>({{$d.Type}})</small></h1>

  <details class="panel"><summary>Edit entity</summary>
    <form method="post" action="/ui/entity/edit">
      <input type="hidden" name="oldName" value="{{$d.Name}}">
      <input type="hidden" name="p" value="{{$p}}">
      <input name="newName" value="{{$d.Name}}">
      <select name="type">
        {{$t := $d.Type}}
        {{range $.Types}}<option value="{{.}}"{{if eq . $t}} selected{{end}}>{{.}}</option>{{end}}
      </select>
      <button>Save</button>
    </form>
  </details>

  <h2>Observations</h2>
  <ul id="obs" class="obs">
    {{template "observations" .}}
  </ul>
  <form method="post" action="/ui/observation" hx-target="#obs" hx-swap="outerHTML">
    <input type="hidden" name="p" value="{{$p}}">
    <input type="hidden" name="entity" value="{{$d.Name}}">
    <input name="content" placeholder="new observation…" required>
    <button>Add</button>
  </form>

  <h2>Relations</h2>
  <ul id="rels" class="rels">
    {{template "relations" .}}
  </ul>
  <form method="post" action="/ui/relation" hx-target="#rels" hx-swap="outerHTML">
    <input type="hidden" name="p" value="{{$p}}">
    <input type="hidden" name="from" value="{{$d.Name}}">
    <input name="to" placeholder="target entity name" required>
    <input name="type" placeholder="RELATION_TYPE" required>
    <button>Add</button>
  </form>

  <details class="panel danger"><summary>Delete entity</summary>
    <form method="post" action="/ui/entity/delete" onsubmit="return confirm('Delete {{$d.Name}} and all its data?')">
      <input type="hidden" name="name" value="{{$d.Name}}">
      <input type="hidden" name="p" value="{{$p}}">
      <button class="danger">Delete permanently</button>
    </form>
  </details>

  {{else}}
  <h1>New entity <small>(project: {{$p}})</small></h1>
  <form method="post" action="/ui/entity" class="panel">
    <input type="hidden" name="p" value="{{$p}}">
    <input name="name" placeholder="entity name" required>
    <select name="type">{{range .Types}}<option value="{{.}}">{{.}}</option>{{end}}</select>
    <textarea name="observations" placeholder="one observation per line"></textarea>
    <button>Create</button>
  </form>
  {{end}}
</main>
</body>
</html>
{{end}}

{{define "observations"}}
{{$p := $.Project}}
{{range $d := $.Detail.Observations}}
<li>
  <span>{{.Content}}</span>
  <form method="post" action="/ui/observation/delete"
        hx-target="#obs" hx-swap="outerHTML"
        onsubmit="return confirm('Delete this observation?')">
    <input type="hidden" name="p" value="{{$p}}">
    <input type="hidden" name="id" value="{{.ID}}">
    <button class="x">✕</button>
  </form>
</li>
{{else}}<li><em>no observations</em></li>{{end}}
{{end}}

{{define "relations"}}
{{$p := $.Project}}
{{range $.Detail.Relations}}
<li>
  <span>{{if eq .Direction "out"}}→ {{else}}← {{end}} <b>{{.Type}}</b>
    {{if eq .Direction "out"}}→{{else}}←{{end}} <a href="/ui/entity?name={{.Other | queryenc}}&p={{$p}}">{{.Other}}</a></span>
  <form method="post" action="/ui/relation/delete" hx-target="#rels" hx-swap="outerHTML"
        onsubmit="return confirm('Delete this relation?')">
    <input type="hidden" name="p" value="{{$p}}">
    <input type="hidden" name="id" value="{{.ID}}">
    <button class="x">✕</button>
  </form>
</li>
{{else}}<li><em>no relations</em></li>{{end}}
{{end}}
```

- [ ] **Step 7: Create `static/app.css`**:

```css
:root{--bg:#0f1115;--fg:#e6e6e6;--muted:#9aa;--accent:#4f8cff;--line:#262b33;--warn:#e0a800}
*{box-sizing:border-box}
body{margin:0;font:14px/1.5 system-ui,Segoe UI,sans-serif;background:var(--bg);color:var(--fg)}
a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}
.topnav{display:flex;gap:12px;align-items:center;padding:10px 16px;background:#161a21;border-bottom:1px solid var(--line)}
.topnav .brand{font-weight:700;font-size:16px}
.topnav form.proj{display:flex;gap:6px;align-items:center;margin-left:auto;color:var(--muted)}
.topnav form.proj input{width:120px;padding:4px 6px;background:#0f1115;color:var(--fg);border:1px solid var(--line);border-radius:4px}
.topnav form{margin:0}
button,.btn{cursor:pointer;padding:5px 10px;border:1px solid var(--line);background:#1d222b;color:var(--fg);border-radius:4px}
button:hover,.btn:hover{border-color:var(--accent)}
.wrap{max-width:1100px;margin:0 auto;padding:16px}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(120px,1fr));gap:10px;margin:16px 0}
.card{background:#161a21;border:1px solid var(--line);border-radius:8px;padding:14px;text-align:center}
.card b{display:block;font-size:24px}
.card span{color:var(--muted);font-size:12px}
.card.warn b{color:var(--warn)}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:16px;margin-top:16px}
.grid h2{font-size:14px;color:var(--muted);border-bottom:1px solid var(--line);padding-bottom:4px}
table{width:100%;border-collapse:collapse}
td,th{padding:5px 8px;border-bottom:1px solid var(--line);text-align:left;font-size:13px}
.filters{display:flex;gap:10px;align-items:center;margin:12px 0;flex-wrap:wrap}
.filters input,.filters select,.panel input,.panel select,.panel textarea{background:#0f1115;color:var(--fg);border:1px solid var(--line);border-radius:4px;padding:6px}
.panel{background:#161a21;border:1px solid var(--line);border-radius:8px;padding:12px;margin:10px 0}
.panel summary{cursor:pointer;color:var(--muted)}
.panel textarea{width:100%;display:block;margin:6px 0}
.panel.danger summary{color:var(--warn)}
.danger{border-color:#5a2a2a}
.obs,.rels{list-style:none;padding:0}
.obs li,.rels li{display:flex;justify-content:space-between;align-items:center;padding:6px 8px;border-bottom:1px solid var(--line)}
button.x{padding:2px 7px;background:transparent;border:none;color:var(--muted)}
button.x:hover{color:#f66}
.login{max-width:280px;margin:80px auto;text-align:center}
.login form{display:flex;flex-direction:column;gap:8px;margin-top:16px}
.err{color:#f88}
```

- [ ] **Step 8: Download vendored htmx (pinned)**:

```bash
cd /home/orin/code/archive/mcp-memory-server-go
mkdir -p static
curl -fsSL https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js -o static/htmx.min.js
test -s static/htmx.min.js && head -c 80 static/htmx.min.js && echo
```
Expected: first bytes show `/*` htmx minified comment. (Pinned to 2.0.4; do not change without a reason.)

- [ ] **Step 9: Add the `queryenc` template func** (used by templates above). Edit `assets.go` `Funcs` map to add URL query encoding:

In `initTemplates()`, add to the Funcs map:
```go
		"queryenc": func(s string) string {
			// url.QueryEscape without importing net/url in template layer is messy;
			// expose via a closure.
			return queryEscape(s)
		},
```
And add to `assets.go`:
```go
import "net/url"

func queryEscape(s string) string { return url.QueryEscape(s) }
```
(Merge the import into the existing import block; do not duplicate `import` statements.)

- [ ] **Step 10: Build + vet** (templates now compile via embed):

```bash
go vet ./... && go build ./... && echo "BUILD OK"
```
Expected: `BUILD OK`. (Templates aren't rendered yet — Task 4/5 wire handlers. Parsing happens at runtime in `initTemplates`, but embed compiles now.)

- [ ] **Step 11: Commit**

```bash
git add assets.go templates/ static/
git commit -m "feat(ui): add templates, static assets (htmx 2.0.4, css), embed + render helpers"
```

---

### Task 4: Auth — cookie sign/verify, sessionAuth, login/logout

**Files:**
- Create: `web.go`
- Create: `web_test.go`

**Interfaces:**
- Consumes: package global `jwtSecret`; `render`/`tmpl`/`initTemplates` from Task 3.
- Produces (used by Task 5):
  - package vars `uiPassword string`, `cookieInsecure bool`
  - `signCookieValue(expiry int64) string`
  - `verifyCookieValue(v string) bool`
  - `sessionAuth(next http.Handler) http.Handler`
  - `auth(h http.HandlerFunc) http.Handler` // convenience wrapper
  - `handleLogin(w, r)`, `handleLogout(w, r)`
  - `activeProject(r *http.Request) string`

- [ ] **Step 1: Write the failing test** — create `web_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCookieSignVerify(t *testing.T) {
	jwtSecret = "test-secret"
	v := signCookieValue(time.Now().Add(time.Hour).Unix())
	if !verifyCookieValue(v) {
		t.Fatal("valid cookie rejected")
	}
	if verifyCookieValue("9999999999.tampered") {
		t.Fatal("tampered cookie accepted")
	}
	expired := signCookieValue(time.Now().Add(-time.Hour).Unix())
	if verifyCookieValue(expired) {
		t.Fatal("expired cookie accepted")
	}
	if verifyCookieValue("garbage") {
		t.Fatal("malformed cookie accepted")
	}
}

func TestSessionAuth(t *testing.T) {
	jwtSecret = "test-secret"
	called := false
	h := sessionAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	// No cookie -> redirect, handler not called.
	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("no-cookie status = %d, want 303", w.Code)
	}
	if called {
		t.Fatal("handler called without cookie")
	}

	// Valid cookie -> handler called.
	req2 := httptest.NewRequest(http.MethodGet, "/ui", nil)
	req2.AddCookie(&http.Cookie{Name: "mem_session", Value: signCookieValue(time.Now().Add(time.Hour).Unix())})
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("valid-cookie status = %d, want 200", w2.Code)
	}
	if !called {
		t.Fatal("handler not called with valid cookie")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -run 'TestCookieSignVerify|TestSessionAuth' -v ./...
```
Expected: FAIL — `signCookieValue` undefined.

- [ ] **Step 3: Implement `web.go`** (auth part only; handlers added in Task 5):

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	uiPassword     string
	cookieInsecure bool
)

const (
	sessionCookieName = "mem_session"
	projectCookieName = "mem_project"
	sessionMaxAge     = 30 * 24 * 3600
)

// signCookieValue produces "<expiry>.<base64url(HMAC-SHA256(secret, expiry))>".
func signCookieValue(expiry int64) string {
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	fmt.Fprintf(mac, "%d", expiry)
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d.%s", expiry, sig)
}

// verifyCookieValue checks signature and non-expiry.
func verifyCookieValue(v string) bool {
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return false
	}
	var expiry int64
	if _, err := fmt.Sscanf(parts[0], "%d", &expiry); err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	fmt.Fprintf(mac, "%d", expiry)
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return false
	}
	return expiry > time.Now().Unix()
}

func sessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || !verifyCookieValue(c.Value) {
			http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// auth wraps a HandlerFunc with sessionAuth.
func auth(h http.HandlerFunc) http.Handler { return sessionAuth(h) }

func activeProject(r *http.Request) string {
	if c, err := r.Cookie(projectCookieName); err == nil {
		if v := strings.TrimSpace(c.Value); v != "" {
			return v
		}
	}
	return "default"
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		render(w, "login", map[string]any{"Error": ""})
		return
	}
	_ = r.ParseForm()
	pw := r.FormValue("password")
	if uiPassword == "" || subtle.ConstantTimeCompare([]byte(pw), []byte(uiPassword)) != 1 {
		time.Sleep(300 * time.Millisecond) // blunt guessing
		render(w, "login", map[string]any{"Error": "Password salah"})
		return
	}
	exp := time.Now().Add(time.Duration(sessionMaxAge) * time.Second).Unix()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: signCookieValue(exp),
		Path: "/ui", MaxAge: sessionMaxAge,
		HttpOnly: true, Secure: !cookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/ui", http.StatusSeeOther)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/ui", MaxAge: -1,
		HttpOnly: true, Secure: !cookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
}
```

- [ ] **Step 4: Run test to verify pass**

```bash
go test -run 'TestCookieSignVerify|TestSessionAuth' -v ./...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web.go web_test.go
git commit -m "feat(ui): add session cookie auth (HMAC sign/verify, sessionAuth, login/logout)"
```

---

### Task 5: UI handlers, route wiring, main.go env, Dockerfile, .env.example

**Files:**
- Modify: `web.go` (append handlers)
- Modify: `main.go` (parse UI env + register routes)
- Modify: `Dockerfile` (COPY templates/ static/)
- Modify: `.env.example` (document new vars)

**Interfaces:**
- Consumes: everything from Tasks 1–4; existing `CreateEntities`, `AddObservations`, `CreateRelations`, `DeleteEntities` from `db.go`.
- Produces: a runnable server with `/ui` admin surface.

- [ ] **Step 1: Append UI handlers to `web.go`**:

```go
var entityTypes = []string{"project", "person", "decision", "tool", "concept", "place"}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	p := activeProject(r)
	m, err := DashboardMetrics(r.Context(), pool, p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "dashboard", map[string]any{"Project": p, "Metrics": m})
}

func handleEntities(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	tf := r.URL.Query().Get("type")
	q := r.URL.Query().Get("q")
	rows, err := ListEntities(r.Context(), pool, p, tf, q, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := map[string]any{"Project": p, "TypeFilter": tf, "Query": q, "Rows": rows}
	if r.Header.Get("HX-Request") == "true" {
		renderFragment(w, "entities_rows", data)
		return
	}
	render(w, "entities", data)
}

func handleEntityDetail(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	name := r.URL.Query().Get("name")
	var detail *EntityDetail
	if name != "" {
		d, err := GetEntityDetail(r.Context(), pool, p, name)
		if err == nil {
			detail = d
		}
	}
	render(w, "entity", map[string]any{"Project": p, "Detail": detail, "Types": entityTypes})
}

func handleEntityCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := r.FormValue("p")
	lines := strings.Split(r.FormValue("observations"), "\n")
	var obs []string
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" {
			obs = append(obs, s)
		}
	}
	if _, err := CreateEntities(r.Context(), pool, p, []EntityInput{
		{Name: r.FormValue("name"), EntityType: r.FormValue("type"), Observations: obs},
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entities?p="+p, http.StatusSeeOther)
}

func handleEntityUpdate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := r.FormValue("p")
	if err := UpdateEntity(r.Context(), pool, p, r.FormValue("oldName"), r.FormValue("newName"), r.FormValue("type")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entity?name="+r.FormValue("newName")+"&p="+p, http.StatusSeeOther)
}

func handleEntityDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := r.FormValue("p")
	if err := DeleteEntities(r.Context(), pool, p, []string{r.FormValue("name")}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entities?p="+p, http.StatusSeeOther)
}

func handleObservationAdd(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := r.FormValue("p")
	if err := AddObservations(r.Context(), pool, p, r.FormValue("entity"), []string{r.FormValue("content")}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	renderObsOrRel(w, r, "observations")
}

func handleObservationDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	var id int
	fmt.Sscanf(r.FormValue("id"), "%d", &id)
	_ = DeleteObservation(r.Context(), pool, r.FormValue("p"), id)
	renderObsOrRel(w, r, "observations")
}

func handleRelationCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := r.FormValue("p")
	if _, err := CreateRelations(r.Context(), pool, p, []RelationInput{
		{From: r.FormValue("from"), To: r.FormValue("to"), RelationType: r.FormValue("type")},
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	renderObsOrRel(w, r, "relations")
}

func handleRelationDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	var id int
	fmt.Sscanf(r.FormValue("id"), "%d", &id)
	_ = DeleteRelation(r.Context(), pool, r.FormValue("p"), id)
	renderObsOrRel(w, r, "relations")
}

func handleSetProject(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := strings.TrimSpace(r.FormValue("project"))
	if p == "" {
		p = "default"
	}
	http.SetCookie(w, &http.Cookie{
		Name: projectCookieName, Value: p, Path: "/",
		MaxAge: 365 * 24 * 3600, HttpOnly: true, Secure: !cookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/ui", http.StatusSeeOther)
}

// renderObsOrRel re-renders the observations or relations fragment for an entity,
// used as the htmx swap target after add/delete.
func renderObsOrRel(w http.ResponseWriter, r *http.Request, frag string) {
	p := r.FormValue("p")
	name := r.FormValue("entity")
	if name == "" {
		name = r.FormValue("from")
	}
	var detail *EntityDetail
	if d, err := GetEntityDetail(r.Context(), pool, p, name); err == nil {
		detail = d
	}
	renderFragment(w, frag, map[string]any{"Project": p, "Detail": detail})
}
```

**Note:** the `renderObsOrRel` for relations looks up the entity by `from`. For an incoming-relation delete this still works because we re-read the same entity's full detail. Acceptable for v1.

- [ ] **Step 2: Register routes + parse env in `main.go`**

In `main()`, after `jwtSecret` is assigned and before `server := buildServer()`, add:

```go
	// --- Web UI config ---
	uiPassword = os.Getenv("UI_PASSWORD")
	if uiPassword == "" {
		uiPassword = token // fallback to MEMORY_API_TOKEN
	}
	cookieInsecure = os.Getenv("UI_COOKIE_INSECURE") == "true"
```

After the existing `mux.HandleFunc("/health", ...)` block (still before `corsHandler := ...`), add UI route registration:

```go
	// --- Web UI (fail-closed: routes only registered if a password is set) ---
	initTemplates()
	if uiPassword != "" {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
		mux.HandleFunc("GET /ui/login", handleLogin)
		mux.HandleFunc("POST /ui/login", handleLogin)
		mux.Handle("POST /ui/logout", auth(handleLogout))
		mux.Handle("POST /ui/project", auth(handleSetProject))
		mux.Handle("GET /", auth(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ui", http.StatusSeeOther) }))
		mux.Handle("GET /ui", auth(handleDashboard))
		mux.Handle("GET /ui/entities", auth(handleEntities))
		mux.Handle("GET /ui/entity", auth(handleEntityDetail))
		mux.Handle("POST /ui/entity", auth(handleEntityCreate))
		mux.Handle("POST /ui/entity/edit", auth(handleEntityUpdate))
		mux.Handle("POST /ui/entity/delete", auth(handleEntityDelete))
		mux.Handle("POST /ui/observation", auth(handleObservationAdd))
		mux.Handle("POST /ui/observation/delete", auth(handleObservationDelete))
		mux.Handle("POST /ui/relation", auth(handleRelationCreate))
		mux.Handle("POST /ui/relation/delete", auth(handleRelationDelete))
	} else {
		log.Printf("WARNING: UI_PASSWORD and MEMORY_API_TOKEN both unset — web UI disabled (fail-closed)")
	}
```

(`GET /` redirect and `/static/` are intentionally outside `sessionAuth`; all `/ui/*` routes are auth-gated. `initTemplates()` is called once at startup.)

- [ ] **Step 3: Update `Dockerfile`** — add the COPY lines before `RUN go build`:

```dockerfile
COPY *.go ./
COPY schema.sql ./
COPY templates/ ./templates/
COPY static/ ./static/
```
(Insert the two `COPY templates/` / `COPY static/` lines after the existing `COPY schema.sql ./` line.)

- [ ] **Step 4: Update `.env.example`** — append:

```
# --- Web UI (optional) ---
# Password for the /ui admin dashboard login. Falls back to MEMORY_API_TOKEN if unset.
# If both are empty, the UI is disabled (fail-closed).
UI_PASSWORD=
# Set to "true" only for local http dev (omits the cookie Secure flag).
UI_COOKIE_INSECURE=false
```

- [ ] **Step 5: Integration smoke test** — create `web_integration_test.go`:

```go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUILoginAndCRUDSmoke(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()
	// Reassign the package-global pool used by handlers.
	savedPool := pool
	_ = savedPool
	ctx := context.Background()

	jwtSecret = "test-secret"
	uiPassword = "secret"
	cookieInsecure = true
	initTemplates()

	// Register routes on a fresh mux.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/login", handleLogin)
	mux.HandleFunc("POST /ui/login", handleLogin)
	mux.Handle("GET /ui", auth(handleDashboard))
	mux.Handle("POST /ui/entity", auth(handleEntityCreate))

	// Unauthenticated dashboard redirects to login.
	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("unauth status = %d, want 303", w.Code)
	}

	// Login yields a valid session cookie.
	form := url.Values{"password": {"secret"}}
	lreq := httptest.NewRequest(http.MethodPost, "/ui/login", strings.NewReader(form.Encode()))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lw := httptest.NewRecorder()
	mux.ServeHTTP(lw, lreq)
	if lw.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", lw.Code)
	}
	var sess string
	for _, c := range lw.Result().Cookies() {
		if c.Name == "mem_session" {
			sess = c.Value
		}
	}
	if sess == "" || !verifyCookieValue(sess) {
		t.Fatal("no valid session cookie after login")
	}

	// Authenticated dashboard returns 200.
	dreq := httptest.NewRequest(http.MethodGet, "/ui", nil)
	dreq.AddCookie(&http.Cookie{Name: "mem_session", Value: sess, Path: "/ui"})
	dw := httptest.NewRecorder()
	mux.ServeHTTP(dw, dreq)
	if dw.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", dw.Code)
	}

	// Create entity via authenticated POST.
	CreateEntities(ctx, pool, "default", nil) // ensure project exists; noop
	cform := url.Values{"p": {"default"}, "name": {"Smoke"}, "type": {"tool"}}
	creq := httptest.NewRequest(http.MethodPost, "/ui/entity", strings.NewReader(cform.Encode()))
	creq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	creq.AddCookie(&http.Cookie{Name: "mem_session", Value: sess, Path: "/ui"})
	cw := httptest.NewRecorder()
	mux.ServeHTTP(cw, creq)
	if cw.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, want 303; body=%s", cw.Code, cw.Body.String())
	}
	d, err := GetEntityDetail(ctx, pool, "default", "Smoke")
	if err != nil || d == nil {
		t.Fatalf("entity not created: %v", err)
	}
	_ = time.Now
}
```

- [ ] **Step 6: Run full suite + build + vet**

```bash
PGPASSWORD=root psql -h localhost -p 5432 -U postgres -d postgres -c "DROP DATABASE IF EXISTS memory_web_test; CREATE DATABASE memory_web_test OWNER postgres;"
DATABASE_URL="postgres://postgres:root@localhost:5432/memory_web_test" go test ./... -v 2>&1 | tail -40
go vet ./... && go build ./... && echo "BUILD OK"
```
Expected: all tests PASS, `BUILD OK`.

- [ ] **Step 7: Commit**

```bash
git add web.go web_integration_test.go main.go Dockerfile .env.example
git commit -m "feat(ui): add UI handlers, route wiring, main env, Dockerfile + .env.example"
```

- [ ] **Step 8: Manual smoke (local) before push**

```bash
# terminal 1: local postgres already up on :5432
DATABASE_URL="postgres://postgres:root@localhost:5432/memory" UI_PASSWORD=devtest UI_COOKIE_INSECURE=true PORT=3100 ./mcp-memory-server &
# browser: http://localhost:3100/ui  -> login (devtest) -> dashboard -> create/edit/delete
```
Verify: login works, dashboard shows counts, can create an entity, add+delete an observation, add+delete a relation, delete an entity. Kill the server afterward.

- [ ] **Step 9: Push (triggers Coolify rebuild)**

```bash
git push origin main
```
Then in Coolify: redeploy; set `UI_PASSWORD` env (or rely on `MEMORY_API_TOKEN` fallback). Access `https://memory.najib.id/ui`.

---

## Self-Review (run after writing; fix inline)

1. **Spec coverage:**
   - Monitoring dashboard metrics → Task 2 (`DashboardMetrics`) + Task 5 (`handleDashboard`) + `dashboard.html`. ✓
   - Browse (list/filter/search) → Task 2 (`ListEntities`) + Task 5 (`handleEntities`) + `entities.html`. ✓
   - Browse entity detail → Task 2 (`GetEntityDetail`) + Task 5 (`handleEntityDetail`) + `entity.html`. ✓
   - CRUD → Task 1 (`crud.go`) + Task 5 handlers (create entity, edit entity, delete entity, add/delete observation, create/delete relation). ✓
   - Login-form auth + stateless HMAC cookie → Task 4. ✓
   - Project selector → `activeProject` + `handleSetProject` + nav form. ✓
   - Fail-closed when no password → `main.go` guard. ✓
   - go:embed single static binary + Dockerfile COPY → Task 3 + Task 5. ✓
   - Cross-project safety → `crud.go` ownership checks + test. ✓
   - Tests (crud, stats, cookie, integration smoke) → Tasks 1,2,4,5. ✓
2. **Placeholder scan:** none. htmx fetched by pinned curl (deterministic, not a placeholder). CSS + templates fully provided.
3. **Type consistency:** `Metrics.RelationTypes_` field name matches template use `{{$m.RelationTypes_}}`; `renderFragment` fragment names (`entities_rows`, `observations`, `relations`) match template `{{define}}` names; handler `renderObsOrRel` uses those fragment names; `entityTypes` matches `{{range .Types}}`; `GetEntityDetail` returns `*EntityDetail` used as `Detail` in templates; `EntitySummary` fields used in templates match. ✓
4. **Execution order note:** Task 2 (`GetEntityDetail`) must be implemented before Task 1's test passes (Task 1 test imports `GetEntityDetail`). Tasks are written Task1→Task5 but the implementer should land Task 2 before running Task 1 tests (or run both together). This is called out in Task 1 Step 2.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-03-memory-web-ui.md`. See the message that follows for execution options.
