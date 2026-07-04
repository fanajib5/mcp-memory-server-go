package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	csrfCookieName    = "mem_csrf"
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

// generateCSRFToken returns a random hex token for double-submit CSRF.
func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano()) // extremely unlikely fallback
	}
	return hex.EncodeToString(b)
}

func csrfToken(r *http.Request) string {
	if c, err := r.Cookie(csrfCookieName); err == nil {
		return c.Value
	}
	return ""
}

// csrfCheck enforces double-submit CSRF: the mem_csrf cookie must equal the form "csrf" field.
func csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(csrfCookieName)
		if err != nil || c.Value == "" {
			http.Error(w, "csrf: missing token", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
		}
		if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(c.Value)) != 1 {
			http.Error(w, "csrf: mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// csrfAuth wraps a mutating HandlerFunc with session + CSRF checks.
func csrfAuth(h http.HandlerFunc) http.Handler { return sessionAuth(csrfCheck(h)) }

// renderUI renders a full page, injecting the CSRF token into the data map.
func renderUI(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	if data != nil {
		data["CSRF"] = csrfToken(r)
	}
	render(w, page, data)
}

// renderUIFragment renders a fragment, injecting the CSRF token into the data map.
func renderUIFragment(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data != nil {
		data["CSRF"] = csrfToken(r)
	}
	renderFragment(w, name, data)
}

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
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: generateCSRFToken(),
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
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: "", Path: "/ui", MaxAge: -1,
		HttpOnly: true, Secure: !cookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
}

var entityTypes = []string{"project", "person", "decision", "tool", "concept", "place"}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	p := activeProject(r)
	m, err := DashboardMetrics(r.Context(), pool, p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderUI(w, r, "dashboard", map[string]any{"Project": p, "Metrics": m})
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
		renderUIFragment(w, r, "entities_rows", data)
		return
	}
	renderUI(w, r, "entities", data)
}

func handleEntityDetail(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	name := r.URL.Query().Get("name")
	var detail *EntityDetail
	if name != "" {
		if d, err := GetEntityDetail(r.Context(), pool, p, name); err == nil {
			detail = d
		}
	}
	renderUI(w, r, "entity", map[string]any{"Project": p, "Detail": detail, "Types": entityTypes})
}

func handleEntityCreate(w http.ResponseWriter, r *http.Request) {
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
	if _, err := CreateEntities(r.Context(), pool, p, []EntityInput{
		{Name: r.FormValue("name"), EntityType: r.FormValue("type"), Observations: obs},
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entities?p="+url.QueryEscape(p), http.StatusSeeOther)
}

func handleEntityUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := r.FormValue("p")
	newName := r.FormValue("newName")
	if err := UpdateEntity(r.Context(), pool, p, r.FormValue("oldName"), newName, r.FormValue("type")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entity?name="+url.QueryEscape(newName)+"&p="+url.QueryEscape(p), http.StatusSeeOther)
}

func handleEntityDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := r.FormValue("p")
	if err := DeleteEntities(r.Context(), pool, p, []string{r.FormValue("name")}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/entities?p="+url.QueryEscape(p), http.StatusSeeOther)
}

func handleObservationAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := r.FormValue("p")
	if err := AddObservations(r.Context(), pool, p, r.FormValue("entity"), []string{r.FormValue("content")}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	renderObsOrRel(w, r, "observations")
}

func handleObservationDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id int
	fmt.Sscanf(r.FormValue("id"), "%d", &id)
	_ = DeleteObservation(r.Context(), pool, r.FormValue("p"), id)
	renderObsOrRel(w, r, "observations")
}

func handleRelationCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id int
	fmt.Sscanf(r.FormValue("id"), "%d", &id)
	_ = DeleteRelation(r.Context(), pool, r.FormValue("p"), id)
	renderObsOrRel(w, r, "relations")
}

func handleSetProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
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
	renderUIFragment(w, r, frag, map[string]any{"Project": p, "Detail": detail})
}

func handleGraph(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	renderUI(w, r, "graph", map[string]any{"Project": p})
}

func handleGraphJSON(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	g, err := GraphData(r.Context(), pool, p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

// handleObservationEditGet returns an inline edit form for one observation.
func handleObservationEditGet(w http.ResponseWriter, r *http.Request) {
	var id int
	fmt.Sscanf(r.URL.Query().Get("id"), "%d", &id)
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	content, entity, err := ObservationByID(r.Context(), pool, p, id)
	if err != nil {
		http.Error(w, "observation not found", http.StatusNotFound)
		return
	}
	renderUIFragment(w, r, "observation_edit", map[string]any{"Project": p, "ID": id, "Content": content, "Entity": entity})
}

// handleObservationEditSave updates one observation by id and returns the refreshed list.
func handleObservationEditSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id int
	fmt.Sscanf(r.FormValue("id"), "%d", &id)
	p := r.FormValue("p")
	if err := UpdateObservation(r.Context(), pool, p, id, r.FormValue("content")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	renderObsOrRel(w, r, "observations")
}

// handleObservationRow returns the read-only row for one observation (cancel-edit target).
func handleObservationRow(w http.ResponseWriter, r *http.Request) {
	var id int
	fmt.Sscanf(r.URL.Query().Get("id"), "%d", &id)
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	content, entity, err := ObservationByID(r.Context(), pool, p, id)
	if err != nil {
		http.Error(w, "observation not found", http.StatusNotFound)
		return
	}
	renderUIFragment(w, r, "observation_row", map[string]any{
		"Project": p, "Entity": entity, "Obs": EntityDetailObservation{ID: id, Content: content},
	})
}

func handleBackup(w http.ResponseWriter, r *http.Request) {
	p := activeProject(r)
	renderUI(w, r, "backup", map[string]any{"Project": p})
}

func handleBackupExport(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	payload, err := ExportGraph(r.Context(), pool, p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="memory-`+p+`.json"`)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

func handleBackupImport(w http.ResponseWriter, r *http.Request) {
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
	var payload ExportPayload
	if err := json.NewDecoder(file).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	res, err := ImportGraph(r.Context(), pool, p, &payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderUI(w, r, "backup", map[string]any{"Project": p, "Imported": res})
}
