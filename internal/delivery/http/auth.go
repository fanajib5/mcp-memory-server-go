package http

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

func (u *UI) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		render(w, u.Tmpl, "login", map[string]any{"Error": ""})
		return
	}
	_ = r.ParseForm()
	pw := r.FormValue("password")
	if u.Cfg.UIPassword == "" || subtle.ConstantTimeCompare([]byte(pw), []byte(u.Cfg.UIPassword)) != 1 {
		time.Sleep(300 * time.Millisecond) // blunt guessing
		render(w, u.Tmpl, "login", map[string]any{"Error": "Password salah"})
		return
	}
	exp := time.Now().Add(time.Duration(sessionMaxAge) * time.Second).Unix()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: u.Session.signCookieValue(exp),
		Path: "/ui", MaxAge: sessionMaxAge,
		HttpOnly: true, Secure: !u.Cfg.CookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: generateCSRFToken(),
		Path: "/ui", MaxAge: sessionMaxAge,
		HttpOnly: true, Secure: !u.Cfg.CookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/ui", http.StatusSeeOther)
}

func (u *UI) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/ui", MaxAge: -1,
		HttpOnly: true, Secure: !u.Cfg.CookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: "", Path: "/ui", MaxAge: -1,
		HttpOnly: true, Secure: !u.Cfg.CookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
}

func (u *UI) HandleSetProject(w http.ResponseWriter, r *http.Request) {
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
		MaxAge: 365 * 24 * 3600, HttpOnly: true, Secure: !u.Cfg.CookieInsecure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/ui", http.StatusSeeOther)
}
