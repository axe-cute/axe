package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/axe-cute/axe/pkg/ws"
)

// ── Room unit tests ──────────────────────────────────────────────────────────

func TestHub_RoomSize_NonExistent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	if got := hub.RoomSize("nonexistent"); got != 0 {
		t.Errorf("RoomSize(nonexistent) = %d, want 0", got)
	}
}

func TestHub_Presence_NonExistent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	if got := hub.Presence("nonexistent"); got != nil {
		t.Errorf("Presence(nonexistent) = %v, want nil", got)
	}
}

func TestHub_Broadcast_NoRoom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	// Broadcasting to a non-existent room should not error (no-op local, adapter only).
	if err := hub.Broadcast(ctx, "ghost-room", []byte("hello")); err != nil {
		t.Errorf("Broadcast to ghost-room: %v", err)
	}
}

func TestHub_Leave_ExplicitRemoval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	const room = "leave-test"

	var clientRef *ws.Client
	var mu sync.Mutex
	joined := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		hub.Join(client, room)
		mu.Lock()
		clientRef = client
		mu.Unlock()
		close(joined)
		<-client.Done()
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow() //nolint:errcheck
	<-joined
	time.Sleep(50 * time.Millisecond)

	if hub.RoomSize(room) != 1 {
		t.Fatalf("before Leave: RoomSize = %d, want 1", hub.RoomSize(room))
	}

	mu.Lock()
	c := clientRef
	mu.Unlock()

	hub.Leave(c, room)
	// Room should be empty and cleaned up.
	if got := hub.RoomSize(room); got != 0 {
		t.Errorf("after Leave: RoomSize = %d, want 0", got)
	}
}

func TestHub_Leave_NonExistentRoom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)

	var clientRef *ws.Client
	var mu sync.Mutex
	joined := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		mu.Lock()
		clientRef = client
		mu.Unlock()
		close(joined)
		<-client.Done()
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow() //nolint:errcheck
	<-joined
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	c := clientRef
	mu.Unlock()

	// Leaving a room that doesn't exist should not panic.
	hub.Leave(c, "never-joined")
}

func TestHub_MultiRoom_Join(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	rooms := []string{"room-a", "room-b", "room-c"}

	joined := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		for _, room := range rooms {
			hub.Join(client, room)
		}
		close(joined)
		<-client.Done()
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow() //nolint:errcheck
	<-joined
	time.Sleep(50 * time.Millisecond)

	for _, room := range rooms {
		if got := hub.RoomSize(room); got != 1 {
			t.Errorf("RoomSize(%s) = %d, want 1", room, got)
		}
	}
}

func TestHub_OnMessage_Echo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	const room = "echo"

	joined := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		hub.Join(client, room)
		client.OnMessage(func(msg []byte) {
			_ = hub.Broadcast(hub.Context(), room, msg)
		})
		close(joined)
		<-client.Done()
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow() //nolint:errcheck
	<-joined
	time.Sleep(30 * time.Millisecond)

	payload := []byte(`{"ping":"pong"}`)
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadLimit(1 << 20)
	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != string(payload) {
		t.Errorf("echo got %q, want %q", msg, payload)
	}
}

func TestHub_ClientCount_MultiConnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	ready := make(chan struct{}, 5)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		ready <- struct{}{}
		<-client.Done()
	}))
	defer srv.Close()

	const n = 5
	conns := make([]*websocket.Conn, n)
	for i := 0; i < n; i++ {
		conns[i] = dialWS(t, srv)
	}
	for i := 0; i < n; i++ {
		<-ready
	}
	time.Sleep(50 * time.Millisecond)

	if got := hub.ClientCount(); got != n {
		t.Errorf("ClientCount = %d, want %d", got, n)
	}

	// Disconnect 2.
	conns[0].CloseNow() //nolint:errcheck
	conns[1].CloseNow() //nolint:errcheck
	time.Sleep(100 * time.Millisecond)

	if got := hub.ClientCount(); got != n-2 {
		t.Errorf("ClientCount after 2 disconnects = %d, want %d", got, n-2)
	}

	for _, c := range conns[2:] {
		c.CloseNow() //nolint:errcheck
	}
}

func TestHub_Shutdown_CleansUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := newTestHub(ctx)

	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		hub.Join(client, "cleanup-room")
		close(ready)
		<-client.Done()
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow() //nolint:errcheck
	<-ready
	time.Sleep(50 * time.Millisecond)

	hub.Shutdown()
	time.Sleep(100 * time.Millisecond)

	if got := hub.ClientCount(); got != 0 {
		t.Errorf("after Shutdown: ClientCount = %d, want 0", got)
	}
	if got := hub.RoomSize("cleanup-room"); got != 0 {
		t.Errorf("after Shutdown: RoomSize = %d, want 0", got)
	}
}

func TestHub_Context(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	hub := newTestHub(ctx)
	hubCtx := hub.Context()

	if hubCtx == nil {
		t.Fatal("Hub.Context() returned nil")
	}

	select {
	case <-hubCtx.Done():
		t.Fatal("Hub context done before shutdown")
	default:
		// expected: not done
	}
}

// ── MemoryAdapter tests ──────────────────────────────────────────────────────

func TestMemoryAdapter_NoOp(t *testing.T) {
	a := ws.MemoryAdapter{}

	if err := a.Publish(context.Background(), "ch", []byte("hi")); err != nil {
		t.Errorf("Publish: %v", err)
	}
	if err := a.Subscribe(context.Background(), "ch", func([]byte) {}); err != nil {
		t.Errorf("Subscribe: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// ── UserConnTracker concurrent tests ─────────────────────────────────────────

func TestUserConnTracker_ConcurrentAcquireRelease(t *testing.T) {
	tracker := ws.NewUserConnTracker()
	const max = 5
	const userID = "concurrent-user"

	var wg sync.WaitGroup
	successes := make(chan bool, 100)

	// 20 goroutines try to acquire max=5 slots.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok := tracker.Acquire(userID, max)
			successes <- ok
			if ok {
				time.Sleep(10 * time.Millisecond)
				tracker.Release(userID)
			}
		}()
	}
	wg.Wait()
	close(successes)

	acquired := 0
	for ok := range successes {
		if ok {
			acquired++
		}
	}

	if acquired == 0 {
		t.Error("no goroutines acquired; expected some to succeed")
	}
	// After all releases, count should be 0.
	if got := tracker.Count(userID); got != 0 {
		t.Errorf("Count after all releases = %d, want 0", got)
	}
}

func TestUserConnTracker_CountForUnknownUser(t *testing.T) {
	tracker := ws.NewUserConnTracker()
	if got := tracker.Count("unknown"); got != 0 {
		t.Errorf("Count(unknown) = %d, want 0", got)
	}
}

// ── extractWSToken tests (via WSAuth middleware) ──────────────────────────────

func TestClaimsFromCtx_Nil(t *testing.T) {
	// Empty context should return nil claims.
	claims := ws.ClaimsFromCtx(context.Background())
	if claims != nil {
		t.Errorf("ClaimsFromCtx(empty) = %v, want nil", claims)
	}
}

func TestWithMaxConnsPerUser_Zero(t *testing.T) {
	// WithMaxConnsPerUser(0) should not change default (noop).
	// This exercises the option function without panicking.
	opt := ws.WithMaxConnsPerUser(0)
	if opt == nil {
		t.Error("WithMaxConnsPerUser returned nil")
	}
}
