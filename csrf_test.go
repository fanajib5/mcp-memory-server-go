package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCSRFCheck(t *testing.T) {
	called := false
	h := csrfCheck(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	// Missing csrf cookie -> 403, handler not called.
	req := httptest.NewRequest(http.MethodPost, "/ui/x", strings.NewReader(""))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("no-cookie status = %d, want 403", w.Code)
	}
	if called {
		t.Fatal("handler called without csrf cookie")
	}

	// Cookie + matching form field -> 200, handler called.
	form := url.Values{"csrf": {"tok"}}
	req2 := httptest.NewRequest(http.MethodPost, "/ui/x", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("match status = %d, want 200", w2.Code)
	}
	if !called {
		t.Fatal("handler not called on valid csrf")
	}

	// Mismatch -> 403, handler not called.
	called = false
	form.Set("csrf", "wrong")
	req3 := httptest.NewRequest(http.MethodPost, "/ui/x", strings.NewReader(form.Encode()))
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req3.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)
	if w3.Code != http.StatusForbidden {
		t.Fatalf("mismatch status = %d, want 403", w3.Code)
	}
	if called {
		t.Fatal("handler called on csrf mismatch")
	}
}
