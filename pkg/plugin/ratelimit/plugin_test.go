package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	redis_rate "github.com/go-redis/redis_rate/v10"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/cache"
	"github.com/axe-cute/axe/pkg/plugin"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_ZeroRPS(t *testing.T) {
	_, err := New(Config{RPS: 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RPS must be > 0")
}

func TestNew_NegativeRPS(t *testing.T) {
	_, err := New(Config{RPS: -5})
	require.Error(t, err)
}

func TestNew_ValidConfig(t *testing.T) {
	p, err := New(Config{RPS: 100})
	require.NoError(t, err)
	assert.NotNil(t, p)
	assert.Equal(t, "ratelimit", p.Name())
}

func TestNew_DefaultBurst(t *testing.T) {
	p, err := New(Config{RPS: 50})
	require.NoError(t, err)
	assert.Equal(t, 50, p.cfg.Burst, "Burst should default to RPS")
}

func TestNew_CustomBurst(t *testing.T) {
	p, err := New(Config{RPS: 50, Burst: 100})
	require.NoError(t, err)
	assert.Equal(t, 100, p.cfg.Burst, "Custom Burst should be preserved")
}

func TestNew_DefaultKeyFunc(t *testing.T) {
	p, err := New(Config{RPS: 10})
	require.NoError(t, err)
	assert.NotNil(t, p.cfg.KeyBy, "KeyBy should default to KeyByIP")
}

func TestNew_DefaultGlobal(t *testing.T) {
	p, err := New(Config{RPS: 10})
	require.NoError(t, err)
	assert.True(t, p.cfg.Global, "Global should default to true")
}

// ── Limiter middleware behaviour ──────────────────────────────────────────────

// testLimiter creates a Limiter backed by an in-process miniredis.
func testLimiter(t *testing.T, rps int) *Limiter {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	inner := redis_rate.NewLimiter(rdb)

	cfg := Config{RPS: rps, Burst: rps, KeyBy: KeyByIP}
	cfg.defaults()
	return &Limiter{inner: inner, cfg: cfg, limit: redis_rate.PerSecond(rps)}
}

func TestMiddleware_AllowsUnderLimit(t *testing.T) {
	l := testLimiter(t, 10)
	mw := l.Middleware()

	ok := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok = true
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	assert.True(t, ok, "handler should be called within limit")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"))
}

func TestMiddleware_Blocks429WhenExceeded(t *testing.T) {
	l := testLimiter(t, 2) // only 2 rps

	mw := l.Middleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:9999"

	var lastCode int
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		lastCode = w.Code
	}
	// After 10 requests at RPS=2, at least the last must be 429.
	assert.Equal(t, http.StatusTooManyRequests, lastCode)
}

func TestMiddleware_429HasRetryAfterHeader(t *testing.T) {
	l := testLimiter(t, 1)
	mw := l.Middleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "5.5.5.5:1234"

	// Exhaust the limit.
	var lastW *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		lastW = httptest.NewRecorder()
		handler.ServeHTTP(lastW, r)
	}
	if lastW.Code == http.StatusTooManyRequests {
		assert.NotEmpty(t, lastW.Header().Get("Retry-After"))
	}
}

func TestMiddleware_429BodyContainsJSON(t *testing.T) {
	l := testLimiter(t, 1)
	mw := l.Middleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "7.7.7.7:1234"

	var lastW *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		lastW = httptest.NewRecorder()
		handler.ServeHTTP(lastW, r)
	}
	if lastW.Code == http.StatusTooManyRequests {
		body := lastW.Body.String()
		assert.Contains(t, body, "rate limit exceeded")
		assert.Contains(t, body, "retry_after")
	}
}

func TestMiddleware_DifferentIPsNotBlocked(t *testing.T) {
	l := testLimiter(t, 2)
	mw := l.Middleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First IP exhausts limit
	for i := 0; i < 10; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	// Second IP should still be allowed
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.2:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_SetsRateLimitHeaders(t *testing.T) {
	l := testLimiter(t, 100)
	mw := l.Middleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "8.8.8.8:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	assert.Equal(t, "100", w.Header().Get("X-RateLimit-Limit"))
	remaining := w.Header().Get("X-RateLimit-Remaining")
	assert.NotEmpty(t, remaining)
}

// ── KeyByIP ───────────────────────────────────────────────────────────────────

func TestKeyByIP_Standard(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1:54321"
	assert.Equal(t, "192.168.1.1", KeyByIP(r))
}

func TestKeyByIP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	assert.Equal(t, "203.0.113.5", KeyByIP(r))
}

func TestKeyByIP_NoPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1" // no port — fallback path
	key := KeyByIP(r)
	assert.Equal(t, "192.168.1.1", key)
}

func TestKeyByIP_XForwardedForSingle(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	assert.Equal(t, "203.0.113.5", KeyByIP(r))
}

// ── KeyByUser ─────────────────────────────────────────────────────────────────

func TestKeyByUser_WithUserID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	ctx := context.WithValue(r.Context(), "user_id", "uid-42")
	r = r.WithContext(ctx)
	assert.Equal(t, "user:uid-42", KeyByUser(r))
}

func TestKeyByUser_FallbackToIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	// No user_id in context — should fall back to IP.
	key := KeyByUser(r)
	assert.Equal(t, "10.0.0.1", key)
}

func TestKeyByUser_EmptyUserID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	ctx := context.WithValue(r.Context(), "user_id", "")
	r = r.WithContext(ctx)
	// Empty user_id should fall back to IP.
	key := KeyByUser(r)
	assert.Equal(t, "10.0.0.1", key)
}

