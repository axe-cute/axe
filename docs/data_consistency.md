# 🔄 Data Consistency — Transaction & Outbox
> Hướng dẫn đảm bảo dữ liệu nhất quán trong mọi tình huống.

---

## 1. Transaction Manager

### Interface (định nghĩa trong pkg/txmanager/)
```go
type TxManager interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
```

### Implementation
```go
type pgxTxManager struct {
    db *sql.DB
}

func (tm *pgxTxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
    tx, err := tm.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }

    ctx = injectTx(ctx, tx)  // inject vào context

    if err := fn(ctx); err != nil {
        _ = tx.Rollback()
        return err
    }

    return tx.Commit()
}
```

### Repository extracts tx from context
```go
func (r *postgresOrderRepo) Create(ctx context.Context, order domain.Order) error {
    db := extractTx(ctx, r.db)  // lấy tx nếu có, fallback về db pool
    // ...
}
```

---

## 2. Outbox Pattern

### Schema
```sql
CREATE TABLE outbox_events (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate    TEXT        NOT NULL,  -- "order", "user"
    event_type   TEXT        NOT NULL,  -- "OrderPlaced", "UserRegistered"
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    retries      INT         NOT NULL DEFAULT 0
);

CREATE INDEX idx_outbox_unprocessed ON outbox_events(created_at)
    WHERE processed_at IS NULL;
```

### Write (trong cùng transaction)
```go
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        order, err := s.orderRepo.Create(ctx, ...)
        if err != nil { return err }

        // Atomic: vào cùng tx với order insert
        return s.outboxRepo.Append(ctx, OutboxEvent{
            Aggregate: "order",
            EventType: "OrderPlaced",
            Payload:   mustMarshal(OrderPlacedPayload{OrderID: order.ID}),
        })
    })
}
```

### Poller (background goroutine)
```go
func StartOutboxPoller(ctx context.Context, db *sql.DB, queue *asynq.Client) {
    ticker := time.NewTicker(1 * time.Second)
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            events, _ := fetchUnprocessed(ctx, db, 100)
            for _, e := range events {
                if err := queue.Enqueue(asynq.NewTask(e.EventType, e.Payload)); err != nil {
                    log.Error(ctx, "enqueue failed", "event_id", e.ID)
                    continue
                }
                markProcessed(ctx, db, e.ID)
            }
        }
    }
}
```

---

## 3. Failure Modes Matrix

| Scenario | Result | Strategy |
|---|---|---|
| DB write OK, outbox insert fail | Transaction rollback → nothing persisted | Safe, retry toàn bộ |
| Both write OK, poller down | Event sits in outbox | Poller restart picks up |
| Poller enqueue fail | Event stays unprocessed | Retry next tick |
| Worker fail after dequeue | Task stays in Asynq retry queue | Exponential backoff |
| Worker success, DB update fail | markProcessed fail → duplicate risk | Idempotency key |

---

## 4. Idempotency

Mỗi worker PHẢI xử lý idempotent:
```go
func HandleOrderPlaced(ctx context.Context, task *asynq.Task) error {
    var payload OrderPlacedPayload
    json.Unmarshal(task.Payload(), &payload)

    // Check đã xử lý chưa (event_id là idempotency key)
    if alreadyProcessed(ctx, payload.EventID) {
        return nil  // skip, không lỗi
    }

    // Process...
    return markEventProcessed(ctx, payload.EventID)
}
```
