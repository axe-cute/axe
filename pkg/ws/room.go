package ws

import (
	"sync"
)

// Room is a named group of connected WebSocket clients.
// All methods are safe for concurrent use.
type Room struct {
	id      string
	clients map[string]*Client
	mu      sync.RWMutex
}

func newRoom(id string) *Room {
	return &Room{
		id:      id,
		clients: make(map[string]*Client),
	}
}

// add registers a client in the room.
func (r *Room) add(c *Client) {
	r.mu.Lock()
	r.clients[c.id] = c
	r.mu.Unlock()
}

// remove unregisters a client from the room.
func (r *Room) remove(clientID string) {
	r.mu.Lock()
	delete(r.clients, clientID)
	r.mu.Unlock()
}

// broadcast delivers msg to every client currently in the room.
// Delivery is non-blocking: slow clients whose send buffer is full are skipped
// and logged by Client.Send.
func (r *Room) broadcast(msg []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		c.send(msg)
	}
}

// Presence returns the IDs of all clients currently in the room.
func (r *Room) Presence() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.clients))
	for id := range r.clients {
		ids = append(ids, id)
	}
	return ids
}

// Size returns the number of clients currently in the room.
func (r *Room) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// isEmpty reports whether the room has no clients.
func (r *Room) isEmpty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients) == 0
}
