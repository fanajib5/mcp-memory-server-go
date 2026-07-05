package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mcp-memory-server/internal/entity"
)

func TestUILoginAndCRUDSmoke(t *testing.T) {
	ui := uiTestHarness(t)
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/login", ui.HandleLogin)
	mux.HandleFunc("POST /ui/login", ui.HandleLogin)
	mux.Handle("GET /ui", ui.Session.Auth(ui.HandleDashboard))
	mux.Handle("GET /ui/entity", ui.Session.Auth(ui.HandleEntityDetail))
	mux.Handle("POST /ui/entity", ui.Session.Auth(ui.HandleEntityCreate))

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
	if sess == "" || !ui.Session.verifyCookieValue(sess) {
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
	d, err := ui.SU.GetEntityDetail(ctx, "default", "Smoke")
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

func TestBackupExportImport(t *testing.T) {
	ui := uiTestHarness(t)
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ui/login", ui.HandleLogin)
	mux.Handle("GET /ui/export", ui.Session.Auth(ui.HandleBackupExport))
	mux.Handle("POST /ui/import", ui.Session.CSRFAuth(ui.HandleBackupImport))

	// Login -> session + csrf cookies.
	lreq := httptest.NewRequest(http.MethodPost, "/ui/login", strings.NewReader("password=secret"))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lw := httptest.NewRecorder()
	mux.ServeHTTP(lw, lreq)
	var sess, csrf string
	for _, c := range lw.Result().Cookies() {
		if c.Name == sessionCookieName {
			sess = c.Value
		}
		if c.Name == csrfCookieName {
			csrf = c.Value
		}
	}
	if sess == "" {
		t.Fatal("no session cookie")
	}
	if csrf == "" {
		t.Fatal("no csrf cookie")
	}

	// Seed an entity, then export it as JSON.
	ui.UC.CreateEntities(ctx, "default", []entity.EntityInput{
		{Name: "Seed", Type: "tool", Observations: []string{"seed fact"}},
	})

	ereq := httptest.NewRequest(http.MethodGet, "/ui/export?p=default", nil)
	ereq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess, Path: "/ui"})
	ew := httptest.NewRecorder()
	mux.ServeHTTP(ew, ereq)
	if ew.Code != http.StatusOK {
		t.Fatalf("export status = %d", ew.Code)
	}
	var payload entity.ExportPayload
	if err := json.Unmarshal(ew.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if len(payload.Entities) == 0 {
		t.Fatal("export returned no entities")
	}

	// Import the exported JSON into a DIFFERENT project via multipart upload (with csrf).
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, _ := mw.CreateFormField("p")
	fw.Write([]byte("imported"))
	fw2, _ := mw.CreateFormField("csrf")
	fw2.Write([]byte(csrf))
	ff, _ := mw.CreateFormFile("file", "backup.json")
	ff.Write(ew.Body.Bytes())
	mw.Close()

	ireq := httptest.NewRequest(http.MethodPost, "/ui/import", body)
	ireq.Header.Set("Content-Type", "multipart/form-data; boundary="+mw.Boundary())
	ireq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess, Path: "/ui"})
	ireq.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf, Path: "/ui"})
	iw := httptest.NewRecorder()
	mux.ServeHTTP(iw, ireq)
	if iw.Code != http.StatusOK {
		t.Fatalf("import status = %d body=%s", iw.Code, iw.Body.String())
	}

	// Verify the entity landed in project "imported".
	d, err := ui.SU.GetEntityDetail(ctx, "imported", "Seed")
	if err != nil || d == nil {
		t.Fatalf("not imported: %v", err)
	}
	if d.Type != "tool" || len(d.Observations) != 1 || d.Observations[0].Content != "seed fact" {
		t.Fatalf("imported detail = %+v", d)
	}
}

func TestInlineEditObservation(t *testing.T) {
	ui := uiTestHarness(t)
	ctx := context.Background()

	ui.UC.CreateEntities(ctx, "default", []entity.EntityInput{
		{Name: "Inline", Type: "tool", Observations: []string{"original text"}},
	})
	d, _ := ui.SU.GetEntityDetail(ctx, "default", "Inline")
	id := d.Observations[0].ID

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ui/login", ui.HandleLogin)
	mux.Handle("GET /ui/observation/edit", ui.Session.Auth(ui.HandleObservationEditGet))
	mux.Handle("POST /ui/observation/edit", ui.Session.Auth(ui.HandleObservationEditSave))

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

	d2, _ := ui.SU.GetEntityDetail(ctx, "default", "Inline")
	if d2.Observations[0].Content != "edited text" {
		t.Fatalf("content = %q, want 'edited text'", d2.Observations[0].Content)
	}
}
