# WebSocket Semantics

> This document describes the delivery guarantees, ordering behavior, and operational
> characteristics of the axe WebSocket hub (`pkg/ws`).

---

## Message Ordering Guarantees

| Adapter | Ordering | Notes |
|---|---|---|
| **MemoryAdapter** (single instance) | ✅ FIFO per room | Go channels guarantee FIFO. All messages within a room arrive in send order. |
| **RedisAdapter** (multi-instance) | ⚠️ Best-effort FIFO | Redis Pub/Sub is near-FIFO under normal conditions. **No hard guarantee** during network partitions, reconnects, or Redis failover. |

### What this means in practice

- **Chat applications**: Message ordering is sufficient for both adapters. Occasional reordering under Redis failover is acceptable for chat UX.
- **Collaborative editing**: If strict ordering is required, add a sequence number to your messages and sort client-side.

---

## Delivery Semantics

The axe WebSocket hub uses **at-most-once delivery**.

| Property | Value |
|---|---|
| **Delivery guarantee** | At-most-once |
| **Message persistence** | None — messages are not stored |
| **Retry on failure** | No |
| **Duplicate delivery** | Not possible |

### When messages are lost

1. **Client send buffer full** — Each client has a 256-message send buffer. If a client is slow and the buffer fills up, new messages are **silently dropped** (with a `WARN` log).
2. **Client disconnects** — Messages sent during the disconnect window are lost.
3. **Redis Pub/Sub reconnect** — During Redis adapter reconnection, fan-out messages may be lost.

### Recommendation for critical data

If you use WebSocket for notifications, order status updates, or any data where loss is unacceptable:

```
Strategy: Use WebSocket for real-time push + HTTP polling as fallback.

Client                          Server
  │                               │
  │◄─── WS: order_updated ────────│  (real-time, may be lost)
  │                               │
  │──── GET /api/v1/orders ──────►│  (polling fallback, guaranteed)
  │◄─── 200 OK ──────────────────│
```

**Do NOT rely solely on WebSocket for critical business data.**

---

## Connection Lifecycle

```
Client connects
  → WSAuth middleware validates JWT (header or ?token= query param)
  → Hub.UpgradeAuthenticated() performs WebSocket upgrade
  → Per-user connection limit check (default: max 5)
  → Client readPump + writePump goroutines start
  → Hub.Join(client, "room-name")
  → client.OnMessage(handler)
  → ... messages flow ...
  → client disconnects → readPump exits → tracker.Release()
```

### Timeouts & Buffers

| Parameter | Value | Configurable? |
|---|---|---|
| Send buffer size | 256 messages | Compile-time constant (`sendBufSize`) |
| Write timeout | 10 seconds | Compile-time constant (`writeTimeout`) |
| Ping interval | 30 seconds | Compile-time constant (`pingInterval`) |
| Max connections per user | 5 | Via `UserConnTracker` constructor |

---

## Scaling

### Single Instance (default)

Uses `MemoryAdapter` — zero overhead, zero configuration.

```
[Client A] ──► [Hub (memory)] ──► [Client B]
                    │
                    └──► [Client C]
```

### Multi-Instance (horizontal scaling)

Uses `RedisAdapter` — messages are fan-out via Redis Pub/Sub.

```
[Client A] ──► [Hub 1] ──publish──► [Redis Pub/Sub]
                                          │
                    ┌─────subscribe────────┘
                    ▼
               [Hub 2] ──► [Client B]
               [Hub 3] ──► [Client C]
```

**Setup:**
```go
import "github.com/axe-cute/axe/pkg/ws"

hub := ws.NewHub(
    ws.WithAdapter(ws.NewRedisAdapter(redisClient)),
    ws.WithLogger(logger),
)
```

---

## Metrics

All WebSocket metrics use the `axe_ws_` namespace:

| Metric | Type | Description |
|---|---|---|
| `axe_ws_active_connections` | Gauge | Current active WebSocket connections |
| `axe_ws_messages_total{direction}` | Counter | Messages sent/received (`inbound`/`outbound`) |
| `axe_ws_rooms_active` | Gauge | Number of active rooms |
| `axe_ws_connect_rejected_total` | Counter | Rejected upgrade attempts (auth failure, conn limit) |

---

*Last updated: 2026-04-20*
