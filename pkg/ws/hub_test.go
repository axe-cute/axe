package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/axe-cute/axe/pkg/ws"
)

// dialWS dials a test server's WebSocket endpoint and returns the conn.
func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

// newTestHub creates a Hub and starts its Run loop in a background goroutine.
// The hub shuts down when ctx is cancelled.
func newTestHub(ctx context.Context) *ws.Hub {
	hub := ws.NewHub()
	go hub.Run(ctx)
	return hub
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestHub_Upgrade_Connect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		<-client.Done()
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow() //nolint:errcheck

	// Give hub time to register.
	time.Sleep(50 * time.Millisecond)

	if got := hub.ClientCount(); got != 1 {
		t.Errorf("ClientCount = %d, want 1", got)
	}
}

func TestHub_Join_Presence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	const room = "lobby"

	var wg sync.WaitGroup

	makeHandler := func() http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			client, err := hub.Upgrade(w, r)
			if err != nil {
				return
			}
			hub.Join(client, room)
			wg.Done()
			<-client.Done()
		}
	}

	srv := httptest.NewServer(makeHandler())
	defer srv.Close()

	// Connect 3 clients.
	conns := make([]*websocket.Conn, 3)
	wg.Add(3)
	for i := range conns {
		conns[i] = dialWS(t, srv)
	}
	wg.Wait() // all 3 joined the room

	// Give hub a little time to process registers.
	time.Sleep(50 * time.Millisecond)

	presence := hub.Presence(room)
	if len(presence) != 3 {
		t.Errorf("Presence = %d, want 3", len(presence))
	}
	if hub.RoomSize(room) != 3 {
		t.Errorf("RoomSize = %d, want 3", hub.RoomSize(room))
	}

	// Close all connections.
	for _, c := range conns {
		c.CloseNow() //nolint:errcheck
	}
}

func TestHub_Broadcast(t *testing.T) {
	// Scenario: 2 clients join the same room.
	// Server broadcasts directly via hub.Broadcast() → both clients receive it
	// via their WS connection.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	const room = "chat"

	joined := make(chan struct{}, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		hub.Join(client, room)
		joined <- struct{}{} // signal that this client joined
		<-client.Done()
	}))
	defer srv.Close()

	// Connect two clients.
	conn1 := dialWS(t, srv)
	defer conn1.CloseNow() //nolint:errcheck
	conn2 := dialWS(t, srv)
	defer conn2.CloseNow() //nolint:errcheck

	// Wait for both to join.
	<-joined
	<-joined
	time.Sleep(30 * time.Millisecond) // let hub register

	// Broadcast from server side.
	payload := []byte(`{"event":"new_post"}`)
	if err := hub.Broadcast(ctx, room, payload); err != nil {
		t.Fatalf("hub.Broadcast: %v", err)
	}

	// Both conns should receive the message.
	readMsg := func(conn *websocket.Conn, label string) {
		conn.SetReadLimit(1 << 20)
		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("%s read error: %v", label, err)
			return
		}
		if string(msg) != string(payload) {
			t.Errorf("%s got %q, want %q", label, msg, payload)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); readMsg(conn1, "conn1") }()
	go func() { defer wg.Done(); readMsg(conn2, "conn2") }()
	wg.Wait()
}

func TestHub_Broadcast_Direct(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	const room = "notif"

	joined := make(chan struct{})
	recvSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		hub.Join(client, room)
		close(joined)
		<-client.Done()
	}))
	defer recvSrv.Close()

	recvConn := dialWS(t, recvSrv)
	defer recvConn.CloseNow() //nolint:errcheck

	<-joined
	time.Sleep(30 * time.Millisecond)

	// Broadcast directly from server side.
	payload := []byte(`{"event":"update"}`)
	if err := hub.Broadcast(ctx, room, payload); err != nil {
		t.Fatalf("hub.Broadcast: %v", err)
	}

	// The receiver WebSocket conn should receive it.
	recvConn.SetReadLimit(1 << 20)
	_, msg, err := recvConn.Read(ctx)
	if err != nil {
		t.Fatalf("recv read: %v", err)
	}
	if string(msg) != string(payload) {
		t.Errorf("got %q, want %q", msg, payload)
	}
}

func TestHub_GracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	hub := newTestHub(ctx)

	disconnected := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		<-client.Done()
		disconnected <- struct{}{}
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)

	// Cancel context → triggers hub shutdown → clients closed.
	cancel()

	select {
	case <-disconnected:
		// ✅ client was disconnected by hub shutdown
	case <-time.After(2 * time.Second):
		t.Error("timeout: client not disconnected after hub shutdown")
	}
}

func TestHub_Leave_CleanupRoom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	const room = "temp"

	var clientRef *ws.Client
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		hub.Join(client, room)
		mu.Lock()
		clientRef = client
		mu.Unlock()
		<-client.Done()
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	time.Sleep(50 * time.Millisecond)

	if hub.RoomSize(room) != 1 {
		t.Errorf("before leave: RoomSize = %d, want 1", hub.RoomSize(room))
	}

	// Disconnect → hub unregisters the client, room should be cleaned up.
	conn.CloseNow() //nolint:errcheck
	time.Sleep(100 * time.Millisecond)

	if hub.RoomSize(room) != 0 {
		t.Errorf("after leave: RoomSize = %d, want 0", hub.RoomSize(room))
	}

	mu.Lock()
	_ = clientRef
	mu.Unlock()
}

// ── UserConnTracker tests ─────────────────────────────────────────────────────

func TestUserConnTracker_AcquireRelease(t *testing.T) {
	tracker := ws.NewUserConnTracker()
	const userID = "user-1"
	const max = 3

	// Acquire up to max.
	for i := 0; i < max; i++ {
		if !tracker.Acquire(userID, max) {
			t.Fatalf("Acquire #%d failed, expected success", i+1)
		}
	}

	// Should be at limit now.
	if tracker.Acquire(userID, max) {
		t.Error("Acquire should fail when at limit")
	}
	if tracker.Count(userID) != int64(max) {
		t.Errorf("Count = %d, want %d", tracker.Count(userID), max)
	}

	// Release one and try again.
	tracker.Release(userID)
	if !tracker.Acquire(userID, max) {
		t.Error("Acquire should succeed after Release")
	}
}

func TestUserConnTracker_ReleaseUnderflow(t *testing.T) {
	tracker := ws.NewUserConnTracker()
	// Releasing without acquiring should not panic or go negative.
	tracker.Release("ghost-user")
	if tracker.Count("ghost-user") != 0 {
		t.Errorf("Count = %d after Release on zero, want 0", tracker.Count("ghost-user"))
	}
}
