package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBackupExportImport(t *testing.T) {
	pool = integrationPool(t)
	defer pool.Close()
	ctx := context.Background()

	jwtSecret = "test-secret"
	uiPassword = "secret"
	cookieInsecure = true
	initTemplates()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ui/login", handleLogin)
	mux.Handle("GET /ui/export", auth(handleBackupExport))
	mux.Handle("POST /ui/import", csrfAuth(handleBackupImport))

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
	CreateEntities(ctx, pool, "default", []EntityInput{
		{Name: "Seed", EntityType: "tool", Observations: []string{"seed fact"}},
	})
	ereq := httptest.NewRequest(http.MethodGet, "/ui/export?p=default", nil)
	ereq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess, Path: "/ui"})
	ew := httptest.NewRecorder()
	mux.ServeHTTP(ew, ereq)
	if ew.Code != http.StatusOK {
		t.Fatalf("export status = %d", ew.Code)
	}
	var payload ExportPayload
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
	d, err := GetEntityDetail(ctx, pool, "imported", "Seed")
	if err != nil || d == nil {
		t.Fatalf("not imported: %v", err)
	}
	if d.Type != "tool" || len(d.Observations) != 1 || d.Observations[0].Content != "seed fact" {
		t.Fatalf("imported detail = %+v", d)
	}
}
