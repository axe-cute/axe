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

*Last updated: 2026-04-16*
