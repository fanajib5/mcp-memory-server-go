// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

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

	register := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	t.Run("generates credentials for confidential client", func(t *testing.T) {
		w := register(`{"redirect_uris":["https://client.example/cb"],"client_name":"grok","grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"client_secret_post"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201. body=%s", w.Code, w.Body.String())
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if res["client_id"] == "" || res["client_id"] == nil {
			t.Fatal("client_id not generated")
		}
		if res["client_secret"] == "" || res["client_secret"] == nil {
			t.Fatal("client_secret not generated")
		}
		if res["client_secret_expires_at"] != float64(0) {
			t.Errorf("client_secret_expires_at = %v, want 0", res["client_secret_expires_at"])
		}
		if res["token_endpoint_auth_method"] != "client_secret_post" {
			t.Errorf("token_endpoint_auth_method = %v, want client_secret_post", res["token_endpoint_auth_method"])
		}
		uris, _ := res["redirect_uris"].([]any)
		if len(uris) != 1 || uris[0] != "https://client.example/cb" {
			t.Errorf("redirect_uris = %v, want echo of request", res["redirect_uris"])
		}
	})

	t.Run("public client gets no secret", func(t *testing.T) {
		w := register(`{"redirect_uris":["https://client.example/cb"],"client_name":"grok-public","token_endpoint_auth_method":"none"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201. body=%s", w.Code, w.Body.String())
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if res["client_id"] == "" || res["client_id"] == nil {
			t.Fatal("client_id not generated")
		}
		if secret, _ := res["client_secret"].(string); secret != "" {
			t.Errorf("client_secret = %q, want empty for public client", secret)
		}
	})

	t.Run("missing redirect_uris rejected", func(t *testing.T) {
		w := register(`{"client_name":"nourl"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if res["error"] != "invalid_client_metadata" {
			t.Errorf("error = %v, want invalid_client_metadata", res["error"])
		}
	})
}
