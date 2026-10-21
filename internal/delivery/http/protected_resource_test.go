// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func newProtectedResourceTestEnv(t *testing.T) http.Handler {
	t.Helper()
	oauth := NewOAuthService(testConfig())
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", oauth.HandleProtectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource/", oauth.HandleProtectedResourceMetadata)
	return mux
}

func TestProtectedResourceMetadata(t *testing.T) {
	handler := newProtectedResourceTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var meta ProtectedResourceMetadata
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.Resource != "https://example.com/mcp" {
		t.Errorf("resource = %q, want https://example.com/mcp", meta.Resource)
	}
	if len(meta.AuthorizationServers) != 1 || meta.AuthorizationServers[0] != "https://example.com" {
		t.Errorf("authorization_servers = %v, want [https://example.com]", meta.AuthorizationServers)
	}
	if !containsString(meta.BearerMethodsSupported, "header") {
		t.Errorf("bearer_methods_supported = %v, want to include header", meta.BearerMethodsSupported)
	}
	if !containsString(meta.ScopesSupported, "mcp") {
		t.Errorf("scopes_supported = %v, want to include mcp", meta.ScopesSupported)
	}
	if meta.ResourceName == "" {
		t.Error("resource_name is empty")
	}
}

func TestProtectedResourceMetadataPathAware(t *testing.T) {
	handler := newProtectedResourceTestEnv(t)

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/.well-known/oauth-protected-resource/mcp", "https://example.com/mcp"},
		{"/.well-known/oauth-protected-resource/events", "https://example.com/events"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tc.path, w.Code)
		}
		var meta ProtectedResourceMetadata
		if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
			t.Fatalf("%s: decode metadata: %v", tc.path, err)
		}
		if meta.Resource != tc.want {
			t.Errorf("%s: resource = %q, want %q", tc.path, meta.Resource, tc.want)
		}
	}
}

func TestAuthMiddlewareWWWAuthenticateChallenge(t *testing.T) {
	cfg := testConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(cfg, inner))

	challenge := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		got := w.Header().Get("WWW-Authenticate")
		if !strings.Contains(got, "Bearer") {
			t.Errorf("WWW-Authenticate = %q, want Bearer scheme", got)
		}
		want := `resource_metadata="https://example.com/.well-known/oauth-protected-resource/mcp"`
		if !strings.Contains(got, want) {
			t.Errorf("WWW-Authenticate = %q, want to contain %s", got, want)
		}
		return got
	}

	t.Run("missing token gets challenge", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp", http.NoBody)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		challenge(w)
	})

	t.Run("invalid token gets challenge", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp", http.NoBody)
		req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		challenge(w)
	})
}

func containsString(list []string, want string) bool {
	return slices.Contains(list, want)
}
