package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var pool *pgxpool.Pool
var jwtSecret string

type AuthCode struct {
	Code        string
	ClientID    string
	RedirectURI string
	ExpiresAt   time.Time
	Scope       string
}

var (
	authCodes      = make(map[string]*AuthCode)
	authCodesMutex = &sync.Mutex{}
)

type Claims struct {
	jwt.RegisteredClaims
	Scope string `json:"scope,omitempty"`
}

type OAuthMetadata struct {
	Issuer                   string   `json:"issuer"`
	AuthorizationEndpoint    string   `json:"authorization_endpoint"`
	TokenEndpoint            string   `json:"token_endpoint"`
	RegistrationEndpoint     string   `json:"registration_endpoint,omitempty"`
	ScopesSupported          []string `json:"scopes_supported"`
	ResponseTypesSupported   []string `json:"response_types_supported"`
	GrantTypesSupported      []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

func handleOAuthMetadata(baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		metadata := OAuthMetadata{
			Issuer:                   baseURL,
			AuthorizationEndpoint:    baseURL + "/oauth/authorize",
			TokenEndpoint:            baseURL + "/oauth/token",
			RegistrationEndpoint:     baseURL + "/oauth/register",
			ScopesSupported:          []string{"mcp"},
			ResponseTypesSupported:   []string{"code"},
			GrantTypesSupported:      []string{"client_credentials"},
			TokenEndpointAuthMethods: []string{"client_secret_basic", "client_secret_post"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metadata)
	}
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	if clientID == "" || clientSecret == "" {
		http.Error(w, "Missing client_id or client_secret", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{
		"client_id": "` + clientID + `",
		"client_secret": "` + clientSecret + `",
		"grant_types": ["client_credentials"],
		"token_endpoint_auth_method": "client_secret_post"
	}`))
}

func randomState(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	scope := r.URL.Query().Get("scope")
	responseType := r.URL.Query().Get("response_type")

	if responseType != "code" {
		http.Error(w, `{"error":"unsupported_response_type"}`, http.StatusBadRequest)
		return
	}
	if clientID == "" || redirectURI == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	oauthClientID := os.Getenv("OAUTH_CLIENT_ID")
	if oauthClientID == "" {
		oauthClientID = os.Getenv("MEMORY_API_TOKEN")
	}
	if clientID != oauthClientID {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
		return
	}

	code := base64.RawURLEncoding.EncodeToString([]byte(randomState(32)))
	authCodesMutex.Lock()
	authCodes[code] = &AuthCode{
		Code:        code,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		Scope:       scope,
	}
	if authCodes[code].Scope == "" {
		authCodes[code].Scope = "mcp"
	}
	authCodesMutex.Unlock()

	redirectTarget := redirectURI + "?code=" + code
	if state != "" {
		redirectTarget += "&state=" + state
	}
	http.Redirect(w, r, redirectTarget, http.StatusFound)
}

func handleToken(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	grantType := r.FormValue("grant_type")

	if grantType == "authorization_code" {
		code := r.FormValue("code")
		redirectURI := r.FormValue("redirect_uri")
		clientID := r.FormValue("client_id")
		clientSecret := r.FormValue("client_secret")

		if clientID == "" && r.Header.Get("Authorization") != "" {
			auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Basic ")
			if decoded, err := base64.StdEncoding.DecodeString(auth); err == nil {
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 {
					clientID, clientSecret = parts[0], parts[1]
				}
			}
		}

		authCodesMutex.Lock()
		stored, ok := authCodes[code]
		if ok {
			delete(authCodes, code)
		}
		authCodesMutex.Unlock()

		if !ok || stored == nil || stored.ExpiresAt.Before(time.Now()) {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		if redirectURI != "" && stored.RedirectURI != redirectURI {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		oauthClientID := os.Getenv("OAUTH_CLIENT_ID")
		if oauthClientID == "" {
			oauthClientID = os.Getenv("MEMORY_API_TOKEN")
		}
		oauthClientSecret := os.Getenv("OAUTH_CLIENT_SECRET")
		if oauthClientSecret == "" {
			oauthClientSecret = os.Getenv("MEMORY_API_TOKEN")
		}
		if clientID != oauthClientID || clientSecret != oauthClientSecret {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}

		claims := Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				Subject:   stored.ClientID,
			},
			Scope: stored.Scope,
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(jwtSecret))
		if err != nil {
			http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"` + tokenString + `","token_type":"Bearer","expires_in":3600,"scope":"` + stored.Scope + `"}`))
		return
	}

	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	if clientID == "" && r.Header.Get("Authorization") != "" {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Basic ")
		if decoded, err := base64.StdEncoding.DecodeString(auth); err == nil {
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				clientID, clientSecret = parts[0], parts[1]
			}
		}
	}

	oauthClientID := os.Getenv("OAUTH_CLIENT_ID")
	oauthClientSecret := os.Getenv("OAUTH_CLIENT_SECRET")

	if oauthClientID == "" || oauthClientSecret == "" {
		oauthClientID = os.Getenv("MEMORY_API_TOKEN")
		oauthClientSecret = os.Getenv("MEMORY_API_TOKEN")
	}

	if clientID != oauthClientID || clientSecret != oauthClientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   clientID,
		},
		Scope: "mcp",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"access_token":"` + tokenString + `","token_type":"Bearer","expires_in":3600,"scope":"mcp` + `"}`))
}

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

func corsMiddleware(allowedOrigins string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowedOrigins != "" {
			if allowedOrigins == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				allowed := strings.Split(allowedOrigins, ",")
				for _, o := range allowed {
					if strings.TrimSpace(o) == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Vary", "Origin")
						break
					}
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims := &Claims{}
		_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			return []byte(jwtSecret), nil
		})
		if err != nil {
			http.Error(w, `{"error":"Invalid token"}`, http.StatusUnauthorized)
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
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	oauthClientID := os.Getenv("OAUTH_CLIENT_ID")
	oauthClientSecret := os.Getenv("OAUTH_CLIENT_SECRET")
	if oauthClientID == "" || oauthClientSecret == "" {
		if token == "" {
			log.Fatal("FATAL: Set OAUTH_CLIENT_ID and OAUTH_CLIENT_SECRET, or MEMORY_API_TOKEN as fallback")
		}
		oauthClientID = token
		oauthClientSecret = token
	}

	jwtSecret = os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = token
	}
	if jwtSecret == "" {
		log.Fatal("FATAL: JWT_SECRET env var is required")
	}

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		host := os.Getenv("HOST")
		if host == "" {
			host = "localhost"
		}
		publicURL = "https://" + host
		if port != "443" && port != "" {
			publicURL += ":" + port
		}
	}

	corsAllowedOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")

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
	mux.Handle("/mcp", authMiddleware(handler))
	mux.HandleFunc("/.well-known/oauth-authorization-server", handleOAuthMetadata(publicURL))
	mux.HandleFunc("/oauth/authorize", handleOAuthAuthorize)
	mux.HandleFunc("/oauth/token", handleToken)
	mux.HandleFunc("/oauth/register", handleRegister)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	corsHandler := corsMiddleware(corsAllowedOrigins, mux)
	log.Printf("mcp-memory-server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, corsHandler); err != nil {
		log.Fatal(err)
	}
}
