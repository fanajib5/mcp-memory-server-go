package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCookieSignVerify(t *testing.T) {
	jwtSecret = "test-secret"
	v := signCookieValue(time.Now().Add(time.Hour).Unix())
	if !verifyCookieValue(v) {
		t.Fatal("valid cookie rejected")
	}
	if verifyCookieValue("9999999999.tampered") {
		t.Fatal("tampered cookie accepted")
	}
	expired := signCookieValue(time.Now().Add(-time.Hour).Unix())
	if verifyCookieValue(expired) {
		t.Fatal("expired cookie accepted")
	}
	if verifyCookieValue("garbage") {
		t.Fatal("malformed cookie accepted")
	}
}

func TestSessionAuth(t *testing.T) {
	jwtSecret = "test-secret"
	called := false
	h := sessionAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("no-cookie status = %d, want 303", w.Code)
	}
	if called {
		t.Fatal("handler called without cookie")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/ui", nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: signCookieValue(time.Now().Add(time.Hour).Unix())})
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("valid-cookie status = %d, want 200", w2.Code)
	}
	if !called {
		t.Fatal("handler not called with valid cookie")
	}
}
