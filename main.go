package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var pool *pgxpool.Pool

// ---- Tool input types (jsonschema tags give the LLM field descriptions) ----

type CreateEntitiesInput struct {
	Project  string        `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	Entities []EntityInput `json:"entities" jsonschema:"Entities to create or update"`
}

type AddObservationsInput struct {
	Project      string   `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	EntityName   string   `json:"entityName"`
	Observations []string `json:"observations" jsonschema:"New facts to attach to this entity"`
}

type CreateRelationsInput struct {
	Project   string          `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	Relations []RelationInput `json:"relations" jsonschema:"Directed relations to create between entities"`
}

type DeleteEntitiesInput struct {
	Project string   `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	Names   []string `json:"names" jsonschema:"Entity names to delete, including their observations and relations"`
}

type SearchInput struct {
	Project string `json:"project,omitempty" jsonschema:"Optional project/namespace; defaults to 'default'"`
	Query   string `json:"query"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Max results, default 20, max 100"`
}

type ReadGraphInput struct {
	Project string `json:"project,omitempty" jsonschema:"Optional project to scope to; blank = all projects (debug view)"`
}

type ExportInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project to export; blank = all projects"`
}

type ImportInput struct {
	Project   string           `json:"project,omitempty" jsonschema:"Target project; defaults to 'default'"`
	Entities  []ExportEntity   `json:"entities,omitempty" jsonschema:"Entities to import"`
	Relations []ExportRelation `json:"relations,omitempty" jsonschema:"Relations to import"`
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult("error encoding result: " + err.Error())
	}
	return textResult(string(b))
}

// ---- Tool handlers ----

func handleCreateEntities(ctx context.Context, req *mcp.CallToolRequest, in CreateEntitiesInput) (*mcp.CallToolResult, any, error) {
	created, err := CreateEntities(ctx, pool, in.Project, in.Entities)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	var names strings.Builder
	for i, n := range created {
		if i > 0 {
			names.WriteString(", ")
		}
		names.WriteString(n)
	}
	return textResult("Created/updated entities: " + names.String()), nil, nil
}

func handleAddObservations(ctx context.Context, req *mcp.CallToolRequest, in AddObservationsInput) (*mcp.CallToolResult, any, error) {
	if err := AddObservations(ctx, pool, in.Project, in.EntityName, in.Observations); err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return textResult("Observations added to " + in.EntityName), nil, nil
}

func handleCreateRelations(ctx context.Context, req *mcp.CallToolRequest, in CreateRelationsInput) (*mcp.CallToolResult, any, error) {
	created, err := CreateRelations(ctx, pool, in.Project, in.Relations)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	out := ""
	for i, r := range created {
		if i > 0 {
			out += "\n"
		}
		out += r
	}
	if out == "" {
		out = "No new relations created."
	}
	return textResult(out), nil, nil
}

func handleDeleteEntities(ctx context.Context, req *mcp.CallToolRequest, in DeleteEntitiesInput) (*mcp.CallToolResult, any, error) {
	if err := DeleteEntities(ctx, pool, in.Project, in.Names); err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return textResult("Deleted entities."), nil, nil
}

func handleSearch(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, any, error) {
	results, err := SearchMemory(ctx, pool, in.Project, in.Query, in.Limit)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(results), nil, nil
}

func handleReadGraph(ctx context.Context, req *mcp.CallToolRequest, in ReadGraphInput) (*mcp.CallToolResult, any, error) {
	graph, err := ReadFullGraph(ctx, pool, in.Project)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(graph), nil, nil
}

func handleExport(ctx context.Context, req *mcp.CallToolRequest, in ExportInput) (*mcp.CallToolResult, any, error) {
	payload, err := ExportGraph(ctx, pool, in.Project)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(payload), nil, nil
}

func handleImport(ctx context.Context, req *mcp.CallToolRequest, in ImportInput) (*mcp.CallToolResult, any, error) {
	g := &ExportPayload{Entities: in.Entities, Relations: in.Relations}
	res, err := ImportGraph(ctx, pool, in.Project, g)
	if err != nil {
		return textResult("error: " + err.Error()), nil, nil
	}
	return jsonResult(res), nil, nil
}

// ---- Server wiring ----

func buildServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "mcp-memory-server", Version: "1.0.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_create_entities",
		Description: "Create one or more entities in a project's knowledge graph, optionally with initial observations. Reuses an entity if (project, name) already exists.",
	}, handleCreateEntities)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_add_observations",
		Description: "Add new observations (facts) to an existing entity in a project. Creates the entity if it doesn't exist yet.",
	}, handleAddObservations)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_create_relations",
		Description: "Create directed relations between entities in a project, e.g. 'MIS-APAR --DEPLOYED_VIA--> Coolify'. Relation type should be active voice, UPPER_SNAKE_CASE.",
	}, handleCreateRelations)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_delete_entities",
		Description: "Delete entities (and their observations/relations) by name within a project.",
	}, handleDeleteEntities)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_search",
		Description: "Search a project's knowledge graph for entities matching a query (matches entity names and observation content). Prefer this over memory_read_graph.",
	}, handleSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_read_graph",
		Description: "Read a project's entire knowledge graph (blank project = all projects). Expensive — prefer memory_search for targeted lookups. Use only for discovery/debugging.",
	}, handleReadGraph)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_export",
		Description: "Export a project's knowledge graph (entities + relations) as structured JSON for backup or migration. Blank project = all projects.",
	}, handleExport)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_import",
		Description: "Import entities + relations from structured JSON into a project. Idempotent (skips existing entities/relations). Useful for restoring a backup or migrating data.",
	}, handleImport)

	return server
}

func authMiddleware(token string, next http.Handler) http.Handler {
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, expected) != 1 {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("FATAL: DATABASE_URL env var is required")
	}
	token := os.Getenv("MEMORY_API_TOKEN")
	if token == "" {
		log.Fatal("FATAL: MEMORY_API_TOKEN env var is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	var err error
	pool, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer pool.Close()

	if err := EnsureSchema(ctx, pool); err != nil {
		log.Fatalf("ensure schema: %v", err)
	}

	server := buildServer()
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(token, handler))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("mcp-memory-server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
