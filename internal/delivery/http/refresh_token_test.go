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
	"time"

	"github.com/golang-jwt/jwt/v5"

	"mcp-memory-server/internal/config"
)

func doAuthCodeFlow(t *testing.T, handler http.Handler, clientID, clientSecret string) map[string]any {
	t.Helper()
	authorizeReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id="+clientID+"&redirect_uri=https://claude.ai/oauth/callback&response_type=code", nil)
	authorizeW := httptest.NewRecorder()
	handler.ServeHTTP(authorizeW, authorizeReq)
	if authorizeW.Code != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302", authorizeW.Code)
	}
	loc := authorizeW.Header().Get("Location")
	code := strings.TrimPrefix(loc, "https://claude.ai/oauth/callback?code=")
	code = strings.Split(code, "&")[0]

	body := strings.NewReader("grant_type=authorization_code&code=" + code + "&redirect_uri=https://claude.ai/oauth/callback&client_id=" + clientID + "&client_secret=" + clientSecret)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("token status = %d, want 200. body=%s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return res
}

func postToken(t *testing.T, handler http.Handler, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestAuthorizationCodeExchangeIssuesRefreshToken(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)
	res := doAuthCodeFlow(t, handler, "user", "pass")

	refresh, ok := res["refresh_token"].(string)
	if !ok || refresh == "" {
		t.Fatalf("refresh_token missing from token response: %v", res)
	}
	if res["expires_in"] == nil {
		t.Fatal("expires_in missing from token response")
	}
}

