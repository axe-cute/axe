package cache_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/cache"
)

// newTestCache spins up an in-process miniredis server and returns a cache.Client.
func newTestCache(t *testing.T) *cache.Client {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	c, err := cache.New(cache.Config{
		Addr:   mr.Addr(),
		Prefix: "test:",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// ── Construction ──────────────────────────────────────────────────────────────

func TestNew_Success(t *testing.T) {
	c := newTestCache(t)
	require.NotNil(t, c)
}

func TestNew_InvalidAddr(t *testing.T) {
	// Use a port that is almost certainly not listening
	_, err := cache.New(cache.Config{Addr: "localhost:19999"})
	require.Error(t, err)
}

// ── ErrCacheMiss ─────────────────────────────────────────────────────────────

func TestErrCacheMiss_SentinelIsDistinct(t *testing.T) {
	assert.NotNil(t, cache.ErrCacheMiss)
	assert.True(t, errors.Is(cache.ErrCacheMiss, cache.ErrCacheMiss))
}

// ── Set / Get / Delete ────────────────────────────────────────────────────────

func TestSetGet_RoundTrip(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	err := c.Set(ctx, "hello", "world", 0)
	require.NoError(t, err)

	val, err := c.Get(ctx, "hello")
	require.NoError(t, err)
	assert.Equal(t, "world", val)
}

func TestGet_Miss(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	_, err := c.Get(ctx, "nonexistent-key")
	assert.True(t, errors.Is(err, cache.ErrCacheMiss), "expected ErrCacheMiss, got %v", err)
}

func TestSet_WithTTL_Expires(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	c, err := cache.New(cache.Config{Addr: mr.Addr(), Prefix: "t:"})
	require.NoError(t, err)
	defer c.Close()

	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "ttl-key", "value", 1*time.Second))

	// Key exists
	val, err := c.Get(ctx, "ttl-key")
	require.NoError(t, err)
	assert.Equal(t, "value", val)

	// Fast-forward miniredis clock
	mr.FastForward(2 * time.Second)

	// Key should now be expired
	_, err = c.Get(ctx, "ttl-key")
	assert.True(t, errors.Is(err, cache.ErrCacheMiss))
}

func TestDelete_RemovesKey(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "del-key", "v", 0))
	require.NoError(t, c.Delete(ctx, "del-key"))

	_, err := c.Get(ctx, "del-key")
	assert.True(t, errors.Is(err, cache.ErrCacheMiss))
}

func TestDelete_MultipleKeys(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k1", "v1", 0))
	require.NoError(t, c.Set(ctx, "k2", "v2", 0))
	require.NoError(t, c.Delete(ctx, "k1", "k2"))

	_, err1 := c.Get(ctx, "k1")
	_, err2 := c.Get(ctx, "k2")
	assert.True(t, errors.Is(err1, cache.ErrCacheMiss))
	assert.True(t, errors.Is(err2, cache.ErrCacheMiss))
}

// ── Exists ────────────────────────────────────────────────────────────────────

func TestExists_True(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "exists-key", "1", 0))
	ok, err := c.Exists(ctx, "exists-key")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestExists_False(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	ok, err := c.Exists(ctx, "ghost-key")
	require.NoError(t, err)
	assert.False(t, ok)
}

// ── JWT Token Blocklist ───────────────────────────────────────────────────────

func TestBlockToken_IsBlocked(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	jti := "test-jti-abc123"

	// Before blocking
	blocked, err := c.IsTokenBlocked(ctx, jti)
	require.NoError(t, err)
	assert.False(t, blocked, "token should not be blocked before BlockToken")

	// Block the token
	require.NoError(t, c.BlockToken(ctx, jti, 5*time.Minute))

	// After blocking
	blocked, err = c.IsTokenBlocked(ctx, jti)
	require.NoError(t, err)
	assert.True(t, blocked, "token should be blocked after BlockToken")
}

func TestBlockToken_ExpiresWithTTL(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	c, err := cache.New(cache.Config{Addr: mr.Addr(), Prefix: "t:"})
	require.NoError(t, err)
	defer c.Close()

	ctx := context.Background()
	jti := "expiring-jti"

	require.NoError(t, c.BlockToken(ctx, jti, 1*time.Second))

	blocked, err := c.IsTokenBlocked(ctx, jti)
	require.NoError(t, err)
	assert.True(t, blocked)

	mr.FastForward(2 * time.Second)

	blocked, err = c.IsTokenBlocked(ctx, jti)
	require.NoError(t, err)
	assert.False(t, blocked, "blocked token should expire after TTL")
}

// ── Ping / Health ─────────────────────────────────────────────────────────────

func TestPing_OK(t *testing.T) {
	c := newTestCache(t)
	err := c.Ping(context.Background())
	assert.NoError(t, err)
}

func TestPing_Disconnected(t *testing.T) {
	// Connect to a closed port
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := rdb.Ping(ctx).Err()
	assert.Error(t, err)
	// Verify it's a network error (connection refused or timeout)
	var netErr *net.OpError
	assert.True(t, errors.As(err, &netErr) || err == context.DeadlineExceeded,
		"expected network or timeout error, got %T: %v", err, err)
}

// ── Redis() accessor ─────────────────────────────────────────────────────────

func TestRedis_ReturnsUnderlyingClient(t *testing.T) {
	c := newTestCache(t)
	rdb := c.Redis()
	assert.NotNil(t, rdb, "Redis() should return non-nil *redis.Client")
}

// ── Key prefix isolation ──────────────────────────────────────────────────────

func TestKeyPrefix_IsolatesNamespaces(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	c1, err := cache.New(cache.Config{Addr: mr.Addr(), Prefix: "ns1:"})
	require.NoError(t, err)
	defer c1.Close()

	c2, err := cache.New(cache.Config{Addr: mr.Addr(), Prefix: "ns2:"})
	require.NoError(t, err)
	defer c2.Close()

	ctx := context.Background()

	// Set same logical key in both namespaces
	require.NoError(t, c1.Set(ctx, "shared-key", "from-ns1", 0))
	require.NoError(t, c2.Set(ctx, "shared-key", "from-ns2", 0))

	v1, err := c1.Get(ctx, "shared-key")
	require.NoError(t, err)
	assert.Equal(t, "from-ns1", v1)

	v2, err := c2.Get(ctx, "shared-key")
	require.NoError(t, err)
	assert.Equal(t, "from-ns2", v2)
}
