// Package events provides the axe plugin event bus.
//
// Plugins communicate through events without importing each other —
// eliminating tight coupling as the ecosystem grows.
//
// Usage in Register():
//
//	// Publisher (storage plugin):
//	app.Events.Publish(ctx, events.Event{
//	    Topic:   events.TopicStorageUploaded,
//	    Payload: map[string]any{"key": "2024/01/uploads/photo.jpg"},
//	})
//
//	// Subscriber (AI plugin — no import of storage package):
//	app.Events.Subscribe(events.TopicStorageUploaded, func(ctx context.Context, e events.Event) error {
//	    key := e.Payload["key"].(string)
//	    return p.generateAltText(ctx, key)
//	})
//
// Delivery modes:
//
//	Sync  — handler runs in same goroutine as Publish() — for audit, cache invalidation
//	Async — handler runs in a new goroutine — for AI analysis, thumbnail generation
//	Redis — fan-out across multiple instances via Redis pub/sub (uses app.Cache)
package events

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ── Standard topics ───────────────────────────────────────────────────────────

// Standard topic name constants follow the pattern: "{plugin}.{event}".
// Use these to avoid typos and ensure discoverability.
const (
	// Storage events
	TopicStorageUploaded = "storage.uploaded"
	TopicStorageDeleted  = "storage.deleted"

	// User events
	TopicUserRegistered = "user.registered"
	TopicUserDeleted    = "user.deleted"
	TopicUserLogin      = "user.login"

	// Job events
	TopicJobEnqueued  = "job.enqueued"
	TopicJobCompleted = "job.completed"
	TopicJobFailed    = "job.failed"

	// Email events
	TopicEmailSent   = "email.sent"
	TopicEmailFailed = "email.failed"

	// Payment events
	TopicPaymentSucceeded = "payment.succeeded"
	TopicPaymentFailed    = "payment.failed"
)

// ── Event types ───────────────────────────────────────────────────────────────

// Event is the message published on the bus.
type Event struct {
	// Topic identifies the event type (e.g. "storage.uploaded", "user.registered").
	Topic string

	// Payload carries event data. Type-erased for decoupling — document per topic.
	// Example: {"key": "2024/01/photo.jpg", "size": 1024, "content_type": "image/jpeg"}
	Payload map[string]any

	// Meta carries cross-cutting fields for tracing and debugging.
	Meta EventMeta
}

// EventMeta carries cross-cutting metadata attached to every event.
type EventMeta struct {
	// PluginSource is the Name() of the plugin that published this event.
	PluginSource string
	// Timestamp is when the event was created.
	Timestamp time.Time
	// TraceID can be propagated for distributed tracing (e.g. from request context).
	TraceID string
}

// Handler processes a received event.
// Return a non-nil error to signal processing failure (logged, never panics).
type Handler func(ctx context.Context, e Event) error

// ── Delivery mode ─────────────────────────────────────────────────────────────

// Delivery controls how events are dispatched to subscribers.
type Delivery int

const (
	// Sync dispatches the handler in the same goroutine as Publish.
	// Publish blocks until all handlers return.
	// Use for: cache invalidation, audit logging.
	Sync Delivery = iota

	// Async dispatches each handler in a new goroutine.
	// Publish returns immediately; errors are logged.
	// Use for: AI analysis, thumbnail generation, heavy I/O.
	Async

	// Redis fan-out via Redis pub/sub.
	// Use for: multi-instance coordination (chat, notifications).
	// Requires app.Cache to be non-nil.
	Redis
)

// ── Bus interface ─────────────────────────────────────────────────────────────

// Bus is the plugin event bus interface.
// Access it via app.Events in any plugin's Register() method.
type Bus interface {
	// Publish sends an event to all subscribers of the topic.
	// For Sync delivery, blocks until all handlers return.
	// For Async/Redis, returns immediately.
	Publish(ctx context.Context, e Event) error

	// Subscribe registers a handler for a topic pattern.
	// Pattern supports wildcards: "storage.*" matches all "storage." prefixed topics.
	Subscribe(topic string, h Handler)
}

