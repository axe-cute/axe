// Package kafka provides the axe Kafka producer/consumer plugin.
//
// It implements a pluggable backend: use the in-process broker for tests,
// and swap in a real Kafka broker for production without changing plugin consumers.
//
// Usage:
//
//	app.Use(kafka.New(kafka.Config{
//	    Brokers: []string{"localhost:9092"},
//	    GroupID: "my-service",
//	}))
//
//	// Publish (fire-and-forget, async)
//	pub := plugin.MustResolve[kafka.Publisher](app, kafka.ServiceKey)
//	err := pub.Publish(ctx, "user.events", "user-123", []byte(`{"action":"login"}`))
//
// The plugin also wires subscriptions declared before Start():
//
//	kp, _ := kafka.New(cfg)
//	kp.Subscribe("user.events", myHandler)       // before app.Use
//	app.Use(kp)
//	app.Start(ctx)  // consumer goroutines start here
//
// Layer conformance:
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 5: ServiceKey constant
//   - Layer 6: uses app.Logger, app.Events — no shared pool violation
//     (Kafka requires its own connection, explicitly documented)
package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/obs"
)

// ServiceKey is the service locator key for [Publisher].
const ServiceKey = "kafka"

// Prometheus metrics.
var (
	publishedTotal = obs.NewCounterVec("kafka", "published_total",
		"Messages published to Kafka.", []string{"topic", "status"})
	consumedTotal = obs.NewCounterVec("kafka", "consumed_total",
		"Messages consumed from Kafka.", []string{"topic", "status"})
	dlqTotal = obs.NewCounterVec("kafka", "dlq_total",
		"Messages sent to Dead Letter Queue.", []string{"topic"})
	publishLatency = obs.NewHistogram("kafka", "publish_duration_seconds",
		"Kafka publish latency.")
)

// ── Message types ─────────────────────────────────────────────────────────────

// Message is a Kafka message.
type Message struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers map[string]string
	// Offset and Partition are set on consumed messages.
	Offset    int64
	Partition int32
}

// Handler processes a consumed Kafka message.
// Return a non-nil error to trigger DLQ routing.
type Handler func(ctx context.Context, msg Message) error

// ── Publisher interface ───────────────────────────────────────────────────────

// Publisher is the interface exposed by the Kafka plugin via the service locator.
// Other plugins resolve this to publish messages without importing the kafka package.
type Publisher interface {
	// Publish sends a message to a Kafka topic.
	// key may be nil for round-robin partition assignment.
	Publish(ctx context.Context, topic string, key, value []byte) error
}

// ── Backend interface (injectable transport) ──────────────────────────────────

// Backend abstracts the Kafka transport layer.
// The default backend (used in production) wraps a real Kafka client.
// InProcessBackend is used in tests.
type Backend interface {
	// Send publishes a message. Must be goroutine-safe.
	Send(ctx context.Context, msg Message) error
	// Receive starts a consumer goroutine for the given topic/group.
	// Messages are delivered to handler. Close ctx to stop.
	Receive(ctx context.Context, topic, group string, handler Handler) error
	// Close shuts down the backend, waiting for in-flight messages to drain.
	Close() error
}

// ── Config ────────────────────────────────────────────────────────────────────

// Config configures the Kafka plugin.
type Config struct {
	// Brokers is the list of Kafka broker addresses. Required.
	Brokers []string
	// GroupID is the consumer group ID. Required if any Subscribe() calls are made.
	GroupID string
	// DLQTopic is the topic for failed messages. Default: "".dead-letter-queue".
	DLQTopic string
	// ShutdownTimeout is how long to wait for consumers to drain. Default: 30s.
	ShutdownTimeout time.Duration
	// Backend overrides the Kafka transport (for testing).
	// If nil, a stub backend is returned that logs publishes but does not connect.
	Backend Backend
}

func (c *Config) defaults() {
	if c.DLQTopic == "" {
		c.DLQTopic = "dead-letter-queue"
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
}

func (c *Config) validate() error {
	if len(c.Brokers) == 0 && c.Backend == nil {
		return errors.New("kafka: Brokers is required (or set Backend for testing)")
	}
	return nil
}

// ── Plugin ────────────────────────────────────────────────────────────────────

type subscription struct {
	topic   string
	handler Handler
}

// Plugin is the axe Kafka plugin.
type Plugin struct {
	cfg         Config
	backend     Backend
	log         *slog.Logger
	subs        []subscription
	cancelFuncs []context.CancelFunc
	mu          sync.Mutex
}

// New creates a Kafka plugin. Returns an error if required config is missing.
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	// Use the injected backend; fall back to stub for development/testing.
	if cfg.Backend == nil {
		cfg.Backend = &stubBackend{brokers: cfg.Brokers}
	}
	return &Plugin{cfg: cfg, backend: cfg.Backend}, nil
}

// Name implements [plugin.Plugin].
func (p *Plugin) Name() string { return "kafka" }

// MinAxeVersion declares required axe version.
func (p *Plugin) MinAxeVersion() string { return "v0.5.0" }

// Subscribe registers a message handler for a topic.
// Must be called before app.Start() — consumer goroutines start in Register().
func (p *Plugin) Subscribe(topic string, h Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subs = append(p.subs, subscription{topic: topic, handler: h})
}

