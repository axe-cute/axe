// Package ratelimit provides Redis-backed sliding window rate limiting for axe.
// It wraps go-redis/redis_rate and exposes Chi-compatible middleware.
//
// Usage:
//
//	limiter := ratelimit.New(redisClient)
//	r.Use(limiter.Global())                   // 100 req/min per IP
//	r.With(limiter.Strict()).Post("/login", h) // 10 req/min per IP (auth routes)
package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"

	"github.com/axe-go/axe/internal/handler/middleware"
	"github.com/axe-go/axe/pkg/apperror"
)

// Limiter wraps redis_rate.Limiter with pre-configured limits.
type Limiter struct {
	rl *redis_rate.Limiter
}

// New creates a Limiter backed by the given Redis client.
// If rdb is nil, all middleware calls are no-ops (disabled).
func New(rdb *redis.Client) *Limiter {
	if rdb == nil {
		return &Limiter{}
	}
	return &Limiter{rl: redis_rate.NewLimiter(rdb)}
}

// Global returns a middleware limiting each IP to 100 requests per minute.
// This is intended as the default global rate limit for all API routes.
func (l *Limiter) Global() func(http.Handler) http.Handler {
	return l.Middleware(100, time.Minute, "global")
}

// Strict returns a middleware limiting each IP to 10 requests per minute.
// Apply to authentication-sensitive routes (login, register, password reset)
// to protect against brute-force attacks.
func (l *Limiter) Strict() func(http.Handler) http.Handler {
	return l.Middleware(10, time.Minute, "strict")
}

// Middleware returns a Chi-compatible rate limiting middleware.
//
//	rate   — max requests allowed in the window
//	window — time window duration
//	label  — used as Redis key prefix for namespacing (e.g. "global", "auth")
func (l *Limiter) Middleware(rate int, window time.Duration, label string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// No-op when Redis is unavailable
			if l.rl == nil {
				next.ServeHTTP(w, r)
				return
			}

			ip := realIP(r)
			key := fmt.Sprintf("rl:%s:%s", label, ip)

			result, err := l.rl.Allow(r.Context(), key, redis_rate.Limit{
				Rate:   rate,
				Period: window,
				Burst:  rate, // burst = rate: no burst above the rate limit
			})
			if err != nil {
				// Fail-open: log and pass through if Redis is unhealthy
				next.ServeHTTP(w, r)
				return
			}

			// Set standard rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rate))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(
				time.Now().Add(result.ResetAfter).Unix(), 10,
			))

			if result.Allowed == 0 {
				retryAfter := int(result.RetryAfter.Seconds()) + 1
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				middleware.WriteError(w, apperror.ErrTooManyRequests.WithMessage(
					fmt.Sprintf("rate limit exceeded — retry after %ds", retryAfter),
				))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ── IP extraction ─────────────────────────────────────────────────────────────

// realIP extracts the client's real IP address.
// Respects X-Forwarded-For and X-Real-IP headers set by reverse proxies.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For may contain multiple IPs: "client, proxy1, proxy2"
		for i, c := range xff {
			if c == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Strip port from RemoteAddr
	addr := r.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

// Disabled returns a no-op middleware (useful in tests).
func Disabled() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

// ── AllowN ────────────────────────────────────────────────────────────────────

// Check performs a programmatic rate limit check without HTTP response side effects.
// Returns (allowed, remaining, retryAfter, error).
// Use in handlers that need custom rate limit logic.
func (l *Limiter) Check(ctx context.Context, key string, rate int, window time.Duration) (bool, int, time.Duration, error) {
	if l.rl == nil {
		return true, rate, 0, nil
	}
	result, err := l.rl.Allow(ctx, key, redis_rate.Limit{
		Rate:   rate,
		Period: window,
		Burst:  rate,
	})
	if err != nil {
		return true, 0, 0, err // fail-open
	}
	return result.Allowed > 0, result.Remaining, result.RetryAfter, nil
}
