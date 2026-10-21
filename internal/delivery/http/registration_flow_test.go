// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestRegisteredClientPKCEFlow(t *testing.T) {
	handler, secret := newOAuthTestEnv(t)

	register := func(body string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("register status = %d, want 201. body=%s", w.Code, w.Body.String())
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return res
	}

	authorize := func(clientID, redirectURI, challenge, verifier string) *httptest.ResponseRecorder {
		t.Helper()
		authURL := "/oauth/authorize?client_id=" + url.QueryEscape(clientID) +
			"&redirect_uri=" + url.QueryEscape(redirectURI) +
			"&response_type=code&state=xyz&code_challenge=" + challenge + "&code_challenge_method=S256"
		req := httptest.NewRequest(http.MethodGet, authURL, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	codeFrom := func(w *httptest.ResponseRecorder, redirectURI string) string {
		t.Helper()
		prefix := redirectURI + "?code="
		loc := w.Header().Get("Location")
		code := strings.TrimPrefix(loc, prefix)
		return strings.Split(code, "&")[0]
	}

	exchange := func(form string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	verifier := "test-code-verifier-0123456789abcdef"
	challenge := s256Challenge(verifier)
	cb := "https://client.example/cb"

	t.Run("registered client completes code flow with PKCE", func(t *testing.T) {
		reg := register(`{"redirect_uris":["https://client.example/cb"],"client_name":"grok","token_endpoint_auth_method":"client_secret_post"}`)
		clientID, _ := reg["client_id"].(string)
		clientSecret, _ := reg["client_secret"].(string)
		if clientID == "" || clientSecret == "" {
			t.Fatal("registration did not return credentials")
		}

		authW := authorize(clientID, cb, challenge, verifier)
		if authW.Code != http.StatusFound {
			t.Fatalf("authorize status = %d, want 302", authW.Code)
		}
		loc := authW.Header().Get("Location")
		if !strings.Contains(loc, "&state=xyz") {
			t.Errorf("redirect location = %q, missing state", loc)
		}
		code := codeFrom(authW, cb)

		w := exchange("grant_type=authorization_code&code=" + code +
			"&redirect_uri=" + url.QueryEscape(cb) +
			"&client_id=" + url.QueryEscape(clientID) +
			"&client_secret=" + url.QueryEscape(clientSecret) +
			"&code_verifier=" + verifier)
		if w.Code != http.StatusOK {
			t.Fatalf("token status = %d, want 200. body=%s", w.Code, w.Body.String())
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		accessToken, _ := res["access_token"].(string)
		if accessToken == "" {
			t.Fatal("access_token missing")
		}
		claims := &Claims{}
		if _, err := jwt.ParseWithClaims(accessToken, claims, func(t *jwt.Token) (any, error) {
			return []byte(secret), nil
		}); err != nil {
			t.Fatalf("parse jwt: %v", err)
		}
		if claims.Subject != clientID {
			t.Errorf("subject = %q, want %q", claims.Subject, clientID)
		}
	})

	t.Run("wrong code verifier rejected", func(t *testing.T) {
		reg := register(`{"redirect_uris":["https://client.example/cb"],"client_name":"grok2","token_endpoint_auth_method":"client_secret_post"}`)
		clientID, _ := reg["client_id"].(string)
		clientSecret, _ := reg["client_secret"].(string)

		authW := authorize(clientID, cb, challenge, verifier)
		code := codeFrom(authW, cb)

		w := exchange("grant_type=authorization_code&code=" + code +
			"&redirect_uri=" + url.QueryEscape(cb) +
			"&client_id=" + url.QueryEscape(clientID) +
			"&client_secret=" + url.QueryEscape(clientSecret) +
			"&code_verifier=wrong-verifier-aaaaaaaaaaaaaaaa")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("token status = %d, want 400. body=%s", w.Code, w.Body.String())
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if res["error"] != "invalid_grant" {
			t.Errorf("error = %v, want invalid_grant", res["error"])
		}
	})

	t.Run("unregistered redirect_uri rejected", func(t *testing.T) {
		reg := register(`{"redirect_uris":["https://client.example/cb"],"client_name":"grok3","token_endpoint_auth_method":"client_secret_post"}`)
		clientID, _ := reg["client_id"].(string)

		w := authorize(clientID, "https://evil.example/cb", challenge, verifier)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("authorize status = %d, want 400", w.Code)
		}
	})

	t.Run("public client completes flow without secret", func(t *testing.T) {
		reg := register(`{"redirect_uris":["https://client.example/cb"],"client_name":"grok-public","token_endpoint_auth_method":"none"}`)
		clientID, _ := reg["client_id"].(string)

		authW := authorize(clientID, cb, challenge, verifier)
		if authW.Code != http.StatusFound {
			t.Fatalf("authorize status = %d, want 302", authW.Code)
		}
		code := codeFrom(authW, cb)

		w := exchange("grant_type=authorization_code&code=" + code +
			"&redirect_uri=" + url.QueryEscape(cb) +
			"&client_id=" + url.QueryEscape(clientID) +
			"&code_verifier=" + verifier)
		if w.Code != http.StatusOK {
			t.Fatalf("token status = %d, want 200. body=%s", w.Code, w.Body.String())
		}
	})
}
