// Package cache provides a Redis-backed cache layer for axe.
// All operations accept context for cancellation and timeout propagation.
package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrCacheMiss is returned when a key doesn't exist in the cache.
var ErrCacheMiss = errors.New("cache: key not found")

// Client wraps go-redis with axe-specific helpers.
type Client struct {
	rdb    *redis.Client
	prefix string // key prefix to avoid collisions across envs/tenants
}

// Config holds Redis connection settings.
type Config struct {
	Addr     string // e.g. "localhost:6379"
	Password string
	DB       int
	Prefix   string // key prefix, e.g. "axe:dev:"
}

// New creates and pings a Redis client.
func New(cfg Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("cache: ping redis: %w", err)
	}

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "axe:"
	}

	return &Client{rdb: rdb, prefix: prefix}, nil
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// key returns `prefix + k` to avoid namespace collisions.
func (c *Client) key(k string) string {
	return c.prefix + k
}

// ── Generic string operations ─────────────────────────────────────────────────

// Set stores a string value with an optional TTL (0 = no expiry).
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := c.rdb.Set(ctx, c.key(key), value, ttl).Err(); err != nil {
		return fmt.Errorf("cache.Set %q: %w", key, err)
	}
	return nil
}

// Get retrieves a string value. Returns ErrCacheMiss if not found.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	val, err := c.rdb.Get(ctx, c.key(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheMiss
	}
	if err != nil {
		return "", fmt.Errorf("cache.Get %q: %w", key, err)
	}
	return val, nil
}

// Delete removes one or more keys.
func (c *Client) Delete(ctx context.Context, keys ...string) error {
	prefixed := make([]string, len(keys))
	for i, k := range keys {
		prefixed[i] = c.key(k)
	}
	if err := c.rdb.Del(ctx, prefixed...).Err(); err != nil {
		return fmt.Errorf("cache.Delete: %w", err)
	}
	return nil
}

// Exists reports whether a key exists.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.rdb.Exists(ctx, c.key(key)).Result()
	if err != nil {
		return false, fmt.Errorf("cache.Exists %q: %w", key, err)
	}
	return n > 0, nil
}

// ── Token blocklist (JWT revocation) ─────────────────────────────────────────

const blocklistPrefix = "blocklist:"

// BlockToken adds a token JTI to the blocklist until its expiry.
// Call this on logout or token rotation.
func (c *Client) BlockToken(ctx context.Context, jti string, ttl time.Duration) error {
	return c.Set(ctx, blocklistPrefix+jti, "1", ttl)
}

// IsTokenBlocked reports whether a token JTI has been revoked.
func (c *Client) IsTokenBlocked(ctx context.Context, jti string) (bool, error) {
	return c.Exists(ctx, blocklistPrefix+jti)
}

// ── Health ────────────────────────────────────────────────────────────────────

// Ping checks the Redis connection health.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}
