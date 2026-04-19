// Package ratelimit provides a Redis sliding-window rate limiter for ecommerce.
package ratelimit

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

// Limiter wraps redis_rate.Limiter with pre-configured limits.
type Limiter struct {
	rl *redis_rate.Limiter
}

// New creates a Limiter backed by the given Redis client.
// If rdb is nil, all middleware become no-ops (fail-open) so the app
// still starts without Redis in development.
func New(rdb *redis.Client) *Limiter {
	if rdb == nil {
		return &Limiter{}
	}
	return &Limiter{rl: redis_rate.NewLimiter(rdb)}
}

// Global returns a middleware limiting each IP to 100 requests per minute.
func (l *Limiter) Global() func(http.Handler) http.Handler {
	return l.middleware(100, time.Minute, "global")
}

// Strict returns a middleware limiting each IP to 10 requests per minute.
// Use on auth-sensitive routes (login, register, password reset).
func (l *Limiter) Strict() func(http.Handler) http.Handler {
	return l.middleware(10, time.Minute, "strict")
}

func (l *Limiter) middleware(rate int, window time.Duration, label string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l.rl == nil {
				next.ServeHTTP(w, r)
				return
			}

			ip := realIP(r)
			key := fmt.Sprintf("rl:%s:%s", label, ip)

			result, err := l.rl.Allow(r.Context(), key, redis_rate.Limit{
				Rate:   rate,
				Period: window,
				Burst:  rate,
			})
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rate))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(
				time.Now().Add(result.ResetAfter).Unix(), 10,
			))

			if result.Allowed == 0 {
				retryAfter := int(result.RetryAfter.Seconds()) + 1
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"code":"TOO_MANY_REQUESTS","message":"rate limit exceeded — retry after %ds"}`, retryAfter)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// realIP extracts the real client IP, honoring X-Forwarded-For / X-Real-IP.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
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
	addr := r.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
