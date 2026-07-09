package http

import (
	"html/template"
	"net/http"
	"strings"

	"mcp-memory-server/internal/config"
	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/usecase"
)

// UI holds the dependencies shared by every web-UI handler.
type UI struct {
	Cfg     *config.Config
	UC      *usecase.MemoryUseCase
	SU      *usecase.StatsUseCase
	Tmpl    *template.Template
	Session *Session
	Chat    *ChatHandler
}

func (u *UI) renderUI(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	if data != nil {
		data["CSRF"] = csrfToken(r)
	}
	render(w, u.Tmpl, page, data)
}

func (u *UI) renderUIFragment(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data != nil {
		data["CSRF"] = csrfToken(r)
	}
	renderFragment(w, u.Tmpl, name, data)
}

func activeProject(r *http.Request) string {
	if c, err := r.Cookie(projectCookieName); err == nil {
		if v := strings.TrimSpace(c.Value); v != "" {
			return v
		}
	}
	return "default"
}

// renderObsOrRel re-renders the observations or relations fragment for an entity,
// used as the htmx swap target after add/delete.
func (u *UI) renderObsOrRel(w http.ResponseWriter, r *http.Request, frag string) {
	p := r.FormValue("p")
	name := r.FormValue("entity")
	if name == "" {
		name = r.FormValue("from")
	}
	var detail *entity.EntityDetail
	if d, err := u.SU.GetEntityDetail(r.Context(), p, name); err == nil {
		detail = d
	}
	u.renderUIFragment(w, r, frag, map[string]any{"Project": p, "Detail": detail})
}
