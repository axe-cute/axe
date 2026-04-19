package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"nhooyr.io/websocket"
)

// Hub manages all WebSocket connections and rooms.
type Hub struct {
	clients    map[string]*Client
	rooms      map[string]*Room
	mu         sync.RWMutex
	register   chan *Client
	unregister chan *Client
	adapter    Adapter
	ctx        context.Context
	cancel     context.CancelFunc
	log        *slog.Logger
}

// HubOption configures a Hub.
type HubOption func(*Hub)

// WithAdapter sets the cross-instance adapter (e.g. Redis Pub/Sub).
func WithAdapter(a Adapter) HubOption   { return func(h *Hub) { h.adapter = a } }

// WithLogger sets the logger for the hub.
func WithLogger(l *slog.Logger) HubOption { return func(h *Hub) { h.log = l } }

// NewHub creates a new WebSocket hub.
func NewHub(opts ...HubOption) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		clients: make(map[string]*Client), rooms: make(map[string]*Room),
		register: make(chan *Client, 32), unregister: make(chan *Client, 32),
		adapter: MemoryAdapter{}, ctx: ctx, cancel: cancel, log: slog.Default(),
	}
	for _, o := range opts { o(h) }
	return h
}

// Run starts the hub event loop. It blocks until ctx is canceled.
func (h *Hub) Run(ctx context.Context) {
	go func() { <-ctx.Done(); h.cancel() }()
	for {
		select {
		case c := <-h.register:
			h.mu.Lock(); h.clients[c.id] = c; h.mu.Unlock()
			wsActiveConnections.Inc()
			h.log.Info("ws: client connected", "client_id", c.id)
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.id]; ok {
				for _, rid := range c.allRooms() {
					if room, ok := h.rooms[rid]; ok {
						room.remove(c.id)
						if room.isEmpty() { delete(h.rooms, rid); wsRoomsActive.Dec() }
					}
				}
				delete(h.clients, c.id); wsActiveConnections.Dec()
				h.log.Info("ws: client disconnected", "client_id", c.id)
			}
			h.mu.Unlock()
		case <-h.ctx.Done():
			h.mu.Lock()
			for _, c := range h.clients { c.Close() }
			h.clients = make(map[string]*Client); h.rooms = make(map[string]*Room)
			h.mu.Unlock()
			_ = h.adapter.Close()
			return
		}
	}
}

// Upgrade upgrades an HTTP connection to a WebSocket connection.
func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*Client, error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil { wsConnectRejectedTotal.Inc(); return nil, fmt.Errorf("ws: upgrade: %w", err) }
	c := newClient(conn, h); h.register <- c
	ctx, cancel := context.WithCancel(h.ctx)
	go func() { defer cancel(); c.writePump(ctx) }()
	go c.readPump(ctx)
	return c, nil
}

// UpgradeAuthenticated upgrades with user identity from context.
func (h *Hub) UpgradeAuthenticated(w http.ResponseWriter, r *http.Request, tracker *UserConnTracker) (*Client, error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil { wsConnectRejectedTotal.Inc(); return nil, fmt.Errorf("ws: upgrade: %w", err) }
	userID := ""
	if claims := ClaimsFromCtx(r.Context()); claims != nil { userID = claims.UserID }
	c := newClientWithMeta(conn, h, userID); h.register <- c
	ctx, cancel := context.WithCancel(h.ctx)
	go func() { defer cancel(); c.writePump(ctx) }()
	go func() {
		c.readPump(ctx)
		if tracker != nil && userID != "" { tracker.Release(userID) }
	}()
	return c, nil
}

// Join adds a client to a room.
func (h *Hub) Join(client *Client, roomID string) {
	h.mu.Lock()
	room, exists := h.rooms[roomID]
	if !exists {
		room = newRoom(roomID); h.rooms[roomID] = room; wsRoomsActive.Inc()
		_ = h.adapter.Subscribe(h.ctx, roomID, func(msg []byte) {
			h.mu.RLock(); r, ok := h.rooms[roomID]; h.mu.RUnlock()
			if ok { r.broadcast(msg) }
		})
	}
	room.add(client); h.mu.Unlock()
	client.joinRoom(roomID)
}

// Leave removes a client from a room.
func (h *Hub) Leave(client *Client, roomID string) {
	h.mu.Lock()
	if room, ok := h.rooms[roomID]; ok {
		room.remove(client.id)
		if room.isEmpty() { delete(h.rooms, roomID); wsRoomsActive.Dec() }
	}
	h.mu.Unlock(); client.leaveRoom(roomID)
}

// Broadcast sends a message to all clients in a room and publishes via the adapter.
func (h *Hub) Broadcast(ctx context.Context, roomID string, msg []byte) error {
	h.mu.RLock(); room, ok := h.rooms[roomID]; h.mu.RUnlock()
	if ok { room.broadcast(msg) }
	return h.adapter.Publish(ctx, roomID, msg)
}

// Presence returns the list of client IDs in a room.
func (h *Hub) Presence(roomID string) []string {
	h.mu.RLock(); room, ok := h.rooms[roomID]; h.mu.RUnlock()
	if !ok { return nil }
	return room.Presence()
}

// ClientCount returns the total number of connected clients.
func (h *Hub) ClientCount() int { h.mu.RLock(); defer h.mu.RUnlock(); return len(h.clients) }

// Shutdown cancels the hub context and closes all connections.
func (h *Hub) Shutdown()        { h.cancel() }

// Context returns the hub's lifecycle context.
func (h *Hub) Context() context.Context { return h.ctx }

// RoomSize returns the number of clients in a room.
func (h *Hub) RoomSize(roomID string) int {
	h.mu.RLock(); room, ok := h.rooms[roomID]; h.mu.RUnlock()
	if !ok { return 0 }
	return room.Size()
}
