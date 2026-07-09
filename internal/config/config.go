package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
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
	OllamaURL          string // OLLAMA_URL, empty = semantic search off
	OllamaEmbedModel   string // OLLAMA_EMBED_MODEL
	BackupCron         string // BACKUP_CRON, empty = auto-backup disabled
	BackupDir          string // BACKUP_DIR
	BackupRetention    int    // BACKUP_RETENTION (file count)
	BackupOnStart      bool   // BACKUP_ON_START

	KiloGatewayAPIKey  string // KILO_GATEWAY_API_KEY, empty = LLM chat disabled
	KiloGatewayBaseURL string // KILO_GATEWAY_BASE_URL, default https://gateway.kilo.ai
	KiloGatewayModel   string // KILO_GATEWAY_DEFAULT_MODEL, default claude-sonnet-4-20250514
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

	ollamaURL := os.Getenv("OLLAMA_URL")
	ollamaModel := os.Getenv("OLLAMA_EMBED_MODEL")
	if ollamaModel == "" {
		ollamaModel = "bge-m3"
	}
	if ollamaURL != "" {
		log.Printf("semantic search enabled: ollama=%s model=%s", ollamaURL, ollamaModel)
	} else {
		log.Printf("OLLAMA_URL not set — semantic search disabled (lexical-only)")
	}

	backupCron := os.Getenv("BACKUP_CRON")
	backupDir := os.Getenv("BACKUP_DIR")
	if backupDir == "" {
		backupDir = "/data/backups"
	}
	backupRetention := 7
	if v := os.Getenv("BACKUP_RETENTION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			backupRetention = n
		}
	}
	if backupCron != "" {
		log.Printf("auto-backup enabled: cron=%s dir=%s retention=%d", backupCron, backupDir, backupRetention)
	}

	kiloKey := os.Getenv("KILO_GATEWAY_API_KEY")
	kiloBase := os.Getenv("KILO_GATEWAY_BASE_URL")
	if kiloBase == "" {
		kiloBase = "https://gateway.kilo.ai"
	}
	kiloModel := os.Getenv("KILO_GATEWAY_DEFAULT_MODEL")
	if kiloModel == "" {
		kiloModel = "claude-sonnet-4-20250514"
	}
	if kiloKey != "" {
		log.Printf("LLM chat enabled: gateway=%s model=%s", kiloBase, kiloModel)
	} else {
		log.Printf("KILO_GATEWAY_API_KEY not set — LLM chat disabled")
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
		OllamaURL:          ollamaURL,
		OllamaEmbedModel:   ollamaModel,
		BackupCron:         backupCron,
		BackupDir:          backupDir,
		BackupRetention:    backupRetention,
		BackupOnStart:      os.Getenv("BACKUP_ON_START") == "true",
		KiloGatewayAPIKey:  kiloKey,
		KiloGatewayBaseURL: kiloBase,
		KiloGatewayModel:   kiloModel,
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
