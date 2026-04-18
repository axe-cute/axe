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

// ── Task factory: WelcomeEmail ────────────────────────────────────────────────

func TestNewWelcomeEmailTask_HappyPath(t *testing.T) {
	task, err := worker.NewWelcomeEmailTask("user-123", "user@example.com", "Alice")
	require.NoError(t, err)
	require.NotNil(t, task)

	assert.Equal(t, worker.TypeSendWelcomeEmail, task.Type())

	var p worker.WelcomeEmailPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &p))
	assert.Equal(t, "user-123", p.UserID)
	assert.Equal(t, "user@example.com", p.Email)
	assert.Equal(t, "Alice", p.Name)
}

func TestNewWelcomeEmailTask_EmptyFields(t *testing.T) {
	// Should not fail with empty strings — that's the caller's problem.
	task, err := worker.NewWelcomeEmailTask("", "", "")
	require.NoError(t, err)
	require.NotNil(t, task)

	var p worker.WelcomeEmailPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &p))
	assert.Empty(t, p.UserID)
}

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
	assert.Contains(t, worker.TypeSendWelcomeEmail, ":")
	assert.Contains(t, worker.TypeProcessOutboxEvent, ":")

	// Ensure they're distinct.
	assert.NotEqual(t, worker.TypeSendWelcomeEmail, worker.TypeProcessOutboxEvent)
}

// ── Payload round-trip ───────────────────────────────────────────────────────

func TestWelcomeEmailPayload_JSONRoundtrip(t *testing.T) {
	original := worker.WelcomeEmailPayload{
		UserID: "uid-abc",
		Email:  "test@test.com",
		Name:   "Nguyen Van A",
	}

	raw, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded worker.WelcomeEmailPayload
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, original, decoded)
}

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

// ── Handler: WelcomeEmailHandler ─────────────────────────────────────────────

func TestWelcomeEmailHandler_ProcessTask(t *testing.T) {
	handler := worker.NewWelcomeEmailHandler(slog.Default()) // nil logger OK in test
	require.NotNil(t, handler)

	// Create a valid task payload.
	payload, _ := json.Marshal(worker.WelcomeEmailPayload{
		UserID: "user-1",
		Email:  "user@example.com",
		Name:   "Test",
	})
	task := asynq.NewTask(worker.TypeSendWelcomeEmail, payload)

	err := handler.ProcessTask(context.Background(), task)
	assert.NoError(t, err)
}

func TestWelcomeEmailHandler_InvalidPayload(t *testing.T) {
	handler := worker.NewWelcomeEmailHandler(slog.Default())

	task := asynq.NewTask(worker.TypeSendWelcomeEmail, []byte("not json"))
	err := handler.ProcessTask(context.Background(), task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

// ── Handler: OutboxEventHandler ──────────────────────────────────────────────

func TestOutboxEventHandler_ProcessTask(t *testing.T) {
	handler := worker.NewOutboxEventHandler(slog.Default()) // nil logger OK in test
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
	// Compile-time checks.
	var _ asynq.Handler = (*worker.WelcomeEmailHandler)(nil)
	var _ asynq.Handler = (*worker.OutboxEventHandler)(nil)
}
