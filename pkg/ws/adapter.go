package ws

import (
	"context"
)

// Adapter abstracts the pub/sub backend used by the Hub to fan out broadcast
// messages across multiple server instances.
//
// MemoryAdapter (default) is a no-op suitable for single-instance deployments.
// RedisAdapter enables cross-instance broadcasting via Redis Pub/Sub.
type Adapter interface {
	// Publish sends a message to the given channel so that all Hub instances
	// subscribed to that channel receive and forward it to local clients.
	Publish(ctx context.Context, channel string, msg []byte) error

	// Subscribe registers a handler that is called for every message arriving
	// on the given channel. Implementations must call handler in a dedicated
	// goroutine and return promptly.
	Subscribe(ctx context.Context, channel string, handler func(msg []byte)) error

	// Close unsubscribes and releases any resources held by the adapter.
	Close() error
}

// ── MemoryAdapter ─────────────────────────────────────────────────────────────

// MemoryAdapter is a no-op Adapter for single-instance deployments.
// Publish is a no-op because the Hub already delivers to local rooms directly;
// Subscribe is never called. This keeps the single-host case zero-overhead.
type MemoryAdapter struct{}

// Publish is a no-op for in-memory deployments.
func (MemoryAdapter) Publish(_ context.Context, _ string, _ []byte) error { return nil }

// Subscribe is a no-op for in-memory deployments.
func (MemoryAdapter) Subscribe(_ context.Context, _ string, _ func([]byte)) error { return nil }

// Close is a no-op for in-memory deployments.
func (MemoryAdapter) Close() error { return nil }
