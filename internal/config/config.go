// Package config loads application configuration from environment variables.
// A .env file in the project root is loaded automatically when present (development only).
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all application-wide configuration.
type Config struct {
	// Server
	Port string
	Env  string // "development" | "staging" | "production"

	// Database
	DatabaseURL string

	// Auth
	JWTSecret     string
	JWTExpiration string // e.g. "24h"

	// SMTP (email dispatch)
	SMTPHost     string
	SMTPPort     string
	SMTPFrom     string // sender address shown to the recipient
	SMTPUser     string // login user (often same as From)
	SMTPPassword string
}

// Load reads .env (if present) and returns a validated Config.
func Load() (*Config, error) {
	// Ignore error — .env is optional in production
	_ = godotenv.Load()

	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		Env:           getEnv("APP_ENV", "development"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		JWTExpiration: getEnv("JWT_EXPIRATION", "24h"),
		// SMTP
		SMTPHost:     getEnv("SMTP_HOST", "smtp.gmail.com"),
		SMTPPort:     getEnv("SMTP_PORT", "587"),
		SMTPFrom:     getEnv("SMTP_FROM", ""),
		SMTPUser:     getEnv("SMTP_USER", getEnv("SMTP_USERNAME", "")),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	
	// Add debug log to verify SMTP values loaded at startup
	fmt.Printf("[Config Debug] SMTP Host: %q, SMTP Port: %q, SMTP User: %q, SMTP From: %q, SMTP Password Length: %d\n",
		cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPFrom, len(cfg.SMTPPassword))

	return cfg, nil
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	return nil
}

// IsDevelopment returns true when running in dev mode.
func (c *Config) IsDevelopment() bool { return c.Env == "development" }

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
