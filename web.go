package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
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
		if d, err := GetEntityDetail(r.Context(), pool, p, name); err == nil {
			detail = d
		}
	}
	render(w, "entity", map[string]any{"Project": p, "Detail": detail, "Types": entityTypes})
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
	renderFragment(w, frag, map[string]any{"Project": p, "Detail": detail})
}

func handleGraph(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	if p == "" {
		p = activeProject(r)
	}
	render(w, "graph", map[string]any{"Project": p})
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
