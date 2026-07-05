package http

import (
	"fmt"
	"net/http"

	"mcp-memory-server/internal/entity"
)

func (u *UI) HandleObservationAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := r.FormValue("p")
	// Optional confidence from the UI form (empty = neutral/NULL).
	var confidences []float64
	if cf := r.FormValue("confidence"); cf != "" {
		var c float64
		if _, err := fmt.Sscanf(cf, "%f", &c); err == nil {
			confidences = []float64{c}
		}
	}
	if err := u.UC.AddObservations(r.Context(), p, r.FormValue("entity"), []string{r.FormValue("content")}, confidences); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u.renderObsOrRel(w, r, "observations")
}

func (u *UI) HandleObservationDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id int
	fmt.Sscanf(r.FormValue("id"), "%d", &id)
	_ = u.UC.DeleteObservation(r.Context(), r.FormValue("p"), id)
	u.renderObsOrRel(w, r, "observations")
}

// HandleObservationEditGet returns an inline edit form for one observation.
func (u *UI) HandleObservationEditGet(w http.ResponseWriter, r *http.Request) {
	var id int
	fmt.Sscanf(r.URL.Query().Get("id"), "%d", &id)
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	content, ent, err := u.SU.ObservationByID(r.Context(), p, id)
	if err != nil {
		http.Error(w, "observation not found", http.StatusNotFound)
		return
	}
	u.renderUIFragment(w, r, "observation_edit", map[string]any{
		"Project": p, "ID": id, "Content": content, "Entity": ent,
	})
}

// HandleObservationEditSave updates one observation by id and returns the refreshed list.
func (u *UI) HandleObservationEditSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id int
	fmt.Sscanf(r.FormValue("id"), "%d", &id)
	p := r.FormValue("p")
	// Optional confidence from the UI form (empty = leave unchanged).
	var newConf *float64
	if cf := r.FormValue("confidence"); cf != "" {
		var c float64
		if _, err := fmt.Sscanf(cf, "%f", &c); err == nil {
			newConf = &c
		}
	}
	if err := u.UC.UpdateObservation(r.Context(), p, id, r.FormValue("content"), newConf); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u.renderObsOrRel(w, r, "observations")
}

// HandleObservationRow returns the read-only row for one observation (cancel-edit target).
func (u *UI) HandleObservationRow(w http.ResponseWriter, r *http.Request) {
	var id int
	fmt.Sscanf(r.URL.Query().Get("id"), "%d", &id)
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	content, ent, err := u.SU.ObservationByID(r.Context(), p, id)
	if err != nil {
		http.Error(w, "observation not found", http.StatusNotFound)
		return
	}
	u.renderUIFragment(w, r, "observation_row", map[string]any{
		"Project": p, "Entity": ent, "Obs": entity.EntityDetailObservation{ID: id, Content: content},
	})
}
