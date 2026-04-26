# 🔄 Data Consistency — Transactions & Outbox

> How axe guarantees data consistency across database writes and
> asynchronous side effects (emails, notifications, external APIs).

---

## 1. Transaction Manager

### Interface

Defined in `pkg/txmanager/`:

```go
type TxManager interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
```

### How it works

The transaction manager starts a database transaction, injects it into the
context, calls your function, and either commits (on success) or rolls back
(on error):

```go
type pgxTxManager struct {
    db *sql.DB
}

func (tm *pgxTxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
    tx, err := tm.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }

    ctx = injectTx(ctx, tx)  // store tx in context

    if err := fn(ctx); err != nil {
        _ = tx.Rollback()
        return err
    }

    return tx.Commit()
}
```

### Repository extracts the tx from context

Repositories never start transactions themselves. They check whether a
transaction exists in the context and use it; otherwise they fall back to
the shared connection pool:

```go
func (r *postgresOrderRepo) Create(ctx context.Context, order domain.Order) error {
    db := extractTxOrDB(ctx, r.db)  // tx if present, otherwise falls back to the pool
    // execute query using db...
}
```

---

## 2. Outbox Pattern

### Problem

When a service needs to both write to the database **and** trigger an
asynchronous side effect (send email, publish event), doing them
separately risks inconsistency:

- DB write succeeds but event publish fails → data saved, no side effect
- Event publish succeeds but DB write fails → side effect fired, no data

### Solution: write the event to the same database, in the same transaction

```
┌─────────────────────────────┐
│      Same Transaction       │
│  1. INSERT order             │
│  2. INSERT outbox_event      │
│  └── both commit or rollback │
└──────────────┬──────────────┘
               │
┌──────────────▼──────────────┐
│    Background Poller (5s)   │
│  Reads unprocessed events   │
│  Publishes to Asynq queue   │
│  Marks event as processed   │
└──────────────┬──────────────┘
               │
┌──────────────▼──────────────┐
│      Asynq Worker           │
│  Processes the task         │
│  (send email, call API...)  │
└─────────────────────────────┘
```

### Schema

```sql
CREATE TABLE outbox_events (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate    TEXT        NOT NULL,  -- e.g. "order", "user"
    event_type   TEXT        NOT NULL,  -- e.g. "OrderPlaced", "UserRegistered"
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    retries      INT         NOT NULL DEFAULT 0
);

CREATE INDEX idx_outbox_unprocessed ON outbox_events(created_at)
    WHERE processed_at IS NULL;
```

### Service usage (event appended within the same transaction)

```go
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        order, err := s.orderRepo.Create(ctx, ...)
        if err != nil { return err }

        // Atomic: this INSERT is in the same tx as the order INSERT above.
        // If either fails, both are rolled back.
        return s.outboxRepo.Append(ctx, OutboxEvent{
            Aggregate: "order",
            EventType: "OrderPlaced",
            Payload:   mustMarshal(OrderPlacedPayload{OrderID: order.ID}),
        })
    })
}
```

### Background poller

The poller runs as a background goroutine. Every 5 seconds it fetches
unprocessed events and publishes them to the Asynq task queue:

```go
func (p *Poller) Start(ctx context.Context) {
    ticker := time.NewTicker(p.cfg.Interval)
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            events, _ := p.fetchUnprocessed(ctx, p.cfg.BatchSize)
            for _, e := range events {
                if err := p.queue.Enqueue(asynq.NewTask(e.EventType, e.Payload)); err != nil {
                    p.log.Error("enqueue failed", "event_id", e.ID, "error", err)
                    continue
                }
                p.markProcessed(ctx, e.ID)
            }
        }
    }
}
```

---

## 3. Failure Modes Matrix

| Scenario | Result | Recovery strategy |
|---|---|---|
| DB write OK, outbox insert fails | Transaction rolls back — nothing is persisted | Safe. Client retries the entire request. |
| Both writes OK, poller is down | Event sits in the outbox table | Poller restart picks it up automatically. |
| Poller enqueue to Asynq fails | Event stays unprocessed (retries column tracks attempts) | Poller retries on the next tick. |
| Asynq worker fails after dequeue | Task stays in the Asynq retry queue | Asynq handles exponential backoff. |
| Worker succeeds, but `markProcessed` fails | Event may be re-delivered (duplicate risk) | Workers must be idempotent (see below). |

