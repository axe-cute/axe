package ws

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// RedisAdapter implements Adapter using Redis Pub/Sub for multi-instance broadcasting.
type RedisAdapter struct {
	rdb     *redis.Client
	prefix  string
	log     *slog.Logger
	pubsubs map[string]*redis.PubSub
}

// NewRedisAdapter creates a new Redis-backed adapter.
func NewRedisAdapter(rdb *redis.Client, opts ...func(*RedisAdapter)) *RedisAdapter {
	a := &RedisAdapter{rdb: rdb, prefix: "axe:ws:", log: slog.Default(), pubsubs: make(map[string]*redis.PubSub)}
	for _, o := range opts { o(a) }
	return a
}

// WithRedisPrefix sets the Redis key prefix.
func WithRedisPrefix(p string) func(*RedisAdapter) { return func(a *RedisAdapter) { a.prefix = p } }

// WithRedisLogger sets the logger.
func WithRedisLogger(l *slog.Logger) func(*RedisAdapter) { return func(a *RedisAdapter) { a.log = l } }

func (a *RedisAdapter) channel(ch string) string { return a.prefix + ch }

// Publish sends a message to a Redis channel.
func (a *RedisAdapter) Publish(ctx context.Context, channel string, msg []byte) error {
	return a.rdb.Publish(ctx, a.channel(channel), msg).Err()
}

// Subscribe listens to a Redis channel and calls handler for each message.
func (a *RedisAdapter) Subscribe(ctx context.Context, channel string, handler func([]byte)) error {
	ch := a.channel(channel)
	ps := a.rdb.Subscribe(ctx, ch)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return fmt.Errorf("ws/redis: subscribe to %q: %w", channel, err)
	}
	a.pubsubs[channel] = ps
	go func() {
		defer ps.Close() //nolint:errcheck
		for msg := range ps.Channel() { handler([]byte(msg.Payload)) }
	}()
	a.log.Info("ws/redis: subscribed", "channel", ch)
	return nil
}

// Close unsubscribes from all channels and closes PubSub connections.
func (a *RedisAdapter) Close() error {
	for ch, ps := range a.pubsubs {
		_ = ps.Unsubscribe(context.Background(), a.channel(ch))
		_ = ps.Close()
	}
	return nil
}
