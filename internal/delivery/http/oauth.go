// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"crypto/rand"
	"crypto/sha256"
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
	Code                string
	ClientID            string
	RedirectURI         string
	ExpiresAt           time.Time
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
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

// ProtectedResourceMetadata is the RFC 9728 document describing this resource
// server so OAuth clients can locate the authorization server.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ResourceName           string   `json:"resource_name,omitempty"`
}

// RegisteredClient is a client created via RFC 7591 dynamic client registration.
type RegisteredClient struct {
	ClientID                string
	ClientSecret            string
	RedirectURIs            []string
	TokenEndpointAuthMethod string
}

type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// OAuthService issues authorization codes and JWT access tokens for MCP clients.
type OAuthService struct {
	cfg     *config.Config
	codes   map[string]*AuthCode
	clients map[string]*RegisteredClient
	mu      sync.Mutex
}

func NewOAuthService(cfg *config.Config) *OAuthService {
	return &OAuthService{
		cfg:     cfg,
		codes:   make(map[string]*AuthCode),
		clients: make(map[string]*RegisteredClient),
	}
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
		TokenEndpointAuthMethods: []string{"client_secret_basic", "client_secret_post", "none"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(metadata)
}

// HandleProtectedResourceMetadata serves RFC 9728 protected resource metadata,
// both at the root well-known URI and path-aware (RFC 9728 section 3.1): a
// request for /.well-known/oauth-protected-resource/mcp advertises resource
// PublicURL + "/mcp" so it matches the URL the client called.
func (s *OAuthService) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	const prefix = "/.well-known/oauth-protected-resource"
	suffix := strings.TrimPrefix(r.URL.Path, prefix)
	if suffix == "" || suffix == "/" {
		suffix = "/mcp"
	}
	metadata := ProtectedResourceMetadata{
		Resource:               s.cfg.PublicURL + suffix,
		AuthorizationServers:   []string{s.cfg.PublicURL},
		ScopesSupported:        []string{"mcp"},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "mcp-memory-server",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(metadata)
}

// HandleRegister implements RFC 7591 dynamic client registration: the client
// posts metadata and the server generates the credentials.
func (s *OAuthService) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req registrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_client_metadata"}`, http.StatusBadRequest)
		return
	}
	if len(req.RedirectURIs) == 0 {
		http.Error(w, `{"error":"invalid_client_metadata"}`, http.StatusBadRequest)
		return
	}
	method := req.TokenEndpointAuthMethod
	if method == "" {
		method = "client_secret_basic"
	}
	switch method {
	case "client_secret_basic", "client_secret_post", "none":
	default:
		http.Error(w, `{"error":"invalid_client_metadata"}`, http.StatusBadRequest)
		return
	}

	client := &RegisteredClient{
		ClientID:                "mcp-" + randomState(16),
		RedirectURIs:            req.RedirectURIs,
		TokenEndpointAuthMethod: method,
	}
	if method != "none" {
		client.ClientSecret = randomState(32)
	}

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}

	s.mu.Lock()
	s.clients[client.ClientID] = client
	s.mu.Unlock()

	resp := registrationResponse{
		ClientID:                client.ClientID,
		ClientSecret:            client.ClientSecret,
		ClientIDIssuedAt:        time.Now().Unix(),
		ClientSecretExpiresAt:   0,
		RedirectURIs:            req.RedirectURIs,
		ClientName:              req.ClientName,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		TokenEndpointAuthMethod: method,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
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

// resolveClient looks up a dynamically registered client, falling back to the
// built-in client from config (any redirect URI allowed for that one).
func (s *OAuthService) resolveClient(clientID string) *RegisteredClient {
	if clientID == "" {
		return nil
	}
	s.mu.Lock()
	registered, ok := s.clients[clientID]
	s.mu.Unlock()
	if ok {
		return registered
	}
	if clientID == s.cfg.OAuthClientID {
		return &RegisteredClient{
			ClientID:                s.cfg.OAuthClientID,
			ClientSecret:            s.cfg.OAuthClientSecret,
			TokenEndpointAuthMethod: "client_secret_post",
		}
	}
	return nil
}

func clientSecretMatches(client *RegisteredClient, clientSecret string) bool {
	if client.TokenEndpointAuthMethod == "none" {
		return true
	}
	return clientSecret != "" && clientSecret == client.ClientSecret
}

func verifyPKCE(stored *AuthCode, verifier string) bool {
	if stored.CodeChallenge == "" {
		return true
	}
	if verifier == "" {
		return false
	}
	switch stored.CodeChallengeMethod {
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:]) == stored.CodeChallenge
	default:
		return verifier == stored.CodeChallenge
	}
}