---

## 4. Idempotency

Because at-least-once delivery is inherent in the outbox pattern,
**every worker must handle duplicate events safely**:

```go
func HandleOrderPlaced(ctx context.Context, task *asynq.Task) error {
    var payload OrderPlacedPayload
    json.Unmarshal(task.Payload(), &payload)

    // Check if this event was already processed (event_id is the idempotency key)
    if alreadyProcessed(ctx, payload.EventID) {
        return nil  // skip silently — not an error
    }

    // Process the event...
    sendConfirmationEmail(ctx, payload.OrderID)

    return markEventProcessed(ctx, payload.EventID)
}
```

**Rule:** If you can't make a side effect idempotent (e.g., charging a
credit card), use a distributed lock or a deduplication table.

---

## 5. Escape Hatches — When You Need Raw Control

The transaction manager covers 90% of use cases. For the remaining 10%,
you can drop down to the raw `database/sql` driver without leaving axe.

### Savepoints (nested transactions)

`WithTx` wraps the entire closure in a single transaction. If you need
a partial rollback (e.g., "try to reserve inventory, but continue even if
it fails"), use a savepoint inside the closure:

```go
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return s.tx.WithTx(ctx, func(tx *sql.Tx) error {
        // Primary write — must succeed.
        order, err := s.orderRepo.CreateWithTx(ctx, tx, input)
        if err != nil {
            return err // entire tx rolls back
        }

        // Optional write — try but don't fail the order if inventory
        // reservation fails (e.g., warehouse API is down).
        _, spErr := tx.ExecContext(ctx, "SAVEPOINT reserve_inventory")
        if spErr != nil {
            return spErr
        }
        if err := s.inventoryRepo.ReserveWithTx(ctx, tx, order.Items); err != nil {
            // Roll back only the reservation, not the order.
            tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT reserve_inventory")
            logger.FromCtx(ctx).Warn("inventory reservation failed, order continues",
                "order_id", order.ID, "error", err)
        } else {
            tx.ExecContext(ctx, "RELEASE SAVEPOINT reserve_inventory")
        }

        return s.outboxRepo.AppendWithTx(ctx, tx, OrderPlacedEvent{OrderID: order.ID})
    })
}
```

### Raw `*sql.DB` access

Every axe project has a `*sql.DB` in `cmd/api/main.go`. You can pass it
directly to any code that needs raw driver access:

```go
// In main.go — sqlDB is already open:
sqlDB, err := sql.Open("pgx", cfg.DatabaseURL)

// Pass to anything that needs raw access:
myCustomRepo := NewCustomRepo(sqlDB)
```

### Raw pgx features (PostgreSQL only)

If you need pgx-specific features (COPY, LISTEN/NOTIFY, large objects),
unwrap the `*sql.DB` to get the underlying pgx connection:

```go
import "github.com/jackc/pgx/v5/stdlib"

conn, err := stdlib.AcquireConn(sqlDB)
if err != nil { return err }
defer stdlib.ReleaseConn(sqlDB, conn)

// Now you have *pgx.Conn — full pgx API available.
_, err = conn.CopyFrom(ctx, pgx.Identifier{"posts"}, columns, source)
```

### When to escape vs. when to stay

| Situation                                  | Recommendation                |
|--------------------------------------------|-------------------------------|
| Simple CRUD, single write                  | No transaction needed         |
| Multi-write (order + outbox)               | `WithTx` — standard pattern   |
| Partial rollback needed                    | Savepoint inside `WithTx`     |
| Bulk insert (COPY)                         | Raw pgx via `stdlib.AcquireConn` |
| LISTEN/NOTIFY                              | Raw pgx connection            |
| Complex JOIN query                         | sqlc (alternative to Ent)     |

**Rule of thumb:** If `WithTx` + Ent can express it, use them. Escape
only when the abstraction gets in the way — and document *why* in a
comment so the next developer doesn't "fix" it back.

---

*Last updated: 2026-04-26*
