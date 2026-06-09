// Package database provides the PostgreSQL connection pool initialisation
// using sqlx (a thin extension over database/sql).
//
// Environment variables:
//
//	DATABASE_URL  - full DSN e.g. postgres://user:pass@host:5432/dbname?sslmode=disable
//	DB_MAX_OPEN   - max open connections (default 25)
//	DB_MAX_IDLE   - max idle connections (default 5)
//	DB_CONN_TTL   - connection max lifetime e.g. "5m" (default "5m")
package database

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // PostgreSQL driver — blank import registers it
)

// NewPostgres creates and validates a *sqlx.DB connection pool.
// The pool is configured for a typical high-throughput API server.
func NewPostgres(dsn string) (*sqlx.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("database: DSN must not be empty")
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("database: connect failed: %w", err)
	}

	// ── Pool tuning ───────────────────────────────────────────────────────────
	db.SetMaxOpenConns(envInt("DB_MAX_OPEN", 25))
	db.SetMaxIdleConns(envInt("DB_MAX_IDLE", 5))
	db.SetConnMaxLifetime(envDuration("DB_CONN_TTL", 5*time.Minute))

	// ── Verify connectivity ───────────────────────────────────────────────────
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("database: ping failed: %w", err)
	}

	slog.Info("database pool ready",
		"max_open", db.Stats().MaxOpenConnections,
		"driver", "postgres",
	)
	return db, nil
}

// envInt reads an integer environment variable with a fallback default.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// envDuration reads a duration environment variable with a fallback default.
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
