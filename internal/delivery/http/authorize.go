// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"encoding/base64"
	"net/http"
	"time"
)

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
