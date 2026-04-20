// Package ratelimit provides Redis-backed sliding window rate limiting for axe.
// It wraps go-redis/redis_rate and exposes Chi-compatible middleware.
//
// Usage:
//
//	limiter := ratelimit.New(redisClient)
//	r.Use(limiter.Global())                   // 100 req/min per IP
//	r.With(limiter.Strict()).Post("/login", h) // 10 req/min per IP (auth routes)
//
// IP extraction:
//
//	By default, the limiter uses RemoteAddr for IP identification (safe default).
//	If running behind a trusted reverse proxy, call WithTrustedProxies to allow
//	X-Forwarded-For / X-Real-IP header inspection:
//
//	limiter := ratelimit.New(redisClient, ratelimit.WithTrustedProxies("10.0.0.0/8", "172.16.0.0/12"))
package ratelimit

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"

	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/pkg/apperror"
)

// FailMode controls behaviour when Redis is temporarily unavailable.
type FailMode int

const (
	// FailOpen allows the request through when Redis errors (default).
	// Safe for non-critical routes; preserves availability over security.
	FailOpen FailMode = iota

	// FailClosed rejects the request with 429 when Redis errors.
	// Use for security-critical routes where allowing unmetered traffic is dangerous.
	FailClosed
)

// Limiter wraps redis_rate.Limiter with pre-configured limits.
type Limiter struct {
	rl             *redis_rate.Limiter
	trustedNets    []*net.IPNet
	failMode       FailMode
}

// Option configures Limiter behaviour.
type Option func(*Limiter)

// WithTrustedProxies sets CIDRs whose X-Forwarded-For / X-Real-IP headers are trusted.
// Without this, the limiter always uses RemoteAddr (safe default).
//
// Example:
//
//	ratelimit.WithTrustedProxies("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")
func WithTrustedProxies(cidrs ...string) Option {
	return func(l *Limiter) {
		for _, cidr := range cidrs {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				// Try as single IP (e.g. "127.0.0.1")
				ip := net.ParseIP(cidr)
				if ip != nil {
					mask := net.CIDRMask(128, 128)
					if ip.To4() != nil {
						mask = net.CIDRMask(32, 32)
					}
					ipNet = &net.IPNet{IP: ip, Mask: mask}
				} else {
					continue // skip invalid CIDR
				}
			}
			l.trustedNets = append(l.trustedNets, ipNet)
		}
	}
}

// WithFailMode sets the behaviour when Redis is unavailable.
// Default: FailOpen (allow requests through).
func WithFailMode(mode FailMode) Option {
	return func(l *Limiter) {
		l.failMode = mode
	}
}

// New creates a Limiter backed by the given Redis client.
// If rdb is nil, all middleware calls are no-ops (disabled).
func New(rdb *redis.Client, opts ...Option) *Limiter {
	l := &Limiter{}
	for _, opt := range opts {
		opt(l)
	}
	if rdb != nil {
		l.rl = redis_rate.NewLimiter(rdb)
	}
	return l
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

			ip := l.realIP(r)
			key := fmt.Sprintf("rl:%s:%s", label, ip)

			result, err := l.rl.Allow(r.Context(), key, redis_rate.Limit{
				Rate:   rate,
				Period: window,
				Burst:  rate, // burst = rate: no burst above the rate limit
			})
			if err != nil {
				if l.failMode == FailClosed {
					middleware.WriteError(w, apperror.ErrTooManyRequests.WithMessage("rate limit service unavailable"))
					return
				}
				// Fail-open: pass through if Redis is unhealthy
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
//
// Security: By default, only RemoteAddr is used. X-Forwarded-For / X-Real-IP
// headers are only trusted when the request arrives from a trusted proxy
// (configured via WithTrustedProxies). This prevents IP spoofing attacks
// where clients set arbitrary X-Forwarded-For headers to bypass rate limits.
func (l *Limiter) realIP(r *http.Request) string {
	remoteIP := stripPort(r.RemoteAddr)

	// Only trust forwarded headers if the direct connection is from a trusted proxy.
	if l.isTrustedProxy(remoteIP) {
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
	}

	return remoteIP
}

// isTrustedProxy checks if the given IP is within any configured trusted CIDR.
func (l *Limiter) isTrustedProxy(ip string) bool {
	if len(l.trustedNets) == 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range l.trustedNets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// stripPort removes the port from an address string.
func stripPort(addr string) string {
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
		if l.failMode == FailClosed {
			return false, 0, 0, err
		}
		return true, 0, 0, err // fail-open
	}
	return result.Allowed > 0, result.Remaining, result.RetryAfter, nil
}
