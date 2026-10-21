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

	"github.com/golang-jwt/jwt/v5"
)

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
