# Epic 7 — WebSocket Hub + Real-time Support

**Goal**: axe cung cấp built-in WebSocket hub với room management, broadcast, và presence — không cần library bên ngoài.

**Business Value**: Real-time features (chat, notifications, live dashboard) là requirement phổ biến. Hiện tại axe không có, user phải tự implement từ đầu.

**Status**: ✅ Done (Sprint 17–18)

**Priority**: P1

---

## Stories

### Story 7.1 — WebSocket Hub Core
**Sprint**: 17 | **Priority**: P1

**Goal**: `pkg/ws` package với Hub, Client, Room abstractions.

**Acceptance Criteria**:
- [x] `pkg/ws/hub.go` — Hub quản lý connections, broadcast, rooms
- [x] `pkg/ws/client.go` — Client wraps `nhooyr.io/websocket` connection
- [x] `pkg/ws/room.go` — Room = named group của clients
- [x] Hub.Broadcast(roomID, message) → tất cả clients trong room nhận message
- [x] Hub.Join(clientID, roomID) / Hub.Leave(clientID, roomID)
- [x] Hub.Presence(roomID) → list online users trong room
- [x] Graceful shutdown: drain pending messages trước khi close
- [x] Metrics: `ws_active_connections`, `ws_messages_total`

**API**:
```go
hub := ws.NewHub()
go hub.Run()

// In handler:
func (h *ChatHandler) Connect(w http.ResponseWriter, r *http.Request) {
    client, err := hub.Upgrade(w, r)
    userID := middleware.ClaimsFromCtx(r.Context()).UserID
    hub.Join(client, "general")
    client.OnMessage(func(msg []byte) {
        hub.Broadcast("general", msg)
    })
}
```

### Story 7.2 — Redis Pub/Sub for Multi-Instance
**Sprint**: 17 | **Priority**: P1

**Goal**: Scale WebSocket across multiple instances bằng Redis Pub/Sub.

**Acceptance Criteria**:
- [x] `pkg/ws/redis_adapter.go` — Redis pub/sub backend cho Hub
- [x] Khi instance A broadcast → instance B cũng nhận và forward tới clients
- [x] `HUB_ADAPTER=redis` | `memory` (default) config
- [x] Integration test: 2 hubs connected qua Redis → cross-broadcast works
- [x] Graceful unsubscribe khi shutdown

### Story 7.3 — WebSocket Middleware
**Sprint**: 18 | **Priority**: P1

**Goal**: Authentication + Rate limiting cho WebSocket connections.

**Acceptance Criteria**:
- [x] WS connections yêu cầu JWT token (query param hoặc header)
- [x] Rate limit: max 5 connections per user
- [x] `ws_connect_rejected_total` Prometheus counter
- [x] Chi middleware: `r.Get("/ws", hub.Middleware(jwtSvc), chatHandler.Connect)`

### Story 7.4 — `axe generate resource --with-ws` Flag
**Sprint**: 18 | **Priority**: P2

**Goal**: Generator tự động tạo WebSocket handler cùng REST handler.

**Acceptance Criteria**:
- [x] `axe generate resource Chat --with-ws` → thêm `ChatWSHandler`
- [x] Generated handler có `Connect`, `Disconnect`, `OnMessage` stubs
- [x] Route registration comment bao gồm WebSocket endpoint

---

## Technical Design

```
pkg/ws/
  hub.go           ← Hub (connection registry + broadcast)
  client.go        ← Client (nhooyr.io/websocket wrapper)
  room.go          ← Room (named group)
  redis_adapter.go ← Redis pub/sub scale-out
  middleware.go    ← JWT auth for WS upgrade
  metrics.go       ← Prometheus counters/gauges
```

**Dependencies**: `nhooyr.io/websocket` (ADR-008: actively maintained, lighter than gorilla)

---

## Risks
- ~~gorilla/websocket không còn actively maintained~~ → Resolved: switched to `nhooyr.io/websocket`
- Redis pub/sub thêm latency → document expected latency SLA
