// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"encoding/base64"
	"net/http"
	"slices"
	"time"
)

func (s *OAuthService) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	scope := r.URL.Query().Get("scope")
	responseType := r.URL.Query().Get("response_type")
	codeChallenge := r.URL.Query().Get("code_challenge")
	codeChallengeMethod := r.URL.Query().Get("code_challenge_method")

	if responseType != "code" {
		http.Error(w, `{"error":"unsupported_response_type"}`, http.StatusBadRequest)
		return
	}
	if clientID == "" || redirectURI == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	client := s.resolveClient(clientID)
	if client == nil {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
		return
	}
	if len(client.RedirectURIs) > 0 && !containsURI(client.RedirectURIs, redirectURI) {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if codeChallenge != "" {
		if codeChallengeMethod == "" {
			codeChallengeMethod = "plain"
		}
		if codeChallengeMethod != "plain" && codeChallengeMethod != "S256" {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
	}

	code := base64.RawURLEncoding.EncodeToString([]byte(randomState(32)))
	s.mu.Lock()
	s.codes[code] = &AuthCode{
		Code:                code,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		Scope:               scope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
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

func containsURI(list []string, want string) bool {
	return slices.Contains(list, want)
}
