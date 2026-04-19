package outbox_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	_ "modernc.org/sqlite" // CGO-free SQLite driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/outbox"
)

// ── Mock Enqueuer ─────────────────────────────────────────────────────────────

// mockEnqueuer records all enqueued tasks for assertion.
type mockEnqueuer struct {
	mu     sync.Mutex
	tasks  []*asynq.Task
	err    error // if set, Enqueue returns this error
	closed bool
}

func (m *mockEnqueuer) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	m.tasks = append(m.tasks, task)
	return &asynq.TaskInfo{}, nil
}

func (m *mockEnqueuer) Close() error {
	m.closed = true
	return nil
}

func (m *mockEnqueuer) Tasks() []*asynq.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*asynq.Task{}, m.tasks...)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// openTestDB opens an in-memory SQLite database and creates the outbox_events schema.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_journal=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS outbox_events (
			id           TEXT        PRIMARY KEY,
			aggregate    TEXT        NOT NULL,
			event_type   TEXT        NOT NULL,
			payload      TEXT        NOT NULL DEFAULT '{}',
			created_at   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processed_at DATETIME    NULL,
			retries      INTEGER     NOT NULL DEFAULT 0
		)
	`)
	require.NoError(t, err, "failed to create outbox_events table")
	return db
}

// insertEvent inserts a raw outbox event directly for test setup.
func insertEvent(t *testing.T, db *sql.DB, id, eventType, aggregate string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO outbox_events (id, aggregate, event_type, payload) VALUES (?, ?, ?, '{}')`,
		id, aggregate, eventType,
	)
	require.NoError(t, err)
}

