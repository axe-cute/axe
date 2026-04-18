package txmanager_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/txmanager"
)

// ── Test DB ──────────────────────────────────────────────────────────────────

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(t, err)
	return db
}

func countItems(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM items").Scan(&n)
	require.NoError(t, err)
	return n
}

// ── ExtractTx ────────────────────────────────────────────────────────────────

func TestExtractTx_NoTxInContext(t *testing.T) {
	tx := txmanager.ExtractTx(context.Background())
	assert.Nil(t, tx, "ExtractTx should return nil when no tx in context")
}

func TestExtractTx_WrongValueType(t *testing.T) {
	// Putting a non-*sql.Tx into context shouldn't crash.
	ctx := context.WithValue(context.Background(), struct{}{}, "not-a-tx")
	tx := txmanager.ExtractTx(ctx)
	assert.Nil(t, tx)
}

// ── ExtractTxOrDB ────────────────────────────────────────────────────────────

func TestExtractTxOrDB_FallsBackToDB(t *testing.T) {
	db := openTestDB(t)
	result := txmanager.ExtractTxOrDB(context.Background(), db)
	require.NotNil(t, result, "should fall back to *sql.DB when no tx in context")
}

// ── WithinTransaction — Commit ──────────────────────────────────────────────

func TestWithinTransaction_Commit(t *testing.T) {
	db := openTestDB(t)
	tm := txmanager.New(db)

	err := tm.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx := txmanager.ExtractTx(ctx)
		require.NotNil(t, tx, "tx should be injected into context")
		_, err := tx.Exec("INSERT INTO items (name) VALUES (?)", "committed-item")
		return err
	})

	require.NoError(t, err)
	assert.Equal(t, 1, countItems(t, db), "committed row should persist")
}

// ── WithinTransaction — Rollback on error ───────────────────────────────────

func TestWithinTransaction_RollbackOnError(t *testing.T) {
	db := openTestDB(t)
	tm := txmanager.New(db)

	sentinel := errors.New("business logic failed")

	err := tm.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx := txmanager.ExtractTx(ctx)
		_, _ = tx.Exec("INSERT INTO items (name) VALUES (?)", "should-be-rolled-back")
		return sentinel
	})

	require.ErrorIs(t, err, sentinel, "original error should be propagated")
	assert.Equal(t, 0, countItems(t, db), "row should be rolled back")
}

// ── WithinTransaction — Multiple operations in same TX ──────────────────────

func TestWithinTransaction_MultipleOps(t *testing.T) {
	db := openTestDB(t)
	tm := txmanager.New(db)

	err := tm.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx := txmanager.ExtractTx(ctx)
		if _, err := tx.Exec("INSERT INTO items (name) VALUES (?)", "item-1"); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO items (name) VALUES (?)", "item-2"); err != nil {
			return err
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, countItems(t, db), "both rows should be committed")
}

func TestWithinTransaction_MultipleOps_PartialFail(t *testing.T) {
	db := openTestDB(t)
	tm := txmanager.New(db)

	err := tm.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx := txmanager.ExtractTx(ctx)
		if _, err := tx.Exec("INSERT INTO items (name) VALUES (?)", "item-ok"); err != nil {
			return err
		}
		// Fail after first insert — both should be rolled back.
		return fmt.Errorf("failed after first insert")
	})

	require.Error(t, err)
	assert.Equal(t, 0, countItems(t, db), "all rows should be rolled back on partial failure")
}

// ── WithinTransaction — ExtractTxOrDB inside TX ─────────────────────────────

func TestWithinTransaction_ExtractTxOrDB_UsesTx(t *testing.T) {
	db := openTestDB(t)
	tm := txmanager.New(db)

	err := tm.WithinTransaction(context.Background(), func(ctx context.Context) error {
		executor := txmanager.ExtractTxOrDB(ctx, db)
		require.NotNil(t, executor)
		// Should be a *sql.Tx, not *sql.DB.
		_, err := executor.ExecContext(ctx, "INSERT INTO items (name) VALUES (?)", "via-executor")
		return err
	})

	require.NoError(t, err)
	assert.Equal(t, 1, countItems(t, db))
}

// ── WithinTransaction — Cancelled context ───────────────────────────────────

func TestWithinTransaction_CancelledContext(t *testing.T) {
	db := openTestDB(t)
	tm := txmanager.New(db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting tx

	err := tm.WithinTransaction(ctx, func(_ context.Context) error {
		return nil
	})

	// BeginTx should fail with cancelled context.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// ── Interface compliance ────────────────────────────────────────────────────

func TestTxManager_InterfaceCompliance(t *testing.T) {
	db := openTestDB(t)
	var _ txmanager.TxManager = txmanager.New(db)
}

// ── Mock TxManager ──────────────────────────────────────────────────────────

type mockTxManager struct {
	err error
}

func (m *mockTxManager) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	if m.err != nil {
		return m.err
	}
	return fn(ctx)
}

func TestMockTxManager_HappyPath(t *testing.T) {
	called := false
	m := &mockTxManager{}

	err := m.WithinTransaction(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestMockTxManager_ErrorPath(t *testing.T) {
	sentinel := errors.New("db error")
	m := &mockTxManager{err: sentinel}

	err := m.WithinTransaction(context.Background(), func(_ context.Context) error {
		return nil
	})

	assert.ErrorIs(t, err, sentinel)
}
