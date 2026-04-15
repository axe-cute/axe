// Package txmanager provides a transaction manager that injects
// a database transaction into a context.Context.
//
// Usage pattern:
//
//	func (s *OrderService) PlaceOrder(ctx context.Context, ...) error {
//	    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
//	        order, err := s.orderRepo.Create(ctx, ...)   // uses tx from ctx
//	        if err != nil { return err }
//	        return s.outboxRepo.Append(ctx, ...)          // same tx
//	    })
//	}
package txmanager

import (
	"context"
	"database/sql"
	"fmt"
)

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

// TxManager defines the contract for transaction management.
// The service layer depends on this interface — never on the concrete type.
type TxManager interface {
	// WithinTransaction executes fn inside a database transaction.
	// The transaction is injected into ctx so repositories can extract it.
	// If fn returns an error, the transaction is rolled back automatically.
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// New creates a new TxManager backed by the given *sql.DB.
func New(db *sql.DB) TxManager {
	return &pgTxManager{db: db}
}

type pgTxManager struct {
	db *sql.DB
}

// WithinTransaction begins a transaction, injects it into ctx, runs fn,
// and commits or rolls back based on the return value.
func (tm *pgTxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := tm.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("txmanager: begin transaction: %w", err)
	}

	// Inject the transaction into the context.
	txCtx := injectTx(ctx, tx)

	if err := fn(txCtx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("txmanager: rollback failed: %v (original: %w)", rbErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("txmanager: commit: %w", err)
	}
	return nil
}

// injectTx stores a *sql.Tx in the context.
func injectTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, contextKey{}, tx)
}

// ExtractTx retrieves a *sql.Tx from the context.
// Returns nil if no transaction is present.
func ExtractTx(ctx context.Context) *sql.Tx {
	tx, _ := ctx.Value(contextKey{}).(*sql.Tx)
	return tx
}

// ExtractTxOrDB returns the transaction from ctx if one exists,
// otherwise falls back to the provided *sql.DB.
// Repositories should call this to support both transactional and non-transactional use.
//
//	func (r *userRepo) Create(ctx context.Context, user domain.User) error {
//	    db := txmanager.ExtractTxOrDB(ctx, r.db)
//	    ...
//	}
func ExtractTxOrDB(ctx context.Context, db *sql.DB) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := ExtractTx(ctx); tx != nil {
		return tx
	}
	return db
}