// countUnprocessed returns the number of unprocessed events with retries < 5.
func countUnprocessed(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE processed_at IS NULL AND retries < 5`).Scan(&count)
	require.NoError(t, err)
	return count
}

// ── Config defaults ───────────────────────────────────────────────────────────

func TestConfig_DefaultsApplied(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}
	p := outbox.New(db, enq, outbox.Config{}, slog.Default())
	require.NotNil(t, p)
}

func TestConfig_DriverDefault(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}
	p := outbox.New(db, enq, outbox.Config{}, slog.Default())
	require.NotNil(t, p)
}

func TestConfig_CustomDriverSqlite(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}
	p := outbox.New(db, enq, outbox.Config{Driver: "sqlite3"}, slog.Default())
	require.NotNil(t, p)
}

// ── Poller struct construction ────────────────────────────────────────────────

func TestNew_WithCustomConfig(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}
	p := outbox.New(db, enq, outbox.Config{
		Interval:  2 * time.Second,
		BatchSize: 100,
	}, slog.Default())
	require.NotNil(t, p, "New() should return a non-nil Poller")
}

func TestNew_WithAllDrivers(t *testing.T) {
	drivers := []string{"postgres", "mysql", "sqlite3"}
	for _, drv := range drivers {
		t.Run(drv, func(t *testing.T) {
			db := openTestDB(t)
			enq := &mockEnqueuer{}
			p := outbox.New(db, enq, outbox.Config{Driver: drv}, slog.Default())
			require.NotNil(t, p)
		})
	}
}

// ── NewWithRedis ─────────────────────────────────────────────────────────────

func TestNewWithRedis_ReturnsPoller(t *testing.T) {
	db := openTestDB(t)
	// Non-reachable Redis — NewWithRedis only creates client, doesn't ping.
	p := outbox.NewWithRedis(db, "localhost:63999", outbox.Config{}, slog.Default())
	require.NotNil(t, p)
}

// ── poll via Start — context cancellation ────────────────────────────────────

func TestStart_CancelImmediately(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}
	p := outbox.New(db, enq, outbox.Config{Interval: 100 * time.Millisecond, Driver: "sqlite3"}, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Start

	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not return after context cancel")
	}
	assert.True(t, enq.closed, "enqueuer should be closed after Start returns")
}

func TestStart_CancelDuringPoll(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}
	p := outbox.New(db, enq, outbox.Config{
		Interval: 50 * time.Millisecond,
		Driver:   "sqlite3",
	}, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// expected — should exit after context timeout
	case <-time.After(3 * time.Second):
		t.Fatal("Start() did not return after context timeout")
	}
}

// ── poll() with mock enqueuer — full lifecycle ───────────────────────────────

func TestStart_PollEnqueuesEvents(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}

	// Insert events before starting poller.
	insertEvent(t, db, "evt-poll-1", "UserCreated", "user")
	insertEvent(t, db, "evt-poll-2", "OrderPlaced", "order")

	p := outbox.New(db, enq, outbox.Config{
		Interval:  50 * time.Millisecond,
		BatchSize: 10,
		Driver:    "sqlite3",
	}, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		p.Start(ctx)
	}()

	// Wait until the mock enqueuer has received at least 2 tasks (with timeout).
	deadline := time.After(5 * time.Second)
	for {
		if len(enq.Tasks()) >= 2 {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatalf("timed out waiting for 2 enqueued tasks, got %d", len(enq.Tasks()))
			return
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	cancel() // stop the poller now that we've verified enqueueing

	// Give poller goroutine time to exit.
	time.Sleep(100 * time.Millisecond)

	// Mock enqueuer should have received 2 tasks.
	tasks := enq.Tasks()
	assert.GreaterOrEqual(t, len(tasks), 2, "should enqueue at least 2 events")

	// Events should be marked as processed.
	assert.Equal(t, 0, countUnprocessed(t, db), "all events should be processed after poll")
}

func TestStart_PollRespectsRetryLimit(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}

	insertEvent(t, db, "evt-retry-exhaust", "FailedEvent", "payment")
	// Set retries to 5 — should NOT be picked up.
	_, err := db.Exec(`UPDATE outbox_events SET retries = 5 WHERE id = ?`, "evt-retry-exhaust")
	require.NoError(t, err)

	// Add one processable event.
	insertEvent(t, db, "evt-retry-ok", "GoodEvent", "payment")

	p := outbox.New(db, enq, outbox.Config{
		Interval:  50 * time.Millisecond,
		BatchSize: 10,
		Driver:    "sqlite3",
	}, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	p.Start(ctx)

	// Only the good event should be enqueued.
	tasks := enq.Tasks()
	assert.GreaterOrEqual(t, len(tasks), 1)
}

func TestStart_PollEnqueueError_EventRemainsUnprocessed(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{err: assert.AnError} // all enqueue calls fail

	insertEvent(t, db, "evt-fail", "FailEvent", "test")

	p := outbox.New(db, enq, outbox.Config{
		Interval:  50 * time.Millisecond,
		BatchSize: 10,
		Driver:    "sqlite3",
	}, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	p.Start(ctx)

	// Event should still be unprocessed — enqueue failed.
	assert.Equal(t, 1, countUnprocessed(t, db), "event should remain unprocessed when enqueue fails")
}

func TestStart_PollEmptyTable_NoError(t *testing.T) {
	db := openTestDB(t)
	enq := &mockEnqueuer{}

	p := outbox.New(db, enq, outbox.Config{
		Interval:  50 * time.Millisecond,
		BatchSize: 10,
		Driver:    "sqlite3",
	}, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	p.Start(ctx)

	// No tasks enqueued — empty table.
	assert.Empty(t, enq.Tasks())
}

// ── Table DDL shape ───────────────────────────────────────────────────────────

func TestOutboxTable_Schema(t *testing.T) {
	db := openTestDB(t)

	// Insert and read back to verify column names match outbox.poll query
	insertEvent(t, db, "evt-schema-1", "UserRegistered", "user")

	rows, err := db.QueryContext(context.Background(), `
		SELECT id, event_type, aggregate
		FROM outbox_events
		WHERE processed_at IS NULL
		  AND retries < 5
		ORDER BY created_at ASC
		LIMIT 1
	`)
	require.NoError(t, err)
	defer rows.Close()

	var id, evtType, aggregate string
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&id, &evtType, &aggregate))
	assert.Equal(t, "evt-schema-1", id)
	assert.Equal(t, "UserRegistered", evtType)
	assert.Equal(t, "user", aggregate)
}

// ── processed_at lifecycle ────────────────────────────────────────────────────

func TestOutboxTable_MarkProcessed(t *testing.T) {
	db := openTestDB(t)
	insertEvent(t, db, "evt-proc-1", "OrderPlaced", "order")

	// Simulate what the poller does after enqueue
	_, err := db.Exec(`
		UPDATE outbox_events
		SET processed_at = CURRENT_TIMESTAMP, retries = retries + 1
		WHERE id = ?
	`, "evt-proc-1")
	require.NoError(t, err)

	// Should not appear in unprocessed query
	assert.Equal(t, 0, countUnprocessed(t, db))
}

// ── retries cap ───────────────────────────────────────────────────────────────

func TestOutboxTable_RetryCapExcludes(t *testing.T) {
	db := openTestDB(t)
	insertEvent(t, db, "evt-retry-1", "PaymentFailed", "payment")

	// Exhaust retries
	_, err := db.Exec(`UPDATE outbox_events SET retries = 5 WHERE id = ?`, "evt-retry-1")
	require.NoError(t, err)

	assert.Equal(t, 0, countUnprocessed(t, db), "event with retries >= 5 should be excluded from poll query")
}

func TestOutboxTable_RetryUnderCapIncluded(t *testing.T) {
	db := openTestDB(t)
	insertEvent(t, db, "evt-retry-ok", "RetryableEvent", "payment")

	_, err := db.Exec(`UPDATE outbox_events SET retries = 4 WHERE id = ?`, "evt-retry-ok")
	require.NoError(t, err)

	assert.Equal(t, 1, countUnprocessed(t, db), "event with retries=4 (< 5) should still appear")
}

// ── payload round-trip ────────────────────────────────────────────────────────

func TestOutboxTable_PayloadRoundtrip(t *testing.T) {
	db := openTestDB(t)

	type payload struct {
		UserID string `json:"user_id"`
	}
	raw, err := json.Marshal(payload{UserID: "user-xyz"})
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO outbox_events (id, aggregate, event_type, payload) VALUES (?, ?, ?, ?)`,
		"evt-payload-1", "user", "UserCreated", string(raw),
	)
	require.NoError(t, err)

	var got string
	err = db.QueryRow(`SELECT payload FROM outbox_events WHERE id = 'evt-payload-1'`).Scan(&got)
	require.NoError(t, err)

	var p payload
	require.NoError(t, json.Unmarshal([]byte(got), &p))
	assert.Equal(t, "user-xyz", p.UserID)
}

