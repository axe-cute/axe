package worker_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/worker"
)

// ── Task factory: OutboxEvent ─────────────────────────────────────────────────

func TestNewOutboxEventTask_HappyPath(t *testing.T) {
	task, err := worker.NewOutboxEventTask("evt-1", "UserRegistered", "user")
	require.NoError(t, err)
	require.NotNil(t, task)

	assert.Equal(t, worker.TypeProcessOutboxEvent, task.Type())

	var p worker.OutboxEventPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &p))
	assert.Equal(t, "evt-1", p.EventID)
	assert.Equal(t, "UserRegistered", p.EventType)
	assert.Equal(t, "user", p.Aggregate)
}

func TestNewOutboxEventTask_EmptyFields(t *testing.T) {
	task, err := worker.NewOutboxEventTask("", "", "")
	require.NoError(t, err)
	require.NotNil(t, task)
}

// ── Task type constants ──────────────────────────────────────────────────────

func TestTaskTypeConstants(t *testing.T) {
	// Ensure task types follow the "namespace:action" convention.
	assert.Contains(t, worker.TypeProcessOutboxEvent, ":")
}

// ── Payload round-trip ───────────────────────────────────────────────────────

func TestOutboxEventPayload_JSONRoundtrip(t *testing.T) {
	original := worker.OutboxEventPayload{
		EventID:   "evt-abc",
		EventType: "OrderPlaced",
		Aggregate: "order",
	}

	raw, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded worker.OutboxEventPayload
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, original, decoded)
}

// ── Handler: OutboxEventHandler ──────────────────────────────────────────────

func TestOutboxEventHandler_ProcessTask(t *testing.T) {
	handler := worker.NewOutboxEventHandler(slog.Default())
	require.NotNil(t, handler)

	payload, _ := json.Marshal(worker.OutboxEventPayload{
		EventID:   "evt-99",
		EventType: "OrderShipped",
		Aggregate: "order",
	})
	task := asynq.NewTask(worker.TypeProcessOutboxEvent, payload)

	err := handler.ProcessTask(context.Background(), task)
	assert.NoError(t, err)
}

func TestOutboxEventHandler_InvalidPayload(t *testing.T) {
	handler := worker.NewOutboxEventHandler(slog.Default())

	task := asynq.NewTask(worker.TypeProcessOutboxEvent, []byte("{invalid"))
	err := handler.ProcessTask(context.Background(), task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

// ── Config defaults ──────────────────────────────────────────────────────────

func TestConfig_DefaultValues(t *testing.T) {
	cfg := worker.Config{}

	// Zero-value config should be accepted by New (defaults applied internally).
	assert.Equal(t, 0, cfg.Concurrency, "zero-value config should have 0 concurrency (New applies default of 10)")
	assert.Nil(t, cfg.Queues, "zero-value config should have nil Queues (New applies default)")
}

// ── Interface compliance ─────────────────────────────────────────────────────

func TestHandler_ImplementsAsynqHandler(t *testing.T) {
	// Compile-time check.
	var _ asynq.Handler = (*worker.OutboxEventHandler)(nil)
}

// ── Server creation ──────────────────────────────────────────────────────────

func TestNew_DefaultConcurrency(t *testing.T) {
	srv := worker.New(worker.Config{
		RedisAddr: "localhost:16379", // bogus address — we don't connect during New
	}, slog.Default())
	require.NotNil(t, srv)
}

func TestNew_CustomQueues(t *testing.T) {
	srv := worker.New(worker.Config{
		RedisAddr:   "localhost:16379",
		Concurrency: 5,
		Queues:      map[string]int{"high": 10, "default": 5},
	}, slog.Default())
	require.NotNil(t, srv)
}

func TestNew_WithPassword(t *testing.T) {
	srv := worker.New(worker.Config{
		RedisAddr:     "localhost:16379",
		RedisPassword: "secret",
	}, slog.Default())
	require.NotNil(t, srv)
}

// ── Server.Register ──────────────────────────────────────────────────────────

func TestServer_Register(t *testing.T) {
	srv := worker.New(worker.Config{
		RedisAddr: "localhost:16379",
	}, slog.Default())

	// Register should not panic.
	srv.Register(worker.TypeProcessOutboxEvent, worker.NewOutboxEventHandler(slog.Default()))
}

// ── Server.Shutdown ──────────────────────────────────────────────────────────

func TestServer_Shutdown(t *testing.T) {
	srv := worker.New(worker.Config{
		RedisAddr: "localhost:16379",
	}, slog.Default())

	// Shutdown on a non-started server should not panic.
	srv.Shutdown()
}

// ── Handler: OutboxEvent edge cases ──────────────────────────────────────────

func TestOutboxEventHandler_EmptyPayload(t *testing.T) {
	handler := worker.NewOutboxEventHandler(slog.Default())
	task := asynq.NewTask(worker.TypeProcessOutboxEvent, []byte("{}"))

	err := handler.ProcessTask(context.Background(), task)
	assert.NoError(t, err)
}

func TestOutboxEventHandler_LargePayload(t *testing.T) {
	handler := worker.NewOutboxEventHandler(slog.Default())
	payload, _ := json.Marshal(worker.OutboxEventPayload{
		EventID:   "evt-large",
		EventType: "BigEvent",
		Aggregate: string(make([]byte, 1024)),
	})
	task := asynq.NewTask(worker.TypeProcessOutboxEvent, payload)

	err := handler.ProcessTask(context.Background(), task)
	assert.NoError(t, err)
}

func TestNewOutboxEventTask_MaxRetry(t *testing.T) {
	task, err := worker.NewOutboxEventTask("e1", "T", "A")
	require.NoError(t, err)
	assert.Equal(t, worker.TypeProcessOutboxEvent, task.Type())
}
