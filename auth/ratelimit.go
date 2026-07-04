package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ---------------------------------------------------------------------------
// Per-IP token-bucket rate limiter with periodic cleanup.
//
// Each IP gets its own *rate.Limiter. Idle entries older than 5 minutes
// are removed by a background goroutine.
// ---------------------------------------------------------------------------

type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	rps      rate.Limit
	burst    int
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(rps, burst int) *ipRateLimiter {
	rl := &ipRateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop runs every minute and evicts IP entries that have not been
// seen for more than 5 minutes.
func (rl *ipRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for ip, entry := range rl.limiters {
			if time.Since(entry.lastSeen) > 5*time.Minute {
				delete(rl.limiters, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// allow checks whether the given IP is within its rate limit.  If the IP
// has no limiter yet, one is created with the configured rate and burst.
// Returns true if the request is allowed.
func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	entry, exists := rl.limiters[ip]
	if !exists {
		entry = &rateLimiterEntry{
			limiter: rate.NewLimiter(rl.rps, rl.burst),
		}
		rl.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	rl.mu.Unlock()
	return entry.limiter.Allow()
}

// ---------------------------------------------------------------------------
// Rate-limit middleware factories.
// ---------------------------------------------------------------------------

// AdminRateLimit returns middleware that rate-limits every request by IP
// using a token bucket with the given sustained rate (req/s) and burst.
//
// Intended for the /api/* route group (all admin endpoints).
func AdminRateLimit(rps, burst int) func(http.Handler) http.Handler {
	rl := newIPRateLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r)
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "1")
				writeJSON(w, http.StatusTooManyRequests, jsonError("Too many requests"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// OAuthRateLimit returns middleware that rate-limits ONLY /api/oauth/*
// requests by IP.  All other paths pass through without consuming tokens.
//
// Intended to be stacked after AdminRateLimit so OAuth endpoints are subject
// to both the general /api/ cap and this stricter cap.
func OAuthRateLimit(rps, burst int) func(http.Handler) http.Handler {
	rl := newIPRateLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/oauth/") {
				next.ServeHTTP(w, r)
				return
			}
			ip := extractClientIP(r)
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "1")
				writeJSON(w, http.StatusTooManyRequests, jsonError("Too many requests"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
