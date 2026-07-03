package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newOAuthTestEnv(t *testing.T) (http.Handler, string) {
	t.Helper()
	jwtSecret = "test-secret"
	handler := http.NewServeMux()
	base := "https://example.com"
	handler.HandleFunc("/.well-known/oauth-authorization-server", handleOAuthMetadata(base))
	handler.HandleFunc("/oauth/token", handleToken)
	handler.HandleFunc("/oauth/register", handleRegister)
	return handler, jwtSecret
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
	if len(meta.GrantTypesSupported) != 1 || meta.GrantTypesSupported[0] != "client_credentials" {
		t.Errorf("grant_types = %v, want [client_credentials]", meta.GrantTypesSupported)
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
		os.Setenv("OAUTH_CLIENT_ID", "user")
		os.Setenv("OAUTH_CLIENT_SECRET", "pass")
		defer os.Unsetenv("OAUTH_CLIENT_ID")
		defer os.Unsetenv("OAUTH_CLIENT_SECRET")

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
		os.Setenv("OAUTH_CLIENT_ID", "user")
		os.Setenv("OAUTH_CLIENT_SECRET", "pass")
		defer os.Unsetenv("OAUTH_CLIENT_ID")
		defer os.Unsetenv("OAUTH_CLIENT_SECRET")

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

func TestAuthMiddleware(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(inner))
	mux.Handle("/.well-known/oauth-authorization-server", handler)
	mux.Handle("/oauth/token", handler)

	t.Run("valid token passes through", func(t *testing.T) {
		now := time.Now()
		claims := &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				IssuedAt:  jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			},
			Scope: "mcp",
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte("test-secret"))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/mcp", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+signed)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
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

	t.Run("invalid token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp", http.NoBody)
		req.Header.Set("Authorization", "Bearer invalid-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})
}
