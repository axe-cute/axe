package ws

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// RedisAdapter implements the Adapter interface using Redis Pub/Sub.
// It enables cross-instance WebSocket broadcast: when instance A calls
// hub.Broadcast, the message is published to Redis so that instance B (and any
// other instance) receives it and forwards it to their local clients.
type RedisAdapter struct {
	rdb    *redis.Client
	prefix string
	log    *slog.Logger

	// pubsub holds active subscriptions keyed by channel.
	pubsubs map[string]*redis.PubSub
}

// NewRedisAdapter creates a RedisAdapter that uses the given go-redis client.
// An optional key prefix prevents collisions in shared Redis deployments
// (defaults to "axe:ws:").
func NewRedisAdapter(rdb *redis.Client, opts ...func(*RedisAdapter)) *RedisAdapter {
	a := &RedisAdapter{
		rdb:     rdb,
		prefix:  "axe:ws:",
		log:     slog.Default(),
		pubsubs: make(map[string]*redis.PubSub),
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// WithRedisPrefix overrides the default channel prefix.
func WithRedisPrefix(p string) func(*RedisAdapter) {
	return func(a *RedisAdapter) { a.prefix = p }
}

// WithRedisLogger overrides the default logger.
func WithRedisLogger(l *slog.Logger) func(*RedisAdapter) {
	return func(a *RedisAdapter) { a.log = l }
}

// channel returns the full Redis channel name for the given room/key.
func (a *RedisAdapter) channel(ch string) string { return a.prefix + ch }

// Publish sends msg to all instances subscribed to channel.
func (a *RedisAdapter) Publish(ctx context.Context, channel string, msg []byte) error {
	if err := a.rdb.Publish(ctx, a.channel(channel), msg).Err(); err != nil {
		return fmt.Errorf("ws/redis: publish to %q: %w", channel, err)
	}
	return nil
}

// Subscribe registers handler to receive every message on channel.
// It spawns a goroutine that reads from the Redis subscription until ctx is
// cancelled; the goroutine exits cleanly when the subscription is closed.
func (a *RedisAdapter) Subscribe(ctx context.Context, channel string, handler func([]byte)) error {
	ch := a.channel(channel)
	ps := a.rdb.Subscribe(ctx, ch)

	// Ensure the subscription is active before returning.
	if _, err := ps.Receive(ctx); err != nil {
		ps.Close() //nolint:errcheck
		return fmt.Errorf("ws/redis: subscribe to %q: %w", channel, err)
	}

	a.pubsubs[channel] = ps

	go func() {
		defer ps.Close() //nolint:errcheck
		msgCh := ps.Channel()
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				handler([]byte(msg.Payload))
			case <-ctx.Done():
				return
			}
		}
	}()

	a.log.Info("ws/redis: subscribed", "channel", ch)
	return nil
}

// Close unsubscribes from all channels and releases resources.
func (a *RedisAdapter) Close() error {
	var errs []error
	for ch, ps := range a.pubsubs {
		if err := ps.Unsubscribe(context.Background(), a.channel(ch)); err != nil {
			errs = append(errs, err)
		}
		if err := ps.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("ws/redis: close errors: %v", errs)
	}
	return nil
}
