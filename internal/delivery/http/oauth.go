// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"mcp-memory-server/internal/config"
)

type AuthCode struct {
	Code        string
	ClientID    string
	RedirectURI string
	ExpiresAt   time.Time
	Scope       string
}

type Claims struct {
	jwt.RegisteredClaims
	Scope string `json:"scope,omitempty"`
}

type OAuthMetadata struct {
	Issuer                   string   `json:"issuer"`
	AuthorizationEndpoint    string   `json:"authorization_endpoint"`
	TokenEndpoint            string   `json:"token_endpoint"`
	RegistrationEndpoint     string   `json:"registration_endpoint,omitempty"`
	ScopesSupported          []string `json:"scopes_supported"`
	ResponseTypesSupported   []string `json:"response_types_supported"`
	GrantTypesSupported      []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

// OAuthService issues authorization codes and JWT access tokens for MCP clients.
type OAuthService struct {
	cfg   *config.Config
	codes map[string]*AuthCode
	mu    sync.Mutex
}

func NewOAuthService(cfg *config.Config) *OAuthService {
	return &OAuthService{cfg: cfg, codes: make(map[string]*AuthCode)}
}

func randomState(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *OAuthService) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	baseURL := s.cfg.PublicURL
	metadata := OAuthMetadata{
		Issuer:                   baseURL,
		AuthorizationEndpoint:    baseURL + "/oauth/authorize",
		TokenEndpoint:            baseURL + "/oauth/token",
		RegistrationEndpoint:     baseURL + "/oauth/register",
		ScopesSupported:          []string{"mcp"},
		ResponseTypesSupported:   []string{"code"},
		GrantTypesSupported:      []string{"authorization_code", "client_credentials"},
		TokenEndpointAuthMethods: []string{"client_secret_basic", "client_secret_post"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

func (s *OAuthService) HandleRegister(w http.ResponseWriter, r *http.Request) {
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	if clientID == "" || clientSecret == "" {
		http.Error(w, "Missing client_id or client_secret", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{
		"client_id": "` + clientID + `",
		"client_secret": "` + clientSecret + `",
		"grant_types": ["client_credentials"],
		"token_endpoint_auth_method": "client_secret_post"
	}`))
}

func (s *OAuthService) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	scope := r.URL.Query().Get("scope")
	responseType := r.URL.Query().Get("response_type")

	if responseType != "code" {
		http.Error(w, `{"error":"unsupported_response_type"}`, http.StatusBadRequest)
		return
	}
	if clientID == "" || redirectURI == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	if clientID != s.cfg.OAuthClientID {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
		return
	}

	code := base64.RawURLEncoding.EncodeToString([]byte(randomState(32)))
	s.mu.Lock()
	s.codes[code] = &AuthCode{
		Code:        code,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		Scope:       scope,
	}
	if s.codes[code].Scope == "" {
		s.codes[code].Scope = "mcp"
	}
	s.mu.Unlock()

	redirectTarget := redirectURI + "?code=" + code
	if state != "" {
		redirectTarget += "&state=" + state
	}
	http.Redirect(w, r, redirectTarget, http.StatusFound)
}

func (s *OAuthService) HandleToken(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	grantType := r.FormValue("grant_type")

	if grantType == "authorization_code" {
		s.tokenFromAuthCode(w, r)
		return
	}
	s.tokenFromClientCredentials(w, r)
}

func (s *OAuthService) tokenFromAuthCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	if clientID == "" && r.Header.Get("Authorization") != "" {
		clientID, clientSecret = parseBasicAuth(r.Header.Get("Authorization"))
	}

	s.mu.Lock()
	stored, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	s.mu.Unlock()

	if !ok || stored == nil || stored.ExpiresAt.Before(time.Now()) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	if redirectURI != "" && stored.RedirectURI != redirectURI {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	if clientID != s.cfg.OAuthClientID || clientSecret != s.cfg.OAuthClientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}

	s.issueToken(w, stored.ClientID, stored.Scope)
}

func (s *OAuthService) tokenFromClientCredentials(w http.ResponseWriter, r *http.Request) {
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	if clientID == "" && r.Header.Get("Authorization") != "" {
		clientID, clientSecret = parseBasicAuth(r.Header.Get("Authorization"))
	}

	if clientID != s.cfg.OAuthClientID || clientSecret != s.cfg.OAuthClientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}

	s.issueToken(w, clientID, "mcp")
}

func (s *OAuthService) issueToken(w http.ResponseWriter, subject, scope string) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   subject,
		},
		Scope: scope,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"access_token":"` + tokenString + `","token_type":"Bearer","expires_in":3600,"scope":"` + scope + `"}`))
}

func parseBasicAuth(header string) (clientID, clientSecret string) {
	auth := strings.TrimPrefix(header, "Basic ")
	if decoded, err := base64.StdEncoding.DecodeString(auth); err == nil {
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	return "", ""
}
