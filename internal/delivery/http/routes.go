package http

import (
	"io/fs"
	"net/http"

	"mcp-memory-server/internal/config"
)

// NewRouter wires every route onto a single mux and returns it. The MCP stream
// handler is passed in (built by main from the MCP server); OAuth, UI and
// session middleware are owned by this package.
func NewRouter(cfg *config.Config, mcpHandler http.Handler, oauth *OAuthService, ui *UI) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(cfg, mcpHandler))
	mux.HandleFunc("/.well-known/oauth-authorization-server", oauth.HandleMetadata)
	mux.HandleFunc("/oauth/authorize", oauth.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauth.HandleToken)
	mux.HandleFunc("/oauth/register", oauth.HandleRegister)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Web UI (fail-closed: routes only registered if a password is set).
	if cfg.UIPassword != "" {
		staticSub, _ := fs.Sub(staticFS, "static")
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
		mux.HandleFunc("GET /ui/login", ui.HandleLogin)
		mux.HandleFunc("POST /ui/login", ui.HandleLogin)
		mux.Handle("POST /ui/logout", ui.Session.CSRFAuth(ui.HandleLogout))
		mux.Handle("POST /ui/project", ui.Session.CSRFAuth(ui.HandleSetProject))
		mux.Handle("/", ui.Session.Auth(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/ui", http.StatusSeeOther)
		}))
		mux.Handle("GET /ui", ui.Session.Auth(ui.HandleDashboard))
		mux.Handle("GET /ui/entities", ui.Session.Auth(ui.HandleEntities))
		mux.Handle("GET /ui/graph", ui.Session.Auth(ui.HandleGraph))
		mux.Handle("GET /ui/graph.json", ui.Session.Auth(ui.HandleGraphJSON))
		mux.Handle("GET /ui/backup", ui.Session.Auth(ui.HandleBackup))
		mux.Handle("GET /ui/export", ui.Session.Auth(ui.HandleBackupExport))
		mux.Handle("POST /ui/import", ui.Session.CSRFAuth(ui.HandleBackupImport))
		mux.Handle("GET /ui/entity", ui.Session.Auth(ui.HandleEntityDetail))
		mux.Handle("POST /ui/entity", ui.Session.CSRFAuth(ui.HandleEntityCreate))
		mux.Handle("POST /ui/entity/edit", ui.Session.CSRFAuth(ui.HandleEntityUpdate))
		mux.Handle("POST /ui/entity/delete", ui.Session.CSRFAuth(ui.HandleEntityDelete))
		mux.Handle("POST /ui/observation", ui.Session.CSRFAuth(ui.HandleObservationAdd))
		mux.Handle("POST /ui/observation/delete", ui.Session.CSRFAuth(ui.HandleObservationDelete))
		mux.Handle("GET /ui/observation/edit", ui.Session.Auth(ui.HandleObservationEditGet))
		mux.Handle("POST /ui/observation/edit", ui.Session.CSRFAuth(ui.HandleObservationEditSave))
		mux.Handle("GET /ui/observation/row", ui.Session.Auth(ui.HandleObservationRow))
		mux.Handle("POST /ui/relation", ui.Session.CSRFAuth(ui.HandleRelationCreate))
		mux.Handle("POST /ui/relation/delete", ui.Session.CSRFAuth(ui.HandleRelationDelete))
		mux.Handle("GET /ui/history", ui.Session.Auth(ui.HandleHistory))
	}

	return corsMiddleware(cfg.CORSAllowedOrigins, mux)
}
