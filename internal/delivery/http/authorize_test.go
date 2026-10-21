// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleOAuthAuthorize(t *testing.T) {
	handler, _ := newOAuthTestEnv(t)

	t.Run("valid auth request redirects with code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=user&redirect_uri=https://claude.ai/oauth/callback&response_type=code&state=abc123", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302", w.Code)
		}
		loc := w.Header().Get("Location")
		if loc == "" {
			t.Fatal("missing Location header")
		}
		if !strings.Contains(loc, "https://claude.ai/oauth/callback?code=") {
			t.Errorf("redirect location = %q, missing code param", loc)
		}
		if !strings.Contains(loc, "&state=abc123") {
			t.Errorf("redirect location = %q, missing state param", loc)
		}
	})

	t.Run("invalid client returns error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=wrong&redirect_uri=https://claude.ai/oauth/callback&response_type=code", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}
