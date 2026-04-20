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
	sendBufSize  = 256
	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
)

// MessageHandler is a callback for incoming WebSocket messages.
type MessageHandler func(msg []byte)

// Client wraps a single WebSocket connection.
type Client struct {
	id        string
	UserID    string
	conn      *websocket.Conn
	hub       *Hub
	sendCh    chan []byte
	done      chan struct{} // closed when readPump exits
	onMessage MessageHandler
	rooms     map[string]struct{}
	mu        sync.RWMutex
	log       *slog.Logger
}

func newClient(conn *websocket.Conn, hub *Hub) *Client { return newClientWithMeta(conn, hub, "") }

func newClientWithMeta(conn *websocket.Conn, hub *Hub, userID string) *Client {
	return &Client{
		id: uuid.New().String(), UserID: userID,
		conn: conn, hub: hub,
		sendCh: make(chan []byte, sendBufSize),
		done:   make(chan struct{}),
		rooms:  make(map[string]struct{}),
		log:    hub.log,
	}
}

// ID returns the unique client identifier.
func (c *Client) ID() string { return c.id }

// OnMessage registers a handler for incoming messages.
func (c *Client) OnMessage(fn MessageHandler) { c.mu.Lock(); c.onMessage = fn; c.mu.Unlock() }

func (c *Client) send(msg []byte) {
	select {
	case c.sendCh <- msg:
		wsMessagesTotal.WithLabelValues("outbound").Inc()
	default:
		c.log.Warn("ws: send buffer full", "client_id", c.id)
	}
}

// Close signals the client to disconnect.
func (c *Client) Close() { close(c.sendCh) }

// Done returns a channel closed when the client disconnects.
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) readPump(ctx context.Context) {
	defer func() {
		close(c.done)
		c.hub.unregister <- c
	}()
	for {
		_, msg, err := c.conn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				c.log.Debug("ws: read error", "client_id", c.id, "error", err)
			}
			return
		}
		wsMessagesTotal.WithLabelValues("inbound").Inc()
		c.mu.RLock()
		h := c.onMessage
		c.mu.RUnlock()
		if h != nil {
			h(msg)
		}
	}
}

func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.sendCh:
			if !ok {
				_ = c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			wCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				c.log.Debug("ws: write error", "client_id", c.id, "error", err)
				return
			}
		case <-ticker.C:
			pCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pCtx)
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

func (c *Client) joinRoom(id string)  { c.mu.Lock(); c.rooms[id] = struct{}{}; c.mu.Unlock() }
func (c *Client) leaveRoom(id string) { c.mu.Lock(); delete(c.rooms, id); c.mu.Unlock() }
func (c *Client) allRooms() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r := make([]string, 0, len(c.rooms))
	for id := range c.rooms {
		r = append(r, id)
	}
	return r
}
func (c *Client) String() string { return fmt.Sprintf("Client(%s)", c.id) }
