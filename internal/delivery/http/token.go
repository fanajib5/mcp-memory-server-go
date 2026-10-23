// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	tokenUseAccess  = "access"
	tokenUseRefresh = "refresh"

	defaultAccessTokenTTL  = 24 * time.Hour
	defaultRefreshTokenTTL = 30 * 24 * time.Hour
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (s *OAuthService) HandleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	switch r.FormValue("grant_type") {
	case "authorization_code":
		s.tokenFromAuthCode(w, r)
	case "refresh_token":
		s.tokenFromRefreshToken(w, r)
	case "client_credentials", "":
		s.tokenFromClientCredentials(w, r)
	default:
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
	}
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

	client := s.resolveClient(clientID)
	if client == nil || !clientSecretMatches(client, clientSecret) || stored.ClientID != clientID {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	if !verifyPKCE(stored, r.FormValue("code_verifier")) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	s.issueToken(w, stored.ClientID, stored.Scope)
}

func (s *OAuthService) tokenFromRefreshToken(w http.ResponseWriter, r *http.Request) {
	raw := r.FormValue("refresh_token")
	if raw == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil || !parsed.Valid || claims.Use != tokenUseRefresh {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	clientID := r.FormValue("client_id")
	if clientID == "" && r.Header.Get("Authorization") != "" {
		clientID, _ = parseBasicAuth(r.Header.Get("Authorization"))
	}
	if clientID != "" && clientID != claims.Subject {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	s.issueToken(w, claims.Subject, claims.Scope)
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
	accessTTL := s.cfg.AccessTokenTTL
	if accessTTL <= 0 {
		accessTTL = defaultAccessTokenTTL
	}
	refreshTTL := s.cfg.RefreshTokenTTL
	if refreshTTL <= 0 {
		refreshTTL = defaultRefreshTokenTTL
	}

	accessJWT, err := s.signToken(subject, scope, tokenUseAccess, accessTTL)
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}
	refreshJWT, err := s.signToken(subject, scope, tokenUseRefresh, refreshTTL)
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:  accessJWT,
		TokenType:    "Bearer",
		ExpiresIn:    int64(accessTTL / time.Second),
		Scope:        scope,
		RefreshToken: refreshJWT,
	})
}

func (s *OAuthService) signToken(subject, scope, use string, ttl time.Duration) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   subject,
		},
		Scope: scope,
		Use:   use,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}
