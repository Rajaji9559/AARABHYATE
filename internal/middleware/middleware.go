// Package middleware provides reusable HTTP middleware for the Aarabhyate API.
// This file contains JWT authentication and role-based authorization middleware.
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// contextKey is an unexported type used for middleware context value keys.
// Using a private type prevents external packages from accidentally colliding
// with the same key string.
type contextKey string

const (
	// CtxKeyUserID holds the authenticated user's UUID in the request context.
	CtxKeyUserID contextKey = "user_id"

	// CtxKeyRole holds the authenticated user's role string in the request context.
	CtxKeyRole contextKey = "role"

	// CtxKeyEmail holds the authenticated user's email in the request context.
	CtxKeyEmail contextKey = "email"
)

// =============================================================================
// Auth — JWT verification
// =============================================================================

// Auth validates the Bearer JWT token present in the Authorization header.
// On success it injects the user's ID, email, and role into the request context.
// On failure it responds 401 Unauthorized immediately.
//
// Usage (chi):
//
//	r.Group(func(r chi.Router) {
//	    r.Use(middleware.Auth(cfg.JWTSecret))
//	    r.Get("/me", h.User.GetMe)
//	})
func Auth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr, err := extractBearerToken(r)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, `{"error":"missing or malformed Authorization header"}`)
				return
			}

			claims, err := parseJWT(tokenStr, jwtSecret)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, fmt.Sprintf(`{"error":"%s"}`, err.Error()))
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, CtxKeyUserID, claims["sub"].(string))
			ctx = context.WithValue(ctx, CtxKeyRole, claims["role"].(string))
			if email, ok := claims["email"].(string); ok {
				ctx = context.WithValue(ctx, CtxKeyEmail, email)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// =============================================================================
// AdminMiddleware — role-based access control
// =============================================================================

// AdminMiddleware is a standalone middleware that:
//  1. Extracts the Bearer token from the Authorization header.
//  2. Verifies and parses the JWT signature and expiry.
//  3. Checks that the embedded "role" claim equals "admin".
//  4. Blocks the request with 403 Forbidden if any check fails.
//
// It is intentionally self-contained — it does NOT rely on the Auth middleware
// being applied first — making it usable as a standalone guard on admin groups.
//
// Usage (chi):
//
//	r.Group(func(r chi.Router) {
//	    r.Use(middleware.AdminMiddleware(cfg.JWTSecret))
//	    r.Get("/admin/users", h.User.ListUsers)
//	})
func AdminMiddleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ── Step 1: extract token ─────────────────────────────────────────
			tokenStr, err := extractBearerToken(r)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized,
					`{"error":"unauthorized: missing or malformed Authorization header"}`)
				return
			}

			// ── Step 2: verify signature & expiry ────────────────────────────
			claims, err := parseJWT(tokenStr, jwtSecret)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized,
					fmt.Sprintf(`{"error":"unauthorized: %s"}`, err.Error()))
				return
			}

			// ── Step 3: enforce admin role ────────────────────────────────────
			role, _ := claims["role"].(string)
			if role != "admin" {
				writeJSON(w, http.StatusForbidden,
					`{"error":"forbidden: admin access required"}`)
				return
			}

			// ── Inject claims into context and continue ───────────────────────
			ctx := r.Context()
			ctx = context.WithValue(ctx, CtxKeyUserID, claims["sub"].(string))
			ctx = context.WithValue(ctx, CtxKeyRole, role)
			if email, ok := claims["email"].(string); ok {
				ctx = context.WithValue(ctx, CtxKeyEmail, email)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminOnly is a lightweight companion middleware that assumes Auth has already
// run (i.e. role is already in context). Use AdminMiddleware if Auth is absent.
//
// Usage (chi — layered after Auth):
//
//	r.Group(func(r chi.Router) {
//	    r.Use(middleware.Auth(cfg.JWTSecret))
//	    r.Use(middleware.AdminOnly)
//	    r.Delete("/admin/users/{id}", h.User.DeleteUser)
//	})
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(CtxKeyRole).(string)
		if role != "admin" {
			writeJSON(w, http.StatusForbidden, `{"error":"forbidden: admin access required"}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// =============================================================================
// Logger — structured request logger
// =============================================================================

// Logger is a minimal structured request logger middleware.
// In production, prefer chi's built-in middleware.Logger which integrates with slog.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rw, r)
		_ = start // extend: log method, path, status, latency here
	})
}

// loggingResponseWriter wraps http.ResponseWriter to capture the status code.
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *loggingResponseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// =============================================================================
// Internal helpers
// =============================================================================

// extractBearerToken pulls the raw token string from the Authorization header.
// Returns an error if the header is absent or not in "Bearer <token>" format.
func extractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("Authorization header is missing")
	}
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", fmt.Errorf("Authorization header must start with 'Bearer '")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		return "", fmt.Errorf("token is empty")
	}
	return token, nil
}

// parseJWT validates the token string and returns the decoded MapClaims.
func parseJWT(tokenStr, secret string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		// Enforce HMAC signing — reject RSA/ECDSA tokens to prevent algorithm confusion attacks.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid or expired token")
	}
	if !token.Valid {
		return nil, fmt.Errorf("token is not valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("could not parse token claims")
	}
	return claims, nil
}

// writeJSON writes a pre-formatted JSON string response — avoids encoding overhead
// for the small, fixed error payloads used in middleware.
func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