// ── KeyByAPIKey ───────────────────────────────────────────────────────────────

func TestKeyByAPIKey_WithBearer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer my-api-key-123")
	assert.Equal(t, "apikey:my-api-key-123", KeyByAPIKey(r))
}

func TestKeyByAPIKey_NoBearer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	// No Authorization header — should fall back to IP.
	key := KeyByAPIKey(r)
	assert.Equal(t, "10.0.0.1", key)
}

func TestKeyByAPIKey_BasicAuth(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	// Not Bearer — should fall back to IP.
	key := KeyByAPIKey(r)
	assert.Equal(t, "10.0.0.1", key)
}

// ── Allow helper ──────────────────────────────────────────────────────────────

func TestAllow_UnderLimit(t *testing.T) {
	l := testLimiter(t, 100)
	allowed, retryAfter, err := l.Allow(context.Background(), "test-key")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Zero(t, retryAfter)
}

func TestAllow_OverLimit(t *testing.T) {
	l := testLimiter(t, 1) // very low
	// First request should be allowed.
	allowed, _, err := l.Allow(context.Background(), "same-key")
	require.NoError(t, err)
	assert.True(t, allowed)

	// Exhaust limit.
	var lastAllowed bool
	for i := 0; i < 10; i++ {
		lastAllowed, _, err = l.Allow(context.Background(), "same-key")
		require.NoError(t, err)
	}
	assert.False(t, lastAllowed)
}

func TestAllow_DifferentKeys(t *testing.T) {
	l := testLimiter(t, 1)
	// Exhaust key-A.
	for i := 0; i < 5; i++ {
		l.Allow(context.Background(), "key-A")
	}
	// key-B should still be allowed.
	allowed, _, err := l.Allow(context.Background(), "key-B")
	require.NoError(t, err)
	assert.True(t, allowed)
}

// ── ServiceKey constant (Layer 5) ─────────────────────────────────────────────

func TestServiceKey_MatchesName(t *testing.T) {
	p, _ := New(Config{RPS: 10})
	assert.Equal(t, p.Name(), ServiceKey)
}

// ── Shutdown no-op ────────────────────────────────────────────────────────────

func TestShutdown_NoError(t *testing.T) {
	p, _ := New(Config{RPS: 10})
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Config.defaults ──────────────────────────────────────────────────────────

func TestConfig_Defaults_NilKeyFunc(t *testing.T) {
	cfg := Config{RPS: 10}
	cfg.defaults()
	assert.NotNil(t, cfg.KeyBy, "KeyBy should default to KeyByIP")
}

func TestConfig_Defaults_ZeroBurst(t *testing.T) {
	cfg := Config{RPS: 25, Burst: 0}
	cfg.defaults()
	assert.Equal(t, 25, cfg.Burst, "Burst should default to RPS when 0")
}

func TestConfig_Defaults_NegativeBurst(t *testing.T) {
	cfg := Config{RPS: 25, Burst: -1}
	cfg.defaults()
	assert.Equal(t, 25, cfg.Burst, "Burst should default to RPS when negative")
}

func TestConfig_Validate_PositiveRPS(t *testing.T) {
	cfg := Config{RPS: 1}
	assert.NoError(t, cfg.validate())
}

func TestConfig_Validate_ZeroRPS(t *testing.T) {
	cfg := Config{RPS: 0}
	err := cfg.validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "RPS must be > 0")
}

// ── Middleware with prometheus counter ────────────────────────────────────────

func TestMiddleware_BlockedCounter(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	inner := redis_rate.NewLimiter(rdb)

	// Create a limiter with a real prometheus counter.
	cfg := Config{RPS: 1, Burst: 1, KeyBy: KeyByIP}
	cfg.defaults()

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_ratelimit_blocked_total",
		Help: "Test counter",
	})

	l := &Limiter{
		inner:  inner,
		cfg:    cfg,
		limit:  redis_rate.PerSecond(1),
		errors: counter,
	}

	mw := l.Middleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "9.9.9.9:1234"

	// Send enough requests to trigger rate limiting.
	blocked := 0
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			blocked++
		}
	}

	// At least some requests should have been blocked.
	assert.Greater(t, blocked, 0, "some requests should be rate limited")
}

// ── Register (plugin lifecycle) ──────────────────────────────────────────────

func TestRegister_NilCache(t *testing.T) {
	p, err := New(Config{RPS: 10})
	require.NoError(t, err)

	// App with nil Cache should fail.
	app := plugin.NewApp(plugin.AppConfig{
		Router: chi.NewRouter(),
		Logger: slog.Default(),
		// Cache intentionally nil.
	})

	err = p.Register(context.Background(), app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app.Cache is nil")
}

func TestRegister_Success(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	cacheClient := testCacheClient(t, mr.Addr())
	app := plugin.NewApp(plugin.AppConfig{
		Router: chi.NewRouter(),
		Logger: slog.Default(),
		Cache:  cacheClient,
	})

	// Uses Global=true (default) — exercises Router.Use and Provide paths.
	p, err := New(Config{RPS: 100})
	require.NoError(t, err)

	err = p.Register(context.Background(), app)
	require.NoError(t, err)
	assert.NotNil(t, p.limiter)

	// Verify service locator registration (Layer 5).
	limiter, ok := plugin.Resolve[*Limiter](app, ServiceKey)
	require.True(t, ok)
	assert.NotNil(t, limiter)
}

// testCacheClient creates a cache.Client backed by miniredis — no real Redis needed.
func testCacheClient(t *testing.T, addr string) *cache.Client {
	t.Helper()
	c, err := cache.New(cache.Config{Addr: addr})
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	return c
}
