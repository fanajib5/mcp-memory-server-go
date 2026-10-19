// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func (s *OAuthService) HandleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
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
	_, _ = w.Write([]byte(`{"access_token":"` + tokenString + `","token_type":"Bearer","expires_in":3600,"scope":"` + scope + `"}`))
}