// ── In-process bus implementation ─────────────────────────────────────────────

// subscription holds a handler and its delivery mode.
type subscription struct {
	pattern  string
	handler  Handler
	delivery Delivery
}

// InProcessBus implements Bus with in-process sync and async delivery.
// Async handlers are bounded by a semaphore to prevent goroutine leaks
// under high event throughput.
type InProcessBus struct {
	mu   sync.RWMutex
	subs []subscription
	log  *slog.Logger
	sem  chan struct{} // bounds concurrent async handlers
}

// defaultAsyncConcurrency limits the number of concurrent async event handlers.
const defaultAsyncConcurrency = 100

// NewInProcessBus creates a new in-process event bus.
// defaultDelivery controls whether Subscribe() creates Sync or Async handlers
// unless overridden via SubscribeMode.
func NewInProcessBus(log *slog.Logger) *InProcessBus {
	if log == nil {
		log = slog.Default()
	}
	return &InProcessBus{
		log: log,
		sem: make(chan struct{}, defaultAsyncConcurrency),
	}
}

// Subscribe registers a Sync handler for the given topic pattern.
// For Async handlers, use SubscribeAsync.
func (b *InProcessBus) Subscribe(topic string, h Handler) {
	b.subscribe(topic, h, Sync)
}

// SubscribeAsync registers an Async handler.
func (b *InProcessBus) SubscribeAsync(topic string, h Handler) {
	b.subscribe(topic, h, Async)
}

func (b *InProcessBus) subscribe(topic string, h Handler, d Delivery) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, subscription{pattern: topic, handler: h, delivery: d})
}

// Publish delivers the event to all matching subscribers.
// Sync handlers are called inline; errors from sync handlers are collected
// and returned as a joined error (via errors.Join).
// Async handlers are launched in goroutines — their errors are logged but
// not returned (fire-and-forget).
func (b *InProcessBus) Publish(ctx context.Context, e Event) error {
	if e.Meta.Timestamp.IsZero() {
		e.Meta.Timestamp = time.Now()
	}

	b.mu.RLock()
	matched := make([]subscription, 0)
	for _, sub := range b.subs {
		if topicMatches(sub.pattern, e.Topic) {
			matched = append(matched, sub)
		}
	}
	b.mu.RUnlock()

	var errs []error
	for _, sub := range matched {
		switch sub.delivery {
		case Async:
			// Acquire semaphore to bound concurrent async handlers.
			// If all slots are occupied, this blocks until one completes.
			b.sem <- struct{}{}
			go func(s subscription) {
				defer func() { <-b.sem }()
				if err := s.handler(ctx, e); err != nil {
					b.log.Error("event handler error (async)",
						"topic", e.Topic,
						"source", e.Meta.PluginSource,
						"error", err,
					)
				}
			}(sub)
		default: // Sync
			if err := sub.handler(ctx, e); err != nil {
				b.log.Error("event handler error (sync)",
					"topic", e.Topic,
					"source", e.Meta.PluginSource,
					"error", err,
				)
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// ── Wildcard matching ─────────────────────────────────────────────────────────

// topicMatches reports whether topic matches a pattern.
// Pattern rules:
//   - Exact: "storage.uploaded" matches only "storage.uploaded"
//   - Wildcard suffix: "storage.*" matches anything with "storage." prefix
//   - Global wildcard: "*" matches everything
func topicMatches(pattern, topic string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(topic, prefix)
	}
	return pattern == topic
}

// ── NoopBus (for testing / opt-out) ──────────────────────────────────────────

// NoopBus discards all events. Safe for plugins that don't need bus services.
type NoopBus struct{}

func (NoopBus) Publish(_ context.Context, _ Event) error { return nil }
func (NoopBus) Subscribe(_ string, _ Handler)            {}
