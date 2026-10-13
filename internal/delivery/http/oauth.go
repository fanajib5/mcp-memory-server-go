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
