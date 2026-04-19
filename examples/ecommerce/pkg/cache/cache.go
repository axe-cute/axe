// Package cache provides a Redis-backed cache client for ecommerce.
package cache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps a Redis client with a key prefix.
type Client struct {
	rdb    *redis.Client
	prefix string
}

// Config holds Redis connection configuration.
type Config struct {
	Addr   string // host:port
	Prefix string // key prefix, e.g. "myapp:dev:"
}

// New creates a new Redis Client and verifies connectivity.
func New(cfg Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.Addr,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb: rdb, prefix: cfg.Prefix}, nil
}

// Redis returns the underlying *redis.Client for advanced usage.
func (c *Client) Redis() *redis.Client { return c.rdb }

// Close closes the Redis connection.
func (c *Client) Close() error { return c.rdb.Close() }

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// key prepends the configured prefix to a cache key.
func (c *Client) key(k string) string { return c.prefix + k }

// Set stores a value under key with the given TTL.
func (c *Client) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	return c.rdb.Set(ctx, c.key(key), val, ttl).Err()
}

// Get retrieves a value by key. Returns redis.Nil if not found.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	v, err := c.rdb.Get(ctx, c.key(key)).Result()
	if err != nil {
		return "", err
	}
	return v, nil
}

// Del removes one or more keys.
func (c *Client) Del(ctx context.Context, keys ...string) error {
	prefixed := make([]string, len(keys))
	for i, k := range keys {
		prefixed[i] = c.key(k)
	}
	return c.rdb.Del(ctx, prefixed...).Err()
}

// Exists reports whether keys exist.
func (c *Client) Exists(ctx context.Context, keys ...string) (bool, error) {
	prefixed := make([]string, len(keys))
	for i, k := range keys {
		prefixed[i] = c.key(k)
	}
	n, err := c.rdb.Exists(ctx, prefixed...).Result()
	return n > 0, err
}

// IsNotFound returns true if err is a Redis nil (cache miss).
func IsNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "redis: nil")
}
