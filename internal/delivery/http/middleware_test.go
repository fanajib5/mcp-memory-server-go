// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"mcp-memory-server/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		PublicURL:         "https://example.com",
		JWTSecret:         "test-secret",
		OAuthClientID:     "user",
		OAuthClientSecret: "pass",
		UIPassword:        "secret",
		CookieInsecure:    true,
	}
}

func TestCSRFCheck(t *testing.T) {
	s := NewSession(testConfig())
	called := false
	h := s.CSRFCheck(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodPost, "/ui/x", strings.NewReader(""))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("no-cookie status = %d, want 403", w.Code)
	}
	if called {
		t.Fatal("handler called without csrf cookie")
	}

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

func TestCookieSignVerify(t *testing.T) {
	s := NewSession(testConfig())
	v := s.signCookieValue(time.Now().Add(time.Hour).Unix())
	if !s.verifyCookieValue(v) {
		t.Fatal("valid cookie rejected")
	}
	if s.verifyCookieValue("9999999999.tampered") {
		t.Fatal("tampered cookie accepted")
	}
	expired := s.signCookieValue(time.Now().Add(-time.Hour).Unix())
	if s.verifyCookieValue(expired) {
		t.Fatal("expired cookie accepted")
	}
	if s.verifyCookieValue("garbage") {
		t.Fatal("malformed cookie accepted")
	}
}

func TestSessionAuth(t *testing.T) {
	s := NewSession(testConfig())
	called := false
	h := s.SessionAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

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
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: s.signCookieValue(time.Now().Add(time.Hour).Unix())})
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("valid-cookie status = %d, want 200", w2.Code)
	}
	if !called {
		t.Fatal("handler not called with valid cookie")
	}
}

func TestCORSMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("options request returns 200 preflight", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.Handle("/mcp", corsMiddleware("*", inner))
		req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("aco = %q, want *", w.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("request from allowed origin gets cors headers", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.Handle("/mcp", corsMiddleware("https://example.com", inner))
		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Errorf("aco = %q, want https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("request from disallowed origin blocked", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.Handle("/mcp", corsMiddleware("https://allowed.com", inner))
		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.Header.Set("Origin", "https://evil.com")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Header().Get("Access-Control-Allow-Origin") == "https://evil.com" {
			t.Errorf("aco should not be set for disallowed origin")
		}
	})
}

func TestAuthMiddleware(t *testing.T) {
	cfg := testConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(cfg, inner))

	t.Run("valid jwt passes through", func(t *testing.T) {
		now := time.Now()
		claims := &Claims{
			RegisteredClaims: jwtRegisteredClaims(now, now.Add(1*time.Hour)),
			Scope:            "mcp",
		}
		signed := mustSign(t, claims, cfg.JWTSecret)

		req := httptest.NewRequest(http.MethodGet, "/mcp", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+signed)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	t.Run("static token rejected when not configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp", http.NoBody)
		req.Header.Set("Authorization", "Bearer static-test-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (no static token configured)", w.Code)
		}
	})

	t.Run("missing token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp", http.NoBody)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})
}

func TestAuthMiddlewareStaticToken(t *testing.T) {
	cfg := testConfig()
	cfg.APIToken = "static-test-token"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(cfg, inner))

	req := httptest.NewRequest(http.MethodGet, "/mcp", http.NoBody)
	req.Header.Set("Authorization", "Bearer static-test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (static bearer token should be accepted)", w.Code)
	}
}
