package http

import (
	"net/http"
)

func (u *UI) HandleHistory(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Redirect(w, r, "/ui/entities?p="+p, http.StatusSeeOther)
		return
	}
	entries, err := u.UC.GetHistory(r.Context(), p, name, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u.renderUI(w, r, "history", map[string]any{"Project": p, "Name": name, "Entries": entries})
}
