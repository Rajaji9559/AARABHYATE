// Package handlers — shared HTTP helpers
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/aarabhyate/backend/internal/middleware"
)

// userIDFromCtx extracts the authenticated user's UUID from the request context.
// It reads the value injected by middleware.Auth or middleware.AdminMiddleware.
func userIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(middleware.CtxKeyUserID).(string)
	return v
}

// respondJSON writes a JSON response with the given status code.
func respondJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "encoding error", http.StatusInternalServerError)
	}
}

// respondError writes a JSON error envelope.
func respondError(w http.ResponseWriter, code int, message string) {
	respondJSON(w, code, map[string]string{"error": message})
}

// paginationParams extracts ?limit= and ?offset= query params with safe defaults.
func paginationParams(r *http.Request) (limit, offset int) {
	limit = 20
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}
