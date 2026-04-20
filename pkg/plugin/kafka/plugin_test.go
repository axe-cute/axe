package kafka

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/plugin"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_NoBrokersNoBackend(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Brokers")
}

func TestNew_NoBrokersWithBackend(t *testing.T) {
	// Backend set → no brokers required.
	p, err := New(Config{Backend: NewInProcessBackend()})
	require.NoError(t, err)
	assert.Equal(t, "kafka", p.Name())
}

func TestNew_WithBrokers(t *testing.T) {
	p, err := New(Config{Brokers: []string{"localhost:9092"}})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNew_Defaults(t *testing.T) {
	p, err := New(Config{Brokers: []string{"localhost:9092"}})
	require.NoError(t, err)
	assert.Equal(t, "dead-letter-queue", p.cfg.DLQTopic)
	assert.Equal(t, 30*time.Second, p.cfg.ShutdownTimeout)
}

// ── Plugin lifecycle ──────────────────────────────────────────────────────────

func TestRegister_ProvidesPublisher(t *testing.T) {
	p, err := New(Config{Backend: NewInProcessBackend()})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	pub, ok := plugin.Resolve[Publisher](app, ServiceKey)
	require.True(t, ok, "Publisher must be provided via service locator")
	assert.NotNil(t, pub)
}

func TestMinAxeVersion(t *testing.T) {
	p, _ := New(Config{Brokers: []string{"b:9092"}})
	assert.NotEmpty(t, p.MinAxeVersion())
}

func TestShutdown_NoSubscribers(t *testing.T) {
	p, _ := New(Config{Backend: NewInProcessBackend()})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Publish ───────────────────────────────────────────────────────────────────

func TestPublish_ReachesBackend(t *testing.T) {
	backend := NewInProcessBackend()
	p, err := New(Config{Backend: backend})
	require.NoError(t, err)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	err = p.Publish(t.Context(), "user.events", []byte("u-1"), []byte(`{"action":"login"}`))
	require.NoError(t, err)

	msgs := backend.Sent("user.events")
	require.Len(t, msgs, 1)
	assert.Equal(t, []byte("u-1"), msgs[0].Key)
	assert.Equal(t, []byte(`{"action":"login"}`), msgs[0].Value)
}

func TestPublish_BackendError_WrapsError(t *testing.T) {
	p, _ := New(Config{Backend: &errBackend{err: errors.New("broker unavailable")}})
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	err := p.Publish(t.Context(), "events", nil, []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kafka")
	assert.Contains(t, err.Error(), "broker unavailable")
}

// ── Subscribe + consumer goroutine ───────────────────────────────────────────

func TestSubscribe_DeliversMessage(t *testing.T) {
	backend := NewInProcessBackend()
	p, _ := New(Config{Backend: backend, GroupID: "test-group"})

	received := make(chan Message, 1)
	p.Subscribe("orders", func(ctx context.Context, msg Message) error {
		received <- msg
		return nil
	})

	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	// Give the goroutine time to start listening.
	time.Sleep(20 * time.Millisecond)

	err := backend.Send(t.Context(), Message{Topic: "orders", Value: []byte("order-1")})
	require.NoError(t, err)

	select {
	case msg := <-received:
		assert.Equal(t, []byte("order-1"), msg.Value)
	case <-time.After(time.Second):
		t.Fatal("timeout: message not delivered to subscriber")
	}
}

func TestSubscribe_MultipleTopics(t *testing.T) {
	backend := NewInProcessBackend()
	p, _ := New(Config{Backend: backend, GroupID: "g"})

	var mu sync.Mutex
	topics := make(map[string]bool)

	for _, topic := range []string{"orders", "payments"} {
		topic := topic
		p.Subscribe(topic, func(_ context.Context, msg Message) error {
			mu.Lock()
			topics[msg.Topic] = true
			mu.Unlock()
			return nil
		})
	}

	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))
	time.Sleep(20 * time.Millisecond)

	_ = backend.Send(t.Context(), Message{Topic: "orders", Value: []byte("o1")})
	_ = backend.Send(t.Context(), Message{Topic: "payments", Value: []byte("p1")})
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, topics["orders"], "orders handler must fire")
	assert.True(t, topics["payments"], "payments handler must fire")
}

// ── Dead Letter Queue ─────────────────────────────────────────────────────────

func TestSubscribe_HandlerError_RoutesToDLQ(t *testing.T) {
	backend := NewInProcessBackend()
	p, _ := New(Config{Backend: backend, GroupID: "g", DLQTopic: "my-dlq"})

	p.Subscribe("orders", func(_ context.Context, _ Message) error {
		return errors.New("processing failed")
	})

	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))
	time.Sleep(20 * time.Millisecond)

	_ = backend.Send(t.Context(), Message{Topic: "orders", Value: []byte("bad-msg")})
	time.Sleep(100 * time.Millisecond)

	dlq := backend.Sent("my-dlq")
	require.Len(t, dlq, 1, "failed message must be routed to DLQ")
	assert.Equal(t, "orders", dlq[0].Headers["x-original-topic"])
	assert.Contains(t, dlq[0].Headers["x-error"], "processing failed")
}

// ── InProcessBackend ─────────────────────────────────────────────────────────

func TestInProcessBackend_SentIsThreadSafe(t *testing.T) {
	backend := NewInProcessBackend()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = backend.Send(context.Background(), Message{Topic: "t", Value: []byte("x")})
		}()
	}
	wg.Wait()
	assert.Len(t, backend.Sent("t"), 50)
}

// ── ServiceKey ────────────────────────────────────────────────────────────────

func TestServiceKey_IsKafka(t *testing.T) {
	p, _ := New(Config{Backend: NewInProcessBackend()})
	assert.Equal(t, p.Name(), ServiceKey)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// errBackend always returns an error from Send.
type errBackend struct{ err error }

func (b *errBackend) Send(_ context.Context, _ Message) error { return b.err }
func (b *errBackend) Receive(ctx context.Context, _, _ string, _ Handler) error {
	<-ctx.Done()
	return ctx.Err()
}
func (b *errBackend) Close() error { return nil }
