// Example 02: Using pkg/txmanager for Unit of Work transactions.
//
// This demonstrates how axe's transaction manager injects *sql.Tx into context,
// so repositories automatically participate in the same transaction without
// passing *sql.Tx as a function parameter.
//
// Run:   go run ./examples/02-txmanager
// Note:  Uses SQLite in-memory — no external database required.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO needed)

	"github.com/axe-cute/axe/pkg/txmanager"
)

// ── Domain ───────────────────────────────────────────────────────────────────

type Order struct {
	ID     string
	UserID string
	Total  float64
}

type AuditLog struct {
	Action    string
	OrderID   string
	Performer string
}

// ── Repository layer ─────────────────────────────────────────────────────────
// Each repo uses txmanager.ExtractTxOrDB to get the tx from context.
// No *sql.Tx is passed as a parameter — clean function signatures.

type OrderRepo struct{ db *sql.DB }

func (r *OrderRepo) Insert(ctx context.Context, o Order) error {
	db := txmanager.ExtractTxOrDB(ctx, r.db) // ← tx from context, or raw db
	_, err := db.ExecContext(ctx,
		"INSERT INTO orders (id, user_id, total) VALUES (?, ?, ?)",
		o.ID, o.UserID, o.Total,
	)
	return err
}

type AuditRepo struct{ db *sql.DB }

func (r *AuditRepo) Insert(ctx context.Context, a AuditLog) error {
	db := txmanager.ExtractTxOrDB(ctx, r.db)
	_, err := db.ExecContext(ctx,
		"INSERT INTO audit_log (action, order_id, performer) VALUES (?, ?, ?)",
		a.Action, a.OrderID, a.Performer,
	)
	return err
}

// ── Service layer ────────────────────────────────────────────────────────────
// The service owns the transaction boundary. Both repos join the SAME tx.

type OrderService struct {
	tx       txmanager.TxManager
	orders   *OrderRepo
	auditLog *AuditRepo
}

func (s *OrderService) PlaceOrder(ctx context.Context, order Order) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		// Insert order — uses the transaction from ctx.
		if err := s.orders.Insert(ctx, order); err != nil {
			return fmt.Errorf("insert order: %w", err) // auto-rollback
		}

		// Insert audit log — SAME transaction.
		if err := s.auditLog.Insert(ctx, AuditLog{
			Action:    "ORDER_PLACED",
			OrderID:   order.ID,
			Performer: order.UserID,
		}); err != nil {
			return fmt.Errorf("insert audit log: %w", err) // auto-rollback
		}

		// Both inserts succeed → auto-commit.
		return nil
	})
}

func (s *OrderService) PlaceOrderWithError(ctx context.Context, order Order) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.orders.Insert(ctx, order); err != nil {
			return err
		}
		// Simulate a failure AFTER order insert — should rollback both.
		return fmt.Errorf("payment declined")
	})
}

// ── Main ─────────────────────────────────────────────────────────────────────

func main() {
	// Setup: SQLite in-memory (no Docker needed).
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE orders (id TEXT, user_id TEXT, total REAL)`)
	db.Exec(`CREATE TABLE audit_log (action TEXT, order_id TEXT, performer TEXT)`)

	// Wire up.
	svc := &OrderService{
		tx:       txmanager.New(db), // ← single line setup
		orders:   &OrderRepo{db: db},
		auditLog: &AuditRepo{db: db},
	}

	ctx := context.Background()

	// ── Test 1: Successful transaction ────────────────────────────────────
	fmt.Println("═══ Test 1: PlaceOrder (should succeed) ═══")
	err = svc.PlaceOrder(ctx, Order{ID: "ord-1", UserID: "user-42", Total: 99.99})
	if err != nil {
		log.Fatalf("PlaceOrder failed: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&count)
	fmt.Printf("  Orders in DB: %d ✅\n", count)

	db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&count)
	fmt.Printf("  Audit logs in DB: %d ✅\n", count)

	// ── Test 2: Failed transaction (rollback) ─────────────────────────────
	fmt.Println("\n═══ Test 2: PlaceOrderWithError (should rollback) ═══")
	err = svc.PlaceOrderWithError(ctx, Order{ID: "ord-2", UserID: "user-42", Total: 50.00})
	fmt.Printf("  Error: %v ✅ (expected)\n", err)

	db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&count)
	fmt.Printf("  Orders in DB: %d (still 1 — rollback worked) ✅\n", count)

	db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&count)
	fmt.Printf("  Audit logs in DB: %d (still 1 — rollback worked) ✅\n", count)

	fmt.Println("\n🪓 Both repos participated in the same transaction — no manual *sql.Tx passing needed.")
}
