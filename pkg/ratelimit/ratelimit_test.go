package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/ratelimit"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// okHandler responds 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func doRequest(t *testing.T, handler http.Handler, ip string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip + ":54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// ── New(nil) — no-op mode ─────────────────────────────────────────────────────

func TestNew_NilRedis_NoOp(t *testing.T) {
	l := ratelimit.New(nil)
	require.NotNil(t, l)

	h := l.Global()(okHandler)
	rec := doRequest(t, h, "1.2.3.4")
	assert.Equal(t, http.StatusOK, rec.Code, "nil-Redis limiter should always pass through")
	// No rate limit headers expected
	assert.Empty(t, rec.Header().Get("X-RateLimit-Limit"))
}

// ── Disabled() ────────────────────────────────────────────────────────────────

func TestDisabled_AlwaysPasses(t *testing.T) {
	h := ratelimit.Disabled()(okHandler)
	for i := 0; i < 5; i++ {
		rec := doRequest(t, h, "9.9.9.9")
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}

// ── Global middleware — headers ───────────────────────────────────────────────

func TestGlobal_SetsRateLimitHeaders(t *testing.T) {
	rdb := newTestRedis(t)
	l := ratelimit.New(rdb)
	h := l.Global()(okHandler)

	rec := doRequest(t, h, "10.0.0.1")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Limit"), "X-RateLimit-Limit header should be set")
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining"), "X-RateLimit-Remaining header should be set")
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Reset"), "X-RateLimit-Reset header should be set")
}

// ── Strict middleware — rate enforced ─────────────────────────────────────────

func TestStrict_BlocksAfterLimit(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	l := ratelimit.New(rdb)
	h := l.Strict()(okHandler) // 10 req/min default

	ip := "192.168.1.42"

	// Exhaust the limit
	for i := 0; i < 10; i++ {
		rec := doRequest(t, h, ip)
		assert.Equal(t, http.StatusOK, rec.Code, "request %d should succeed", i+1)
	}

	// 11th request should be rejected
	rec := doRequest(t, h, ip)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "request beyond limit should return 429")
	assert.NotEmpty(t, rec.Header().Get("Retry-After"), "Retry-After header should be set on 429")
}

// ── IP isolation — different IPs have separate buckets ───────────────────────

func TestStrict_IsolatesByIP(t *testing.T) {
	rdb := newTestRedis(t)
	l := ratelimit.New(rdb)
	h := l.Strict()(okHandler)

	// Exhaust limit for IP 1
	for i := 0; i < 10; i++ {
		doRequest(t, h, "10.0.0.1")
	}
	rec := doRequest(t, h, "10.0.0.1")
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// IP 2 should still have its own bucket untouched
	rec2 := doRequest(t, h, "10.0.0.2")
	assert.Equal(t, http.StatusOK, rec2.Code, "different IP should have its own rate limit bucket")
}

// ── X-Forwarded-For header respected ─────────────────────────────────────────

func TestGlobal_ReadsXForwardedFor(t *testing.T) {
	rdb := newTestRedis(t)
	l := ratelimit.New(rdb)
	h := l.Strict()(okHandler)

	ip := "203.0.113.1"

	// Exhaust limit for X-Forwarded-For IP
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", ip)
		req.RemoteAddr = "127.0.0.1:9999" // proxy addr — should be ignored
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// 11th should be blocked
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", ip)
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

// ── X-Real-IP header respected ────────────────────────────────────────────────

func TestGlobal_ReadsXRealIP(t *testing.T) {
	rdb := newTestRedis(t)
	l := ratelimit.New(rdb)

	ip := "198.51.100.7"

	// Just check it doesn't 429 on first request when X-Real-IP is set
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", ip)
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()
	l.Global()(okHandler).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ── Check() — programmatic check ─────────────────────────────────────────────

func TestCheck_AllowsWithinLimit(t *testing.T) {
	rdb := newTestRedis(t)
	l := ratelimit.New(rdb)

	allowed, remaining, _, err := l.Check(t.Context(), "test-check-key", 5, time.Minute)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 4, remaining) // 5 burst - 1 consumed = 4
}

func TestCheck_NilRedis_AllowsAll(t *testing.T) {
	l := ratelimit.New(nil)

	allowed, remaining, _, err := l.Check(t.Context(), "key", 10, time.Minute)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 10, remaining, "nil Redis should report full remaining budget")
}

// ── Response body on 429 ──────────────────────────────────────────────────────

func TestStrict_429Response_IsJSON(t *testing.T) {
	rdb := newTestRedis(t)
	l := ratelimit.New(rdb)
	h := l.Strict()(okHandler)

	ip := "172.16.0.99"
	for i := 0; i < 10; i++ {
		doRequest(t, h, ip)
	}

	rec := doRequest(t, h, ip)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	body := rec.Body.String()
	assert.Contains(t, body, "TOO_MANY_REQUESTS")
	assert.Contains(t, body, "retry after")
}
