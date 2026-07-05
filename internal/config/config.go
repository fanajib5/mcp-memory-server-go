package config

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Config holds all runtime configuration loaded from environment variables.
// Fields are resolved once at startup (with the same fallback rules the previous
// monolith applied) so the rest of the app depends on a single validated value.
type Config struct {
	DatabaseURL        string
	Port               string
	APIToken           string // MEMORY_API_TOKEN
	OAuthClientID      string
	OAuthClientSecret  string
	JWTSecret          string
	UIPassword         string
	CookieInsecure     bool
	PublicURL          string
	CORSAllowedOrigins string
}

// Load reads and validates environment variables, applying the fallback chain
// the previous main() used. It log.Fatals on missing critical values (fail-fast).
func Load() *Config {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("FATAL: DATABASE_URL env var is required")
	}

	token := os.Getenv("MEMORY_API_TOKEN")

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	oauthClientID := os.Getenv("OAUTH_CLIENT_ID")
	oauthClientSecret := os.Getenv("OAUTH_CLIENT_SECRET")
	if oauthClientID == "" || oauthClientSecret == "" {
		if token == "" {
			log.Fatal("FATAL: Set OAUTH_CLIENT_ID and OAUTH_CLIENT_SECRET, or MEMORY_API_TOKEN as fallback")
		}
		oauthClientID = token
		oauthClientSecret = token
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = token
	}
	if jwtSecret == "" {
		log.Fatal("FATAL: JWT_SECRET env var is required")
	}

	uiPassword := os.Getenv("UI_PASSWORD")
	if uiPassword == "" {
		uiPassword = token // fallback to MEMORY_API_TOKEN
	}

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		host := os.Getenv("HOST")
		if host == "" || host == "0.0.0.0" {
			host = "localhost"
		}
		publicURL = "https://" + host
		if port != "443" && port != "" {
			publicURL += ":" + port
		}
		log.Printf("WARNING: PUBLIC_URL not set, defaulting to %s. Set PUBLIC_URL in Coolify/deploy so OAuth metadata uses your real public domain.", publicURL)
	}

	return &Config{
		DatabaseURL:        dbURL,
		Port:               port,
		APIToken:           token,
		OAuthClientID:      oauthClientID,
		OAuthClientSecret:  oauthClientSecret,
		JWTSecret:          jwtSecret,
		UIPassword:         uiPassword,
		CookieInsecure:     os.Getenv("UI_COOKIE_INSECURE") == "true",
		PublicURL:          publicURL,
		CORSAllowedOrigins: os.Getenv("CORS_ALLOWED_ORIGINS"),
	}
}

// EntityTypes is the canonical list of registered entity types (mirrors the
// schema.sql seed + the UI selector).
func EntityTypes() []string {
	return []string{"project", "person", "decision", "tool", "concept", "place"}
}

// Describe returns a one-line summary useful for startup logging / debugging.
func (c *Config) Describe() string {
	return fmt.Sprintf("db=%s port=%s oauth=%s ui=%s",
		mask(c.DatabaseURL), c.Port, mask(c.OAuthClientID), enabled(c.UIPassword != ""))
}

func mask(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + "***" + s[len(s)-2:]
}

func enabled(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
