package ws

import "sync"

// Room is a named group of WebSocket clients.
type Room struct {
	id      string
	clients map[string]*Client
	mu      sync.RWMutex
}

func newRoom(id string) *Room { return &Room{id: id, clients: make(map[string]*Client)} }

func (r *Room) add(c *Client)          { r.mu.Lock(); r.clients[c.id] = c; r.mu.Unlock() }
func (r *Room) remove(clientID string) { r.mu.Lock(); delete(r.clients, clientID); r.mu.Unlock() }
func (r *Room) broadcast(msg []byte) {
	r.mu.RLock(); defer r.mu.RUnlock()
	for _, c := range r.clients { c.send(msg) }
}

// Presence returns the list of client IDs in this room.
func (r *Room) Presence() []string {
	r.mu.RLock(); defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.clients))
	for id := range r.clients { ids = append(ids, id) }
	return ids
}

// Size returns the number of clients in this room.
func (r *Room) Size() int          { r.mu.RLock(); defer r.mu.RUnlock(); return len(r.clients) }
func (r *Room) isEmpty() bool      { r.mu.RLock(); defer r.mu.RUnlock(); return len(r.clients) == 0 }
