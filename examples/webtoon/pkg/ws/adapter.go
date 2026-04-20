package ws

import "context"

// Adapter allows the Hub to broadcast across multiple instances (e.g. via Redis Pub/Sub).
// The default MemoryAdapter is a no-op for single-instance deployments.
type Adapter interface {
	Publish(ctx context.Context, channel string, msg []byte) error
	Subscribe(ctx context.Context, channel string, handler func(msg []byte)) error
	Close() error
}

// MemoryAdapter is a no-op adapter for single-instance deployments.
type MemoryAdapter struct{}

func (MemoryAdapter) Publish(_ context.Context, _ string, _ []byte) error         { return nil }
func (MemoryAdapter) Subscribe(_ context.Context, _ string, _ func([]byte)) error { return nil }
func (MemoryAdapter) Close() error                                                { return nil }
