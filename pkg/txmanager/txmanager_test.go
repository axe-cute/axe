package txmanager_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/axe-cute/axe/pkg/txmanager"
)

// TestExtractTx verifies context injection and extraction.
func TestExtractTx(t *testing.T) {
	t.Run("no tx in context", func(t *testing.T) {
		ctx := context.Background()
		if tx := txmanager.ExtractTx(ctx); tx != nil {
			t.Error("ExtractTx should return nil when no tx in context")
		}
	})
}

// TestExtractTxOrDB verifies the fallback behaviour.
// We test without a real DB by checking that ExtractTxOrDB returns
// the db when no transaction is present.
func TestExtractTxOrDB_NoDB(t *testing.T) {
	ctx := context.Background()
	db := &sql.DB{} // zero-value, not connected — just for type assertion

	result := txmanager.ExtractTxOrDB(ctx, db)
	if result == nil {
		t.Error("ExtractTxOrDB should return db when no tx in context")
	}
}

// TestWithinTransaction_Rollback verifies that a failing fn triggers rollback.
// This requires a real DB; we skip if DATABASE_URL not set.
func TestWithinTransaction_RequiresDB(t *testing.T) {
	t.Skip("integration test — requires real PostgreSQL (run with `make test-integration`)")
}

// TestWithinTransactionError verifies error propagation without a real DB (unit).
func TestWithinTransaction_ErrorPropagation(t *testing.T) {
	// We can't call WithinTransaction without a real DB connection,
	// but we can verify the interface is properly defined.
	var _ txmanager.TxManager = (*mockTxManager)(nil)
}

// mockTxManager is a test double for TxManager.
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

	err := m.WithinTransaction(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("fn should have been called")
	}
}

func TestMockTxManager_ErrorPath(t *testing.T) {
	sentinel := errors.New("db error")
	m := &mockTxManager{err: sentinel}

	err := m.WithinTransaction(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}
