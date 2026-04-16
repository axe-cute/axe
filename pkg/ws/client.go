package ws

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	// sendBufSize is the capacity of the outbound send channel per client.
	sendBufSize = 256

	// writeTimeout is the maximum duration allowed for a single write.
	writeTimeout = 10 * time.Second

	// pingInterval controls how often the server pings clients.
	pingInterval = 30 * time.Second
)

// MessageHandler is called by the client when a message is received from the browser.
type MessageHandler func(msg []byte)

// Client wraps an nhooyr.io/websocket connection. Each Client has a dedicated
// readPump and writePump goroutine managed by the Hub.
type Client struct {
	// id is the unique per-connection identifier (UUID).
	id string

	// UserID is the authenticated user ID, set by WSAuth middleware.
	// Empty string means the connection is unauthenticated (e.g. public lobby).
	UserID string

	conn *websocket.Conn
	hub  *Hub

	// send is the outbound write buffer. Messages dropped only when full.
	sendCh chan []byte

	// done is closed by readPump when the connection ends.
	// HTTP handlers must block on <-client.Done() to keep the WebSocket alive:
	// nhooyr.io/websocket closes the connection when the handler goroutine returns.
	done chan struct{}

	// onMessage is called for every message received from the peer.
	onMessage MessageHandler

	// rooms tracks which rooms this client has joined (roomID set).
	rooms map[string]struct{}
	mu    sync.RWMutex

	log *slog.Logger
}

func newClient(conn *websocket.Conn, hub *Hub) *Client {
	return newClientWithMeta(conn, hub, "")
}

// newClientWithMeta constructs a Client with an optional authenticated userID.
// Pass an empty string for unauthenticated connections.
func newClientWithMeta(conn *websocket.Conn, hub *Hub, userID string) *Client {
	return &Client{
		id:     uuid.New().String(),
		UserID: userID,
		conn:   conn,
		hub:    hub,
		sendCh: make(chan []byte, sendBufSize),
		done:   make(chan struct{}),
		rooms:  make(map[string]struct{}),
		log:    hub.log,
	}
}

// ID returns the unique identifier for this client.
func (c *Client) ID() string { return c.id }

// OnMessage registers a handler called for every message received from the peer.
// Only the last registered handler is active.
func (c *Client) OnMessage(fn MessageHandler) {
	c.mu.Lock()
	c.onMessage = fn
	c.mu.Unlock()
}

// send delivers msg to the client's send buffer (non-blocking).
// If the buffer is full the message is silently dropped to protect the hub.
func (c *Client) send(msg []byte) {
	select {
	case c.sendCh <- msg:
		wsMessagesTotal.WithLabelValues("outbound").Inc()
	default:
		c.log.Warn("ws: client send buffer full, dropping message", "client_id", c.id)
	}
}

// Close signals the client to disconnect gracefully.
func (c *Client) Close() {
	close(c.sendCh)
}

// Done returns a channel closed when the client disconnects.
// HTTP handlers must block on <-client.Done() to keep the WebSocket alive.
// nhooyr.io/websocket closes the connection when the handler goroutine returns.
func (c *Client) Done() <-chan struct{} { return c.done }

// readPump reads messages from the peer and dispatches them to onMessage.
// It runs in its own goroutine and terminates when the connection closes.
func (c *Client) readPump(ctx context.Context) {
	defer func() {
		close(c.done)          // signal handler goroutine to return
		c.hub.unregister <- c  // remove from hub
	}()

	for {
		_, msg, err := c.conn.Read(ctx)
		if err != nil {
			// Normal closes (context cancel, peer close) are not errors.
			if ctx.Err() == nil {
				c.log.Debug("ws: read error", "client_id", c.id, "error", err)
			}
			return
		}
		wsMessagesTotal.WithLabelValues("inbound").Inc()

		c.mu.RLock()
		handler := c.onMessage
		c.mu.RUnlock()

		if handler != nil {
			handler(msg)
		}
	}
}

// writePump writes messages from the send channel to the peer.
// It also sends periodic pings to keep the connection alive.
func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.sendCh:
			if !ok {
				// Channel closed — graceful disconnect.
				_ = c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				c.log.Debug("ws: write error", "client_id", c.id, "error", err)
				return
			}

		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				c.log.Debug("ws: ping error", "client_id", c.id, "error", err)
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// joinRoom records that this client is a member of roomID.
func (c *Client) joinRoom(roomID string) {
	c.mu.Lock()
	c.rooms[roomID] = struct{}{}
	c.mu.Unlock()
}

// leaveRoom removes roomID from the client's membership set.
func (c *Client) leaveRoom(roomID string) {
	c.mu.Lock()
	delete(c.rooms, roomID)
	c.mu.Unlock()
}

// allRooms returns a snapshot of the rooms this client belongs to.
func (c *Client) allRooms() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rooms := make([]string, 0, len(c.rooms))
	for id := range c.rooms {
		rooms = append(rooms, id)
	}
	return rooms
}

// String implements fmt.Stringer.
func (c *Client) String() string {
	return fmt.Sprintf("Client(%s)", c.id)
}