func TestRefreshTokenGrant(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)

	t.Run("valid refresh token returns new access and refresh tokens", func(t *testing.T) {
		res := doAuthCodeFlow(t, handler, "user", "pass")
		refresh, _ := res["refresh_token"].(string)

		w := postToken(t, handler, "grant_type=refresh_token&refresh_token="+refresh+"&client_id=user")
		if w.Code != http.StatusOK {
			t.Fatalf("refresh status = %d, want 200. body=%s", w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out["access_token"] == nil || out["access_token"] == "" {
			t.Fatal("access_token missing from refresh response")
		}
		if out["refresh_token"] == nil || out["refresh_token"] == "" {
			t.Fatal("refresh_token missing from refresh response (rotation)")
		}
	})

	t.Run("expired refresh token rejected", func(t *testing.T) {
		expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":   "user",
			"scope": "mcp",
			"use":   "refresh",
			"exp":   time.Now().Add(-time.Hour).Unix(),
		})
		signed, err := expired.SignedString([]byte("test-secret"))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		w := postToken(t, handler, "grant_type=refresh_token&refresh_token="+signed+"&client_id=user")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expired refresh status = %d, want 400. body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("access token rejected as refresh token", func(t *testing.T) {
		res := doAuthCodeFlow(t, handler, "user", "pass")
		access, _ := res["access_token"].(string)
		w := postToken(t, handler, "grant_type=refresh_token&refresh_token="+access+"&client_id=user")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("access-as-refresh status = %d, want 400. body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("client_id mismatch rejected", func(t *testing.T) {
		res := doAuthCodeFlow(t, handler, "user", "pass")
		refresh, _ := res["refresh_token"].(string)
		w := postToken(t, handler, "grant_type=refresh_token&refresh_token="+refresh+"&client_id=someone-else")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("mismatch status = %d, want 400. body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("missing refresh token rejected", func(t *testing.T) {
		w := postToken(t, handler, "grant_type=refresh_token&client_id=user")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("missing token status = %d, want 400. body=%s", w.Code, w.Body.String())
		}
	})
}

func TestRefreshSurvivesRestart(t *testing.T) {
	cfg := testConfig()
	buildHandler := func() http.Handler {
		oauth := NewOAuthService(cfg)
		mux := http.NewServeMux()
		mux.HandleFunc("/oauth/authorize", oauth.HandleAuthorize)
		mux.HandleFunc("/oauth/token", oauth.HandleToken)
		return mux
	}

	handler1 := buildHandler()
	res := doAuthCodeFlow(t, handler1, "user", "pass")
	refresh, _ := res["refresh_token"].(string)

	handler2 := buildHandler()
	w := postToken(t, handler2, "grant_type=refresh_token&refresh_token="+refresh+"&client_id=user")
	if w.Code != http.StatusOK {
		t.Fatalf("refresh after restart status = %d, want 200. body=%s", w.Code, w.Body.String())
	}
}

func TestUnsupportedGrantType(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)
	w := postToken(t, handler, "grant_type=password&client_id=user&client_secret=pass")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body=%s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res["error"] != "unsupported_grant_type" {
		t.Errorf("error = %v, want unsupported_grant_type", res["error"])
	}
}

func TestMetadataAdvertisesRefreshAndPKCE(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var meta map[string]any
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	grants, _ := meta["grant_types_supported"].([]any)
	foundRefresh := false
	for _, g := range grants {
		if g == "refresh_token" {
			foundRefresh = true
		}
	}
	if !foundRefresh {
		t.Errorf("grant_types = %v, want to include refresh_token", meta["grant_types_supported"])
	}
	methods, _ := meta["code_challenge_methods_supported"].([]any)
	foundS256 := false
	for _, m := range methods {
		if m == "S256" {
			foundS256 = true
		}
	}
	if !foundS256 {
		t.Errorf("code_challenge_methods_supported = %v, want to include S256", meta["code_challenge_methods_supported"])
	}
}

func TestRefreshTokenRejectedAsBearer(t *testing.T) {
	handler, secret := newOAuthTestEnv(t)
	res := doAuthCodeFlow(t, handler, "user", "pass")
	refresh, _ := res["refresh_token"].(string)
	access, _ := res["access_token"].(string)
	if refresh == "" {
		t.Fatal("refresh_token missing; precondition for this test")
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	authed := authMiddleware(testConfig(), next)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+refresh)
	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("refresh-as-bearer status = %d, want 401", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req2.Header.Set("Authorization", "Bearer "+access)
	w2 := httptest.NewRecorder()
	authed.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("access token status = %d, want 200 (secret=%q)", w2.Code, secret)
	}
}

func TestTokenTTLConfigurable(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/dummy")
	t.Setenv("MEMORY_API_TOKEN", "test-secret")
	t.Setenv("OAUTH_CLIENT_ID", "user")
	t.Setenv("OAUTH_CLIENT_SECRET", "pass")
	t.Setenv("OAUTH_ACCESS_TOKEN_TTL", "7200")
	t.Setenv("OAUTH_REFRESH_TOKEN_TTL", "86400")
	cfg := config.Load()

	oauth := NewOAuthService(cfg)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/authorize", oauth.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauth.HandleToken)

	res := doAuthCodeFlow(t, mux, "user", "pass")

	if res["expires_in"] != float64(7200) {
		t.Errorf("expires_in = %v, want 7200", res["expires_in"])
	}

	now := time.Now()
	checkExp := func(token, use string, want time.Duration) {
		t.Helper()
		claims := &Claims{}
		if _, err := jwt.ParseWithClaims(token, claims, func(tok *jwt.Token) (any, error) {
			return []byte("test-secret"), nil
		}); err != nil {
			t.Fatalf("parse %s token: %v", use, err)
		}
		if claims.Use != use {
			t.Errorf("%s token use = %q, want %q", use, claims.Use, use)
		}
		if claims.ExpiresAt == nil {
			t.Fatalf("%s token missing exp", use)
		}
		got := claims.ExpiresAt.Time.Sub(now)
		if got < want-time.Minute || got > want+time.Minute {
			t.Errorf("%s token ttl = %v, want ~%v", use, got, want)
		}
	}
	access, _ := res["access_token"].(string)
	refresh, _ := res["refresh_token"].(string)
	checkExp(access, "access", 2*time.Hour)
	checkExp(refresh, "refresh", 24*time.Hour)
}
