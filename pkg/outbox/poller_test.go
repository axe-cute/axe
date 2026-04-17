package outbox_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/outbox"
)

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

// ── Config defaults ───────────────────────────────────────────────────────────

func TestConfig_DefaultsApplied(t *testing.T) {
	// Poller.New should apply defaults without panicking
	db := openTestDB(t)
	// We use a non-reachable Redis addr — New only creates the client, doesn't ping
	p := outbox.New(db, "localhost:6399", outbox.Config{}, slog.Default())
	require.NotNil(t, p)
}

// ── Poller struct construction ────────────────────────────────────────────────

func TestNew_WithCustomConfig(t *testing.T) {
	db := openTestDB(t)
	p := outbox.New(db, "localhost:6399", outbox.Config{
		Interval:  2 * time.Second,
		BatchSize: 100,
	}, slog.Default())
	require.NotNil(t, p, "New() should return a non-nil Poller")
}

// ── poll via Start — short context ───────────────────────────────────────────

func TestStart_CancelImmediately(t *testing.T) {
	db := openTestDB(t)
	p := outbox.New(db, "localhost:63999", outbox.Config{Interval: 100 * time.Millisecond}, slog.Default())

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
	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM outbox_events
		WHERE id = 'evt-proc-1' AND processed_at IS NULL
	`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "processed event should not appear in unprocessed query")
}

// ── retries cap ───────────────────────────────────────────────────────────────

func TestOutboxTable_RetryCapExcludes(t *testing.T) {
	db := openTestDB(t)
	insertEvent(t, db, "evt-retry-1", "PaymentFailed", "payment")

	// Exhaust retries
	_, err := db.Exec(`UPDATE outbox_events SET retries = 5 WHERE id = ?`, "evt-retry-1")
	require.NoError(t, err)

	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM outbox_events
		WHERE id = 'evt-retry-1'
		  AND processed_at IS NULL
		  AND retries < 5
	`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "event with retries >= 5 should be excluded from poll query")
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
