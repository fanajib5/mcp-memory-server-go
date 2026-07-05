package http

import (
	"encoding/json"
	"net/http"

	"mcp-memory-server/internal/entity"
)

func (u *UI) HandleBackup(w http.ResponseWriter, r *http.Request) {
	p := activeProject(r)
	u.renderUI(w, r, "backup", map[string]any{"Project": p})
}

func (u *UI) HandleBackupExport(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	payload, err := u.UC.Export(r.Context(), p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="memory-`+p+`.json"`)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

func (u *UI) HandleBackupImport(w http.ResponseWriter, r *http.Request) {
	p := r.FormValue("p")
	if p == "" {
		p = activeProject(r)
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	var payload entity.ExportPayload
	if err := json.NewDecoder(file).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	res, err := u.UC.Import(r.Context(), p, &payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u.renderUI(w, r, "backup", map[string]any{"Project": p, "Imported": res})
}