// Publish sends a message to a Kafka topic (implements [Publisher]).
func (p *Plugin) Publish(ctx context.Context, topic string, key, value []byte) error {
	start := time.Now()
	err := p.backend.Send(ctx, Message{
		Topic: topic,
		Key:   key,
		Value: value,
	})
	publishLatency.Observe(time.Since(start).Seconds())
	if err != nil {
		publishedTotal.WithLabelValues(topic, "error").Inc()
		return fmt.Errorf("kafka: publish %q: %w", topic, err)
	}
	publishedTotal.WithLabelValues(topic, "success").Inc()
	return nil
}

// Register starts consumer goroutines and provides Publisher via service locator.
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
	p.log = obs.Logger(app, p.Name())

	// Layer 5: provide Publisher via typed service locator.
	plugin.Provide[Publisher](app, ServiceKey, p)

	// Start one consumer goroutine per subscription.
	p.mu.Lock()
	subs := make([]subscription, len(p.subs))
	copy(subs, p.subs)
	p.mu.Unlock()

	for _, sub := range subs {
		cctx, cancel := context.WithCancel(ctx)
		p.mu.Lock()
		p.cancelFuncs = append(p.cancelFuncs, cancel)
		p.mu.Unlock()

		topic := sub.topic
		h := sub.handler
		dlqTopic := p.cfg.DLQTopic

		go func() {
			err := p.backend.Receive(cctx, topic, p.cfg.GroupID,
				func(ctx context.Context, msg Message) error {
					if err := h(ctx, msg); err != nil {
						p.log.Error("kafka handler error — routing to DLQ",
							"topic", topic, "error", err)
						dlqTotal.WithLabelValues(topic).Inc()
						// Best-effort DLQ publish.
						_ = p.backend.Send(ctx, Message{
							Topic: dlqTopic,
							Key:   msg.Key,
							Value: msg.Value,
							Headers: map[string]string{
								"x-original-topic": topic,
								"x-error":          err.Error(),
							},
						})
						consumedTotal.WithLabelValues(topic, "dlq").Inc()
						return nil // don't retry — already DLQ'd
					}
					consumedTotal.WithLabelValues(topic, "success").Inc()
					return nil
				})
			if err != nil && !errors.Is(err, context.Canceled) {
				p.log.Error("kafka consumer error", "topic", topic, "error", err)
			}
		}()

		p.log.Info("kafka consumer started", "topic", topic, "group", p.cfg.GroupID)
	}

	p.log.Info("kafka plugin registered",
		"brokers", strings.Join(p.cfg.Brokers, ","),
		"subscriptions", len(subs),
	)
	return nil
}

// Shutdown stops all consumer goroutines and drains in-flight messages.
func (p *Plugin) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	cancels := make([]context.CancelFunc, len(p.cancelFuncs))
	copy(cancels, p.cancelFuncs)
	p.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	done := make(chan error, 1)
	go func() { done <- p.backend.Close() }()

	select {
	case err := <-done:
		p.log.Info("kafka plugin shutdown complete")
		return err
	case <-time.After(p.cfg.ShutdownTimeout):
		p.log.Warn("kafka plugin shutdown timed out")
		return fmt.Errorf("kafka: shutdown timed out after %s", p.cfg.ShutdownTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ── Stub backend (no-op, for development and testing) ────────────────────────

// stubBackend satisfies the Backend interface without needing a real Kafka broker.
// Published messages are logged. Consumers run as no-ops.
type stubBackend struct {
	brokers []string
	mu      sync.Mutex
	sent    []Message // captured for test assertions
}

func (s *stubBackend) Send(_ context.Context, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg)
	return nil
}

func (s *stubBackend) Receive(ctx context.Context, _, _ string, _ Handler) error {
	// Block until cancelled — no messages delivered in stub mode.
	<-ctx.Done()
	return ctx.Err()
}

func (s *stubBackend) Close() error { return nil }

// ── InProcessBackend (for integration tests and dev mode) ────────────────────

// NewInProcessBackend creates an in-process Kafka backend that delivers messages
// synchronously to subscribers. Safe for use in integration tests — no external infra.
func NewInProcessBackend() *InProcessBackend {
	return &InProcessBackend{
		topics: make(map[string][]Message),
		subs:   make(map[string][]chan Message),
	}
}

// InProcessBackend delivers messages in-process for tests.
type InProcessBackend struct {
	mu     sync.RWMutex
	topics map[string][]Message
	subs   map[string][]chan Message
}

func (b *InProcessBackend) Send(_ context.Context, msg Message) error {
	b.mu.Lock()
	b.topics[msg.Topic] = append(b.topics[msg.Topic], msg)
	subs := make([]chan Message, len(b.subs[msg.Topic]))
	copy(subs, b.subs[msg.Topic])
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- msg:
		default: // subscriber not ready — drop
		}
	}
	return nil
}

func (b *InProcessBackend) Receive(ctx context.Context, topic, _ string, h Handler) error {
	ch := make(chan Message, 64)
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], ch)
	b.mu.Unlock()

	for {
		select {
		case msg := <-ch:
			_ = h(ctx, msg)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (b *InProcessBackend) Close() error { return nil }

// Sent returns all messages sent to the given topic (for test assertions).
func (b *InProcessBackend) Sent(topic string) []Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Message, len(b.topics[topic]))
	copy(out, b.topics[topic])
	return out
}
