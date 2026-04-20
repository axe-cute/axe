// Package ws provides a production-ready WebSocket hub for the axe framework.
//
// Quick start:
//
//	hub := ws.NewHub()
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	go hub.Run(ctx)
//
//	// In HTTP handler:
//	func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
//	    client, err := hub.Upgrade(w, r)
//	    if err != nil { return }
//	    hub.Join(client, "general")
//	    client.OnMessage(func(msg []byte) {
//	        hub.Broadcast(context.Background(), "general", msg)
//	    })
//	}
package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"nhooyr.io/websocket"
)

// Hub is the central WebSocket connection registry.
// It manages clients, rooms, and message routing across instances (via Adapter).
// All exported methods are safe for concurrent use.
type Hub struct {
	// internal state (mu-protected)
	clients map[string]*Client
	rooms   map[string]*Room
	mu      sync.RWMutex

	// channels for goroutine-safe state mutations
	register   chan *Client
	unregister chan *Client

	// adapter handles cross-instance pub/sub (MemoryAdapter or RedisAdapter).
	adapter Adapter

	// allowedOrigins controls which origins may connect via WebSocket.
	// Empty = reject all cross-origin (safe default).
	// []string{"*"} = allow all origins (development only).
	// []string{"https://app.example.com"} = explicit whitelist.
	allowedOrigins []string

	// ctx is the hub's root context; cancel() triggers graceful shutdown.
	ctx    context.Context
	cancel context.CancelFunc

	log *slog.Logger
}

// HubOption is a functional option for NewHub.
type HubOption func(*Hub)

// WithAdapter sets the pub/sub backend. Default: MemoryAdapter (single-instance).
func WithAdapter(a Adapter) HubOption {
	return func(h *Hub) { h.adapter = a }
}

// WithLogger sets the logger used by the hub and its clients.
func WithLogger(l *slog.Logger) HubOption {
	return func(h *Hub) { h.log = l }
}

// WithAllowedOrigins sets the origins permitted to connect via WebSocket.
// Default: empty (reject all cross-origin requests — safe default).
// Use []string{"*"} to allow all origins (development/testing only).
// Production should list explicit origins: []string{"https://app.example.com"}.
func WithAllowedOrigins(origins []string) HubOption {
	return func(h *Hub) { h.allowedOrigins = origins }
}

// NewHub constructs a Hub with optional configuration.
func NewHub(opts ...HubOption) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		clients:    make(map[string]*Client),
		rooms:      make(map[string]*Room),
		register:   make(chan *Client, 32),
		unregister: make(chan *Client, 32),
		adapter:    MemoryAdapter{},
		ctx:        ctx,
		cancel:     cancel,
		log:        slog.Default(),
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Run starts the Hub event loop. It blocks until ctx is cancelled.
// Call this in a dedicated goroutine: go hub.Run(ctx).
func (h *Hub) Run(ctx context.Context) {
	// Merge external context with hub's internal cancel.
	// The goroutine exits when either context is done, preventing leaks.
	go func() {
		select {
		case <-ctx.Done():
			h.cancel()
		case <-h.ctx.Done():
			// Hub was shut down directly — exit cleanly.
		}
	}()

	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c.id] = c
			h.mu.Unlock()
			wsActiveConnections.Inc()
			h.log.Info("ws: client connected", "client_id", c.id)

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.id]; ok {
				// Remove client from all rooms it belongs to.
				for _, roomID := range c.allRooms() {
					if room, exists := h.rooms[roomID]; exists {
						room.remove(c.id)
						if room.isEmpty() {
							delete(h.rooms, roomID)
							wsRoomsActive.Dec()
							h.log.Debug("ws: room destroyed", "room", roomID)
						}
					}
				}
				delete(h.clients, c.id)
				wsActiveConnections.Dec()
				h.log.Info("ws: client disconnected", "client_id", c.id)
			}
			h.mu.Unlock()

		case <-h.ctx.Done():
			h.log.Info("ws: hub shutting down")
			h.mu.Lock()
			for _, c := range h.clients {
				c.Close()
			}
			h.clients = make(map[string]*Client)
			h.rooms = make(map[string]*Room)
			h.mu.Unlock()
			if err := h.adapter.Close(); err != nil {
				h.log.Warn("ws: adapter close error", "error", err)
			}
			return
		}
	}
}

// Upgrade performs the HTTP → WebSocket handshake, registers the resulting
// client with the hub, and starts its read/write pumps.
// Returns the new Client so callers can Join rooms and register OnMessage.
func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*Client, error) {
	conn, err := websocket.Accept(w, r, h.acceptOptions())
	if err != nil {
		wsConnectRejectedTotal.Inc()
		h.log.Warn("ws: upgrade failed", "error", err, "remote", r.RemoteAddr)
		return nil, fmt.Errorf("ws: upgrade: %w", err)
	}

	c := newClient(conn, h)
	h.register <- c

	// Each client gets its own context derived from the hub's context so that
	// a hub shutdown cancels all pumps automatically.
	clientCtx, clientCancel := context.WithCancel(h.ctx)
	go func() {
		defer clientCancel()
		c.writePump(clientCtx)
	}()
	go c.readPump(clientCtx)

	return c, nil
}

