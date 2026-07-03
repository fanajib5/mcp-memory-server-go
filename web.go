package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
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
