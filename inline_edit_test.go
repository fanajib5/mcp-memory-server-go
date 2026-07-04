package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInlineEditObservation(t *testing.T) {
	pool = integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	jwtSecret = "test-secret"
	uiPassword = "secret"
	cookieInsecure = true
	initTemplates()

	CreateEntities(ctx, pool, "default", []EntityInput{
		{Name: "Inline", EntityType: "tool", Observations: []string{"original text"}},
	})
	d, _ := GetEntityDetail(ctx, pool, "default", "Inline")
	id := d.Observations[0].ID

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ui/login", handleLogin)
	mux.Handle("GET /ui/observation/edit", auth(handleObservationEditGet))
	mux.Handle("POST /ui/observation/edit", auth(handleObservationEditSave))

	// Login -> session cookie.
	lreq := httptest.NewRequest(http.MethodPost, "/ui/login", strings.NewReader("password=secret"))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lw := httptest.NewRecorder()
	mux.ServeHTTP(lw, lreq)
	var sess string
	for _, c := range lw.Result().Cookies() {
		if c.Name == sessionCookieName {
			sess = c.Value
		}
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: sess, Path: "/ui"}

	// GET edit form -> prefilled with current content.
	greq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/ui/observation/edit?id=%d&p=default", id), nil)
	greq.AddCookie(cookie)
	gw := httptest.NewRecorder()
	mux.ServeHTTP(gw, greq)
	if gw.Code != http.StatusOK || !strings.Contains(gw.Body.String(), "original text") {
		t.Fatalf("edit form: code=%d body=%s", gw.Code, gw.Body.String())
	}

	// POST save -> content updated, refreshed list returned.
	body := fmt.Sprintf("id=%d&p=default&entity=Inline&content=edited+text", id)
	preq := httptest.NewRequest(http.MethodPost, "/ui/observation/edit", strings.NewReader(body))
	preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	preq.AddCookie(cookie)
	pw := httptest.NewRecorder()
	mux.ServeHTTP(pw, preq)
	if pw.Code != http.StatusOK {
		t.Fatalf("save: code=%d body=%s", pw.Code, pw.Body.String())
	}

	d2, _ := GetEntityDetail(ctx, pool, "default", "Inline")
	if d2.Observations[0].Content != "edited text" {
		t.Fatalf("content = %q, want 'edited text'", d2.Observations[0].Content)
	}
}