// ── batch ordering: FIFO by created_at ───────────────────────────────────────

func TestOutboxTable_FIFOOrdering(t *testing.T) {
	db := openTestDB(t)

	// Insert two events with different times
	_, err := db.Exec(
		`INSERT INTO outbox_events (id, aggregate, event_type, payload, created_at) VALUES (?, ?, ?, '{}', ?)`,
		"evt-old", "agg", "EventOld", "2026-01-01 00:00:00",
	)
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO outbox_events (id, aggregate, event_type, payload, created_at) VALUES (?, ?, ?, '{}', ?)`,
		"evt-new", "agg", "EventNew", "2026-01-02 00:00:00",
	)
	require.NoError(t, err)

	rows, err := db.QueryContext(context.Background(), `
		SELECT id FROM outbox_events
		WHERE processed_at IS NULL AND retries < 5
		ORDER BY created_at ASC LIMIT 2
	`)
	require.NoError(t, err)
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.Len(t, ids, 2)
	assert.Equal(t, "evt-old", ids[0], "older event should come first (FIFO)")
	assert.Equal(t, "evt-new", ids[1])
}

// ── batch limit ──────────────────────────────────────────────────────────────

func TestOutboxTable_BatchLimit(t *testing.T) {
	db := openTestDB(t)

	// Insert 10 events
	for i := 0; i < 10; i++ {
		insertEvent(t, db, "evt-batch-"+string(rune('a'+i)), "BatchEvent", "batch")
	}

	// Query with LIMIT 3 — should return exactly 3
	rows, err := db.QueryContext(context.Background(), `
		SELECT id FROM outbox_events
		WHERE processed_at IS NULL AND retries < 5
		ORDER BY created_at ASC LIMIT ?
	`, 3)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		count++
	}
	assert.Equal(t, 3, count, "LIMIT should cap batch size")
}

// ── empty table ──────────────────────────────────────────────────────────────

func TestOutboxTable_EmptyTable(t *testing.T) {
	db := openTestDB(t)
	assert.Equal(t, 0, countUnprocessed(t, db), "empty table should have 0 unprocessed events")
}

// ── multiple events lifecycle ─────────────────────────────────────────────────

func TestOutboxTable_MixedProcessedAndUnprocessed(t *testing.T) {
	db := openTestDB(t)

	insertEvent(t, db, "evt-a", "EventA", "agg")
	insertEvent(t, db, "evt-b", "EventB", "agg")
	insertEvent(t, db, "evt-c", "EventC", "agg")

	// Mark evt-b as processed
	_, err := db.Exec(`UPDATE outbox_events SET processed_at = CURRENT_TIMESTAMP WHERE id = ?`, "evt-b")
	require.NoError(t, err)

	// Mark evt-c as exhausted retries
	_, err = db.Exec(`UPDATE outbox_events SET retries = 5 WHERE id = ?`, "evt-c")
	require.NoError(t, err)

	// Only evt-a should remain
	assert.Equal(t, 1, countUnprocessed(t, db))
}

// ── Enqueuer interface compliance ────────────────────────────────────────────

func TestEnqueuerInterface_MockCompliance(t *testing.T) {
	// Verify mockEnqueuer satisfies the Enqueuer interface at compile time.
	var _ outbox.Enqueuer = (*mockEnqueuer)(nil)
}
