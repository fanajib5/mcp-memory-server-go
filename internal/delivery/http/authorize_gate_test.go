// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func gateMux(t *testing.T) http.Handler {
	t.Helper()
	cfg := testConfig()
	oauth := NewOAuthService(cfg)
	ui := &UI{Cfg: cfg, Tmpl: MustParseTemplates(), Session: NewSession(cfg)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/login", ui.HandleLogin)
	mux.HandleFunc("POST /ui/login", ui.HandleLogin)
	mux.Handle("/oauth/authorize", ui.Session.Auth(oauth.HandleAuthorize))
	mux.HandleFunc("/oauth/token", oauth.HandleToken)
	return mux
}

func postLogin(t *testing.T, handler http.Handler, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/ui/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatal("no session cookie set after login")
	return nil
}

const probeAuthorizeURI = "/oauth/authorize?client_id=user&redirect_uri=https%3A%2F%2Fclaude.ai%2Foauth%2Fcallback&response_type=code"

func TestAuthorizeRequiresLogin(t *testing.T) {
	handler := gateMux(t)
	req := httptest.NewRequest(http.MethodGet, probeAuthorizeURI, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 to login. body=%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/ui/login?next=") {
		t.Fatalf("location = %q, want /ui/login?next=...", loc)
	}
	got, err := url.QueryUnescape(strings.TrimPrefix(loc, "/ui/login?next="))
	if err != nil {
		t.Fatalf("unescape next: %v", err)
	}
	if got != probeAuthorizeURI {
		t.Errorf("next = %q, want %q", got, probeAuthorizeURI)
	}
}

func TestAuthorizeWithSessionIssuesCode(t *testing.T) {
	handler := gateMux(t)
	lw := postLogin(t, handler, "password=secret")
	cookie := sessionCookie(t, lw)

	req := httptest.NewRequest(http.MethodGet, probeAuthorizeURI, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302. body=%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "code=") {
		t.Fatalf("location = %q, want code in callback", loc)
	}
}

func TestLoginNextRedirect(t *testing.T) {
	handler := gateMux(t)

	t.Run("valid relative next is honored", func(t *testing.T) {
		w := postLogin(t, handler, "password=secret&next="+url.QueryEscape(probeAuthorizeURI))
		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", w.Code)
		}
		if loc := w.Header().Get("Location"); loc != probeAuthorizeURI {
			t.Fatalf("location = %q, want %q", loc, probeAuthorizeURI)
		}
	})

	t.Run("absolute url rejected", func(t *testing.T) {
		w := postLogin(t, handler, "password=secret&next="+url.QueryEscape("https://evil.com/steal"))
		if loc := w.Header().Get("Location"); loc != "/ui" {
			t.Fatalf("location = %q, want /ui", loc)
		}
	})

	t.Run("protocol-relative url rejected", func(t *testing.T) {
		w := postLogin(t, handler, "password=secret&next="+url.QueryEscape("//evil.com/steal"))
		if loc := w.Header().Get("Location"); loc != "/ui" {
			t.Fatalf("location = %q, want /ui", loc)
		}
	})

	t.Run("backslash and newline rejected", func(t *testing.T) {
		for _, evil := range []string{"/\\evil.com", "/ui\r\nX-Injected: 1"} {
			w := postLogin(t, handler, "password=secret&next="+url.QueryEscape(evil))
			if loc := w.Header().Get("Location"); loc != "/ui" {
				t.Fatalf("next=%q location = %q, want /ui", evil, loc)
			}
		}
	})
}

func TestLoginSessionCookieCoversOAuth(t *testing.T) {
	handler := gateMux(t)
	w := postLogin(t, handler, "password=secret")
	c := sessionCookie(t, w)
	if c.Path != "/" {
		t.Fatalf("session cookie path = %q, want / (must reach /oauth/authorize)", c.Path)
	}
}
