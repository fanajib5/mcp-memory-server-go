package http

import (
	"net/http"
	"net/url"
	"strings"

	"mcp-memory-server/internal/config"
	"mcp-memory-server/internal/entity"
)

func (u *UI) HandleEntityDetail(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	name := r.URL.Query().Get("name")
	var detail *entity.EntityDetail
	if name != "" {
		if d, err := u.SU.GetEntityDetail(r.Context(), p, name); err == nil {
			detail = d
		}
	}
	u.renderUI(w, r, "entity", map[string]any{"Project": p, "Detail": detail, "Types": config.EntityTypes()})
}

func (u *UI) HandleEntityCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := r.FormValue("p")
	lines := strings.Split(r.FormValue("observations"), "\n")
	var obs []string
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" {
			obs = append(obs, s)
		}
	}
	if _, err := u.UC.CreateEntities(r.Context(), p, []entity.EntityInput{
		{Name: r.FormValue("name"), Type: r.FormValue("type"), Observations: obs},
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entities?p="+url.QueryEscape(p), http.StatusSeeOther)
}

func (u *UI) HandleEntityUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := r.FormValue("p")
	newName := r.FormValue("newName")
	if err := u.UC.UpdateEntity(r.Context(), p, r.FormValue("oldName"), newName, r.FormValue("type")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entity?name="+url.QueryEscape(newName)+"&p="+url.QueryEscape(p), http.StatusSeeOther)
}

func (u *UI) HandleEntityDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := r.FormValue("p")
	if err := u.UC.DeleteEntities(r.Context(), p, []string{r.FormValue("name")}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entities?p="+url.QueryEscape(p), http.StatusSeeOther)
}
