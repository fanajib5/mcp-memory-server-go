// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newOAuthTestEnv builds an OAuthService + mux with a fixed test config.
func newOAuthTestEnv(t *testing.T) (http.Handler, string) {
	t.Helper()
	cfg := testConfig()
	oauth := NewOAuthService(cfg)
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", oauth.HandleMetadata)
	mux.HandleFunc("/oauth/authorize", oauth.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauth.HandleToken)
	mux.HandleFunc("/oauth/register", oauth.HandleRegister)
	return mux, cfg.JWTSecret
}

func TestHandleOAuthMetadata(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var meta OAuthMetadata
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.Issuer != "https://example.com" {
		t.Errorf("issuer = %q, want https://example.com", meta.Issuer)
	}
	if meta.TokenEndpoint != "https://example.com/oauth/token" {
		t.Errorf("token_endpoint = %q, want https://example.com/oauth/token", meta.TokenEndpoint)
	}
	// Grant types include both authorization_code and client_credentials now.
	found := false
	for _, g := range meta.GrantTypesSupported {
		if g == "authorization_code" {
			found = true
		}
	}
	if !found {
		t.Errorf("grant_types = %v, want to include authorization_code", meta.GrantTypesSupported)
	}
}

func TestHandleRegister(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)

	t.Run("success", func(t *testing.T) {
		body := strings.NewReader("client_id=myclient&client_secret=mysecret")
		req := httptest.NewRequest(http.MethodPost, "/oauth/register", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if res["client_id"] != "myclient" {
			t.Errorf("client_id = %q, want myclient", res["client_id"])
		}
		if res["client_secret"] != "mysecret" {
			t.Errorf("client_secret = %q, want mysecret", res["client_secret"])
		}
	})

	t.Run("missing credentials", func(t *testing.T) {
		body := strings.NewReader("client_id=")
		req := httptest.NewRequest(http.MethodPost, "/oauth/register", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}
