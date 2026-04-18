package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis_rate "github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
