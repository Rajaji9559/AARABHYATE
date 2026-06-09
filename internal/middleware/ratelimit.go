// Package middleware — IP-based rate limiter using the token-bucket algorithm.
//
// This file implements a per-IP rate limiter powered by golang.org/x/time/rate.
// Each IP address gets its own token bucket. A background goroutine cleans up
// buckets for IPs that have been idle for more than the eviction window to
// prevent unbounded memory growth.
package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipLimiter holds the rate.Limiter and the last-seen timestamp for an IP.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter manages per-IP token buckets and the cleanup ticker.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*ipLimiter

	// Token bucket parameters
	rps   rate.Limit // tokens added per second
	burst int        // maximum token capacity (= max burst size)

	// How long an IP must be idle before its bucket is evicted from memory
	evictAfter time.Duration
}

// NewRateLimiter creates a new RateLimiter and starts the background cleanup goroutine.
//
//	rps   — sustained requests per second (use a fraction for < 1 rps, e.g. rate.Limit(1.0/60))
//	burst — maximum concurrent requests allowed before tokens run out
//
// OTP example (3 requests per 10 minutes, no burst above 3):
//
//	NewRateLimiter(rate.Limit(3.0/600), 3)
func NewRateLimiter(rps rate.Limit, burst int) *RateLimiter {
	rl := &RateLimiter{
		visitors:   make(map[string]*ipLimiter),
		rps:        rps,
		burst:      burst,
		evictAfter: 10 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// getLimiter returns (or lazily creates) the token bucket for the given IP.
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		v = &ipLimiter{
			limiter: rate.NewLimiter(rl.rps, rl.burst),
		}
		rl.visitors[ip] = v
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// cleanupLoop evicts buckets for IPs that have not been seen recently.
// This prevents unbounded memory growth from port-scanning or one-off IPs.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.evictAfter / 2)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > rl.evictAfter {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Limit returns an http.Handler middleware that enforces the per-IP token bucket.
// Requests from IPs that have exhausted their tokens receive a 429 Too Many Requests
// response with a Retry-After header.
//
// Usage (chi):
//
//	otpLimiter := middleware.NewRateLimiter(rate.Limit(3.0/600), 3)
//	r.With(otpLimiter.Limit()).Post("/auth/verify-otp", h.Auth.VerifyOTP)
func (rl *RateLimiter) Limit() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := realIP(r)
			limiter := rl.getLimiter(ip)

			if !limiter.Allow() {
				// RFC 6585 — 429 Too Many Requests
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "600") // hint: try again in 10 min
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded: too many OTP requests from this IP, please try again later"}`)) //nolint:errcheck
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// realIP extracts the originating IP, respecting X-Forwarded-For / X-Real-IP
// headers set by a reverse proxy, but only using the first (leftmost) value
// which is the client's address before any proxy hops.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For: client, proxy1, proxy2
		// Take the leftmost non-empty token as the originating IP.
		for _, part := range splitAndTrim(xff, ',') {
			if ip := net.ParseIP(part); ip != nil {
				return ip.String()
			}
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// splitAndTrim splits s by sep and trims whitespace from each element.
func splitAndTrim(s string, sep rune) []string {
	var parts []string
	start := 0
	for i, ch := range s {
		if ch == sep {
			parts = append(parts, trimSpace(s[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, trimSpace(s[start:]))
	return parts
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
