// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package http

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwtRegisteredClaims builds RegisteredClaims with issued-at + expiry.
func jwtRegisteredClaims(iat, exp time.Time) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(iat),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
}

// mustSign signs claims with HS256 + secret, failing the test on error.
func mustSign(t *testing.T, claims *Claims, secret string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}
