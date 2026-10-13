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

	"github.com/golang-jwt/jwt/v5"
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

func TestHandleOAuthAuthorize(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)

	t.Run("valid auth request redirects with code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=user&redirect_uri=https://claude.ai/oauth/callback&response_type=code&state=abc123", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302", w.Code)
		}
		loc := w.Header().Get("Location")
		if loc == "" {
			t.Fatal("missing Location header")
		}
		if !strings.Contains(loc, "https://claude.ai/oauth/callback?code=") {
			t.Errorf("redirect location = %q, missing code param", loc)
		}
		if !strings.Contains(loc, "&state=abc123") {
			t.Errorf("redirect location = %q, missing state param", loc)
		}
	})

	t.Run("invalid client returns error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=wrong&redirect_uri=https://claude.ai/oauth/callback&response_type=code", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

func TestHandleTokenAuthorizationCode(t *testing.T) {
	handler, secret := newOAuthTestEnv(t)

	t.Run("exchange valid auth code for jwt", func(t *testing.T) {
		authorizeReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=user&redirect_uri=https://claude.ai/oauth/callback&response_type=code", nil)
		authorizeW := httptest.NewRecorder()
		handler.ServeHTTP(authorizeW, authorizeReq)

		if authorizeW.Code != http.StatusFound {
			t.Fatalf("authorize status = %d, want 302", authorizeW.Code)
		}
		loc := authorizeW.Header().Get("Location")
		code := strings.TrimPrefix(loc, "https://claude.ai/oauth/callback?code=")
		code = strings.Split(code, "&")[0]

		body := strings.NewReader("grant_type=authorization_code&code=" + code + "&redirect_uri=https://claude.ai/oauth/callback&client_id=user&client_secret=pass")
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("token status = %d, want 200. body=%s", w.Code, w.Body.String())
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		accessToken, ok := res["access_token"].(string)
		if !ok || accessToken == "" {
			t.Fatal("access_token missing")
		}
		claims := &Claims{}
		_, err := jwt.ParseWithClaims(accessToken, claims, func(t *jwt.Token) (any, error) {
			return []byte(secret), nil
		})
		if err != nil {
			t.Fatalf("parse jwt: %v", err)
		}
		if claims.Scope != "mcp" {
			t.Errorf("scope claim = %q, want mcp", claims.Scope)
		}
	})

	t.Run("reused auth code rejected", func(t *testing.T) {
		authorizeReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=user&redirect_uri=https://claude.ai/oauth/callback&response_type=code", nil)
		authorizeW := httptest.NewRecorder()
		handler.ServeHTTP(authorizeW, authorizeReq)

		loc := authorizeW.Header().Get("Location")
		code := strings.TrimPrefix(loc, "https://claude.ai/oauth/callback?code=")
		code = strings.Split(code, "&")[0]

		bodyStr := "grant_type=authorization_code&code=" + code + "&redirect_uri=https://claude.ai/oauth/callback&client_id=user&client_secret=pass"

		req1 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(bodyStr))
		req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)

		req2 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(bodyStr))
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req2)

		if w1.Code != http.StatusOK {
			t.Fatalf("first exchange status = %d, want 200", w1.Code)
		}
		if w2.Code != http.StatusBadRequest {
			t.Fatalf("second exchange status = %d, want 400", w2.Code)
		}
	})
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

func TestHandleTokenClientCredentials(t *testing.T) {
	handler, secret := newOAuthTestEnv(t)

	t.Run("valid client credentials returns jwt", func(t *testing.T) {
		body := strings.NewReader("client_id=user&client_secret=pass")
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200. body=%s", w.Code, w.Body.String())
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if res["token_type"] != "Bearer" {
			t.Errorf("token_type = %q, want Bearer", res["token_type"])
		}
		if res["scope"] != "mcp" {
			t.Errorf("scope = %q, want mcp", res["scope"])
		}
		accessToken, ok := res["access_token"].(string)
		if !ok || accessToken == "" {
			t.Fatal("access_token missing")
		}
		claims := &Claims{}
		_, err := jwt.ParseWithClaims(accessToken, claims, func(t *jwt.Token) (any, error) {
			return []byte(secret), nil
		})
		if err != nil {
			t.Fatalf("parse jwt: %v", err)
		}
		if claims.Subject != "user" {
			t.Errorf("subject = %q, want user", claims.Subject)
		}
		if claims.Scope != "mcp" {
			t.Errorf("scope claim = %q, want mcp", claims.Scope)
		}
	})

	t.Run("invalid client returns error", func(t *testing.T) {
		body := strings.NewReader("client_id=user&client_secret=wrong")
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})
}
