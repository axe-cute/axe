// Package txmanager provides a database transaction manager for webtoon.
package txmanager

import (
	"context"
	"database/sql"
	"fmt"
)

// Manager wraps a *sql.DB to provide explicit transaction management.
type Manager struct {
	db *sql.DB
}

// New creates a new transaction Manager.
func New(db *sql.DB) *Manager {
	return &Manager{db: db}
}

// WithTx executes fn inside a database transaction.
// It commits on success and rolls back on any error or panic.
func (m *Manager) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx failed: %w; rollback failed: %v", err, rbErr)
		}
		return err
	}

	return tx.Commit()
}
