package http

import (
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"mcp-memory-server/internal/config"
)

const (
	sessionCookieName = "mem_session"
	projectCookieName = "mem_project"
	csrfCookieName    = "mem_csrf"
	sessionMaxAge     = 30 * 24 * 3600
)

// corsMiddleware injects permissive CORS headers when allowedOrigins is set.
func corsMiddleware(allowedOrigins string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowedOrigins != "" {
			if allowedOrigins == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				allowed := strings.Split(allowedOrigins, ",")
				for _, o := range allowed {
					if strings.TrimSpace(o) == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Vary", "Origin")
						break
					}
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware gates /mcp. It accepts EITHER:
//  1. The raw static bearer token (MEMORY_API_TOKEN) — constant-time, no expiry.
//  2. A JWT issued via the OAuth flow (/oauth/token).
//
// If expectedStaticToken is empty, only JWTs are accepted.
func authMiddleware(cfg *config.Config, next http.Handler) http.Handler {
	var expected []byte
	if cfg.APIToken != "" {
		expected = []byte("Bearer " + cfg.APIToken)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if len(expected) > 0 && subtle.ConstantTimeCompare([]byte(auth), expected) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims := &Claims{}
		if _, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			return []byte(cfg.JWTSecret), nil
		}); err == nil {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, `{"error":"Invalid token"}`, http.StatusUnauthorized)
	})
}

// Session owns signed-cookie session + double-submit CSRF state for the web UI.
type Session struct {
	cfg *config.Config
}

func NewSession(cfg *config.Config) *Session {
	return &Session{cfg: cfg}
}

// signCookieValue produces "<expiry>.<base64url(HMAC-SHA256(secret, expiry))>".
func (s *Session) signCookieValue(expiry int64) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	fmt.Fprintf(mac, "%d", expiry)
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d.%s", expiry, sig)
}

func (s *Session) verifyCookieValue(v string) bool {
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return false
	}
	var expiry int64
	if _, err := fmt.Sscanf(parts[0], "%d", &expiry); err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	fmt.Fprintf(mac, "%d", expiry)
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return false
	}
	return expiry > time.Now().Unix()
}

func (s *Session) SessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || !s.verifyCookieValue(c.Value) {
			http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Auth wraps a HandlerFunc with session auth.
func (s *Session) Auth(h http.HandlerFunc) http.Handler { return s.SessionAuth(h) }

func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano()) // extremely unlikely fallback
	}
	return hex.EncodeToString(b)
}

func csrfToken(r *http.Request) string {
	if c, err := r.Cookie(csrfCookieName); err == nil {
		return c.Value
	}
	return ""
}

// CSRFCheck enforces double-submit CSRF: the mem_csrf cookie must equal the form "csrf" field.
func (s *Session) CSRFCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(csrfCookieName)
		if err != nil || c.Value == "" {
			http.Error(w, "csrf: missing token", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
		}
		if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(c.Value)) != 1 {
			http.Error(w, "csrf: mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CSRFAuth wraps a mutating HandlerFunc with session + CSRF checks.
func (s *Session) CSRFAuth(h http.HandlerFunc) http.Handler { return s.SessionAuth(s.CSRFCheck(h)) }