// UpgradeAuthenticated performs the WebSocket handshake for authenticated
// requests. It reads the userID from the context (injected by WSAuth middleware)
// and creates a Client with UserID set. When the client disconnects,
// tracker.Release(userID) is called automatically to free the connection slot.
//
// Pass a nil tracker to skip connection-count accounting (e.g. in tests).
func (h *Hub) UpgradeAuthenticated(w http.ResponseWriter, r *http.Request, tracker *UserConnTracker) (*Client, error) {
	conn, err := websocket.Accept(w, r, h.acceptOptions())
	if err != nil {
		wsConnectRejectedTotal.Inc()
		h.log.Warn("ws: upgrade failed", "error", err, "remote", r.RemoteAddr)
		return nil, fmt.Errorf("ws: upgrade: %w", err)
	}

	// Extract authenticated userID from context (set by WSAuth).
	userID := ""
	if claims := ClaimsFromCtx(r.Context()); claims != nil {
		userID = claims.UserID
	}

	c := newClientWithMeta(conn, h, userID)
	h.register <- c

	clientCtx, clientCancel := context.WithCancel(h.ctx)
	go func() {
		defer clientCancel()
		c.writePump(clientCtx)
	}()
	go func() {
		c.readPump(clientCtx)
		// Release the connection slot when the pump exits.
		if tracker != nil && userID != "" {
			tracker.Release(userID)
			h.log.Debug("ws: conn slot released", "user_id", userID)
		}
	}()

	return c, nil
}

// Join adds client to the named room, creating the room if necessary.
// It also subscribes the hub to the room's Redis channel (no-op for MemoryAdapter).
func (h *Hub) Join(client *Client, roomID string) {
	h.mu.Lock()
	room, exists := h.rooms[roomID]
	if !exists {
		room = newRoom(roomID)
		h.rooms[roomID] = room
		wsRoomsActive.Inc()
		h.log.Debug("ws: room created", "room", roomID)

		// Subscribe this hub instance to the room channel so that broadcasts
		// from other instances (via Redis) are forwarded to local clients.
		_ = h.adapter.Subscribe(h.ctx, roomID, func(msg []byte) {
			h.mu.RLock()
			r, ok := h.rooms[roomID]
			h.mu.RUnlock()
			if ok {
				r.broadcast(msg)
			}
		})
	}
	room.add(client)
	h.mu.Unlock()

	client.joinRoom(roomID)
	h.log.Debug("ws: client joined room", "client_id", client.id, "room", roomID)
}

// Leave removes client from the named room.
func (h *Hub) Leave(client *Client, roomID string) {
	h.mu.Lock()
	if room, ok := h.rooms[roomID]; ok {
		room.remove(client.id)
		if room.isEmpty() {
			delete(h.rooms, roomID)
			wsRoomsActive.Dec()
			h.log.Debug("ws: room destroyed", "room", roomID)
		}
	}
	h.mu.Unlock()

	client.leaveRoom(roomID)
	h.log.Debug("ws: client left room", "client_id", client.id, "room", roomID)
}

// Broadcast sends msg to all clients in roomID on this instance and publishes
// it to the adapter so that other instances also deliver it.
//
// For MemoryAdapter (single-instance): delivers directly to local clients.
// For RedisAdapter (multi-instance): publishes to Redis only — the subscription
// handler in Join() delivers to local clients, avoiding double delivery.
func (h *Hub) Broadcast(ctx context.Context, roomID string, msg []byte) error {
	// For MemoryAdapter, Publish is a no-op so we must deliver locally.
	// For RedisAdapter, the subscription handler already delivers to local
	// clients — delivering here would cause double delivery.
	if _, isMemory := h.adapter.(MemoryAdapter); isMemory {
		h.mu.RLock()
		room, ok := h.rooms[roomID]
		h.mu.RUnlock()
		if ok {
			room.broadcast(msg)
		}
		return nil
	}

	// Publish to adapter — subscription handler delivers to all instances
	// including this one.
	if err := h.adapter.Publish(ctx, roomID, msg); err != nil {
		return fmt.Errorf("ws: broadcast to %q: %w", roomID, err)
	}
	return nil
}

// Presence returns the IDs of all clients in roomID.
func (h *Hub) Presence(roomID string) []string {
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()
	if !ok {
		return nil
	}
	return room.Presence()
}

// ClientCount returns the total number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Shutdown stops the hub gracefully (same as cancelling the context passed to Run).
func (h *Hub) Shutdown() {
	h.cancel()
}

// Context returns the hub's lifecycle context.
// Use this in OnMessage callbacks instead of r.Context() — the HTTP request
// context is canceled when the handler returns, which silently drops broadcasts.
func (h *Hub) Context() context.Context { return h.ctx }

// RoomSize returns the number of clients currently in roomID.
func (h *Hub) RoomSize(roomID string) int {
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()
	if !ok {
		return 0
	}
	return room.Size()
}

// acceptOptions builds websocket.AcceptOptions from the hub's allowed origins.
//
//   - Empty allowedOrigins → reject all cross-origin (safe default).
//   - {"*"} → InsecureSkipVerify=true (dev/testing only).
//   - Explicit list → OriginPatterns whitelist.
func (h *Hub) acceptOptions() *websocket.AcceptOptions {
	if len(h.allowedOrigins) == 0 {
		// Safe default: reject cross-origin WebSocket connections.
		return &websocket.AcceptOptions{InsecureSkipVerify: false}
	}
	for _, o := range h.allowedOrigins {
		if o == "*" {
			return &websocket.AcceptOptions{InsecureSkipVerify: true}
		}
	}
	return &websocket.AcceptOptions{
		OriginPatterns: h.allowedOrigins,
	}
}
