package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestUILoginAndCRUDSmoke(t *testing.T) {
	pool = integrationPool(t) // assign to package global used by UI handlers
	defer pool.Close()
	ctx := context.Background()

	jwtSecret = "test-secret"
	uiPassword = "secret"
	cookieInsecure = true
	initTemplates()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/login", handleLogin)
	mux.HandleFunc("POST /ui/login", handleLogin)
	mux.Handle("GET /ui", auth(handleDashboard))
	mux.Handle("GET /ui/entity", auth(handleEntityDetail))
	mux.Handle("POST /ui/entity", auth(handleEntityCreate))

	// Unauthenticated dashboard -> redirect to login.
	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("unauth status = %d, want 303", w.Code)
	}

	// Wrong password -> no session cookie.
	form := url.Values{"password": {"wrong"}}
	lreq := httptest.NewRequest(http.MethodPost, "/ui/login", strings.NewReader(form.Encode()))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lw := httptest.NewRecorder()
	mux.ServeHTTP(lw, lreq)
	for _, c := range lw.Result().Cookies() {
		if c.Name == sessionCookieName {
			t.Fatal("wrong password should not set session cookie")
		}
	}

	// Correct password -> valid session cookie.
	form.Set("password", "secret")
	lreq2 := httptest.NewRequest(http.MethodPost, "/ui/login", strings.NewReader(form.Encode()))
	lreq2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lw2 := httptest.NewRecorder()
	mux.ServeHTTP(lw2, lreq2)
	if lw2.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", lw2.Code)
	}
	var sess string
	for _, c := range lw2.Result().Cookies() {
		if c.Name == sessionCookieName {
			sess = c.Value
		}
	}
	if sess == "" || !verifyCookieValue(sess) {
		t.Fatal("no valid session cookie after login")
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: sess, Path: "/ui"}

	// Authenticated dashboard -> 200.
	dreq := httptest.NewRequest(http.MethodGet, "/ui", nil)
	dreq.AddCookie(cookie)
	dw := httptest.NewRecorder()
	mux.ServeHTTP(dw, dreq)
	if dw.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", dw.Code)
	}

	// Create entity via POST.
	cform := url.Values{"p": {"default"}, "name": {"Smoke"}, "type": {"tool"}}
	creq := httptest.NewRequest(http.MethodPost, "/ui/entity", strings.NewReader(cform.Encode()))
	creq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	creq.AddCookie(cookie)
	cw := httptest.NewRecorder()
	mux.ServeHTTP(cw, creq)
	if cw.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, want 303; body=%s", cw.Code, cw.Body.String())
	}
	d, err := GetEntityDetail(ctx, pool, "default", "Smoke")
	if err != nil || d == nil || d.Type != "tool" {
		t.Fatalf("entity not created: %v %+v", err, d)
	}

	// Detail page renders the entity.
	greq := httptest.NewRequest(http.MethodGet, "/ui/entity?name=Smoke&p=default", nil)
	greq.AddCookie(cookie)
	gw := httptest.NewRecorder()
	mux.ServeHTTP(gw, greq)
	if gw.Code != http.StatusOK || !strings.Contains(gw.Body.String(), "Smoke") {
		t.Fatalf("detail status=%d, body missing entity name", gw.Code)
	}
}
