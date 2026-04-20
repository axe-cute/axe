// Package ratelimit provides a Redis-backed rate limiting plugin for axe.
//
// Uses the sliding window algorithm via go-redis/redis_rate.
// Returns HTTP 429 with Retry-After header when the limit is exceeded.
//
// Usage:
//
//	app.Use(ratelimit.New(ratelimit.Config{
//	    RPS:    100,
//	    Burst:  20,
//	    KeyBy:  ratelimit.KeyByIP,
//	}))
//
// The rate limiter registers as a global chi middleware.
// You can also apply it to specific route groups:
//
//	limiter := plugin.MustResolve[*ratelimit.Limiter](app, ratelimit.ServiceKey)
//	app.Router.With(limiter.Middleware()).Post("/api/login", loginHandler)
//
// Metrics:
//
//	axe_ratelimit_blocked_total — counter, labels: key_type
//
// Layer conformance (Story 8.10):
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 5: ServiceKey for cross-plugin resolution
//   - Layer 6: uses app.Cache.Redis() — no self-created Redis connections
package ratelimit

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	redis_rate "github.com/go-redis/redis_rate/v10"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/axe-cute/axe/pkg/plugin"
)

// ServiceKey is the typed service locator key for [Limiter].
const ServiceKey = "ratelimit"

// KeyFunc extracts a rate limit key from a request.
// The key groups requests for counting — e.g. IP address, user ID, API key.
type KeyFunc func(r *http.Request) string

// Built-in key functions.
var (
	// KeyByIP groups by client IP address (default).
	KeyByIP KeyFunc = func(r *http.Request) string {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			// Fallback: RemoteAddr may not have a port (e.g. in tests).
			return r.RemoteAddr
		}
		// Prefer X-Forwarded-For when behind a trusted proxy.
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		}
		return ip
	}

	// KeyByUser groups by JWT sub claim (user ID). Falls back to IP if not found.
	KeyByUser KeyFunc = func(r *http.Request) string {
		// Reads "user_id" from context (set by jwtauth middleware).
		if uid, ok := r.Context().Value("user_id").(string); ok && uid != "" {
			return "user:" + uid
		}
		return KeyByIP(r)
	}

	// KeyByAPIKey groups by Bearer token (for API key rate limiting).
	KeyByAPIKey KeyFunc = func(r *http.Request) string {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			return "apikey:" + strings.TrimPrefix(auth, "Bearer ")
		}
		return KeyByIP(r)
	}
)

// ── Config ────────────────────────────────────────────────────────────────────

// Config configures the rate limit plugin.
type Config struct {
	// RPS is the maximum requests per second per key. Required.
	RPS int

	// Burst is the maximum burst size (requests allowed above RPS for a short window).
	// Default: RPS (no extra burst).
	Burst int

	// KeyBy selects how to identify clients. Default: KeyByIP.
	KeyBy KeyFunc

	// Global controls whether the limiter is applied globally to all routes
	// (via app.Router.Use) or only when explicitly applied to route groups.
	// Default: true (global).
	Global bool
}

func (c *Config) defaults() {
	if c.KeyBy == nil {
		c.KeyBy = KeyByIP
	}
	if c.Burst <= 0 {
		c.Burst = c.RPS
	}
	if !c.Global {
		c.Global = true // default: global
	}
}

func (c *Config) validate() error {
	if c.RPS <= 0 {
		return fmt.Errorf("ratelimit: RPS must be > 0 (got %d)", c.RPS)
	}
	return nil
}

// ── Limiter ───────────────────────────────────────────────────────────────────

// Limiter wraps redis_rate.Limiter with axe-specific key functions.
// Exported so route groups can apply it selectively.
type Limiter struct {
	inner  *redis_rate.Limiter
	cfg    Config
	limit  redis_rate.Limit
	errors prometheus.Counter
}

// Middleware returns a chi-compatible HTTP middleware.
func (l *Limiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := l.cfg.KeyBy(r)
			res, err := l.inner.Allow(r.Context(), key, l.limit)
			if err != nil {
				// Redis unavailable — fail open (don't block legitimate traffic).
				next.ServeHTTP(w, r)
				return
			}
			if res.Allowed == 0 {
				if l.errors != nil {
					l.errors.Inc()
				}
				retryAfter := int(res.RetryAfter.Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(l.cfg.RPS))
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, `{"error":"rate limit exceeded","retry_after":`+strconv.Itoa(retryAfter)+`}`, http.StatusTooManyRequests)
				return
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(l.cfg.RPS))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
			next.ServeHTTP(w, r)
		})
	}
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin implements [plugin.Plugin] for rate limiting.
type Plugin struct {
	cfg     Config
	limiter *Limiter
}

// New creates a ratelimit plugin with the given configuration.
// Returns an error if config is invalid (Layer 4: fail-fast in New).
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg}, nil
}

// Name returns the plugin identifier.
func (p *Plugin) Name() string { return "ratelimit" }

// Register wires up the rate limiter using app.Cache.Redis() (Layer 6: shared connection).
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	log := app.Logger.With("plugin", p.Name())

	if app.Cache == nil {
		return fmt.Errorf("ratelimit: requires Redis — app.Cache is nil (did you configure Redis?)")
	}

	// Prometheus counter — naming follows axe_{plugin}_{metric}_{unit} convention.
	blocked := promauto.NewCounter(prometheus.CounterOpts{
		Name: "axe_ratelimit_blocked_total",
		Help: "Total number of requests blocked by the rate limiter.",
	})

	inner := redis_rate.NewLimiter(app.Cache.Redis())
	limit := redis_rate.PerSecond(p.cfg.RPS)

	l := &Limiter{
		inner:  inner,
		cfg:    p.cfg,
		limit:  limit,
		errors: blocked,
	}
	p.limiter = l

	if p.cfg.Global {
		app.Router.Use(l.Middleware())
		log.Info("rate limiter registered (global)", "rps", p.cfg.RPS, "burst", p.cfg.Burst)
	} else {
		log.Info("rate limiter registered (manual apply)", "rps", p.cfg.RPS, "burst", p.cfg.Burst)
	}

	// Layer 5: provide via typed service locator.
	plugin.Provide[*Limiter](app, ServiceKey, l)
	return nil
}

// Shutdown is a no-op — Redis connection is managed by app.Cache.
func (p *Plugin) Shutdown(_ context.Context) error { return nil }

// ── Allow helper ─────────────────────────────────────────────────────────────

// Allow checks whether a request is within the rate limit without going through HTTP.
// Useful for programmatic rate limiting (e.g. WebSocket connections, gRPC calls).
//
//	l := plugin.MustResolve[*ratelimit.Limiter](app, ratelimit.ServiceKey)
//	allowed, retryAfter, err := l.Allow(ctx, "user:42")
func (l *Limiter) Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error) {
	res, err := l.inner.Allow(ctx, key, l.limit)
	if err != nil {
		return true, 0, err // fail-open
	}
	if res.Allowed == 0 {
		return false, res.RetryAfter, nil
	}
	return true, 0, nil
}
