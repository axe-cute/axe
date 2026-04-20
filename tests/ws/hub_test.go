package ws_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"

	"github.com/axe-cute/axe/pkg/ws"
)

// dialHub dials the given test server and returns a *websocket.Conn.
func dialHub(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err)
	return conn
}

// newTestHub creates a Hub and starts it in the background; registers /ws.
func newTestHub(t *testing.T, opts ...ws.HubOption) (*ws.Hub, *httptest.Server) {
	t.Helper()
	hub := ws.NewHub(opts...)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		hub.Shutdown()
	})
	go hub.Run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		client, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		hub.Join(client, "room1")
		client.OnMessage(func(msg []byte) {
			_ = hub.Broadcast(r.Context(), "room1", msg)
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return hub, srv
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestHub_ClientCount(t *testing.T) {
	hub, srv := newTestHub(t)

	c1 := dialHub(t, srv)
	c2 := dialHub(t, srv)
	time.Sleep(50 * time.Millisecond) // let register loop run

	assert.Equal(t, 2, hub.ClientCount(), "two clients should be registered")

	c1.Close(websocket.StatusNormalClosure, "bye")
	c2.Close(websocket.StatusNormalClosure, "bye")
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, hub.ClientCount(), "all clients should be unregistered after close")
}

func TestHub_JoinAndBroadcast(t *testing.T) {
	_, srv := newTestHub(t)

	// Three clients connect and join "room1".
	c1 := dialHub(t, srv)
	c2 := dialHub(t, srv)
	c3 := dialHub(t, srv)
	defer c1.Close(websocket.StatusNormalClosure, "")
	defer c2.Close(websocket.StatusNormalClosure, "")
	defer c3.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond) // let joins propagate

	// c1 sends a message → c1, c2, c3 all receive it (echo broadcast).
	payload := []byte("hello room")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := c1.Write(ctx, websocket.MessageText, payload)
	require.NoError(t, err)

	var mu sync.Mutex
	received := map[int][]byte{}

	readFrom := func(id int, conn *websocket.Conn) {
		rctx, rcancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer rcancel()
		_, msg, err := conn.Read(rctx)
		if err == nil {
			mu.Lock()
			received[id] = msg
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	for i, conn := range []*websocket.Conn{c1, c2, c3} {
		wg.Add(1)
		go func(id int, c *websocket.Conn) {
			defer wg.Done()
			readFrom(id, c)
		}(i, conn)
	}
	wg.Wait()

	assert.Equal(t, string(payload), string(received[0]), "c1 should receive broadcast")
	assert.Equal(t, string(payload), string(received[1]), "c2 should receive broadcast")
	assert.Equal(t, string(payload), string(received[2]), "c3 should receive broadcast")
}

func TestHub_Presence(t *testing.T) {
	hub, srv := newTestHub(t)

	c1 := dialHub(t, srv)
	c2 := dialHub(t, srv)
	defer c1.Close(websocket.StatusNormalClosure, "")
	defer c2.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond)

	presence := hub.Presence("room1")
	assert.Len(t, presence, 2, "room1 should have 2 members")
}

func TestHub_GracefulShutdown(t *testing.T) {
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		_, _ = hub.Upgrade(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	go hub.Run(ctx)
	time.Sleep(20 * time.Millisecond)

	c := dialHub(t, srv)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, 1, hub.ClientCount())

	// Cancel context → hub shuts down → client should be closed.
	cancel()
	hub.Shutdown()
	time.Sleep(100 * time.Millisecond)

	// Attempt a read after shutdown; should get an error.
	_, _, err := c.Read(context.Background())
	assert.Error(t, err, "reading from a shutdown client should fail")
}

func TestHub_LeaveRoom(t *testing.T) {
	hub, srv := newTestHub(t)

	c1 := dialHub(t, srv)
	time.Sleep(50 * time.Millisecond)

	// Manually broadcast from hub; c1 should receive it.
	ctx := context.Background()
	_ = hub.Broadcast(ctx, "room1", []byte("ping"))

	rctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, msg, err := c1.Read(rctx)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(msg))

	c1.Close(websocket.StatusNormalClosure, "")
}

func TestHub_EmptyRoomDestroyed(t *testing.T) {
	hub, srv := newTestHub(t)

	c1 := dialHub(t, srv)
	time.Sleep(50 * time.Millisecond)
	assert.Len(t, hub.Presence("room1"), 1)

	c1.Close(websocket.StatusNormalClosure, "bye")
	time.Sleep(100 * time.Millisecond)

	// After client leaves, room should be destroyed (presence returns nil).
	assert.Nil(t, hub.Presence("room1"), "empty room should be destroyed")
}

func TestHub_MultipleRooms(t *testing.T) {
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); hub.Shutdown() })
	go hub.Run(ctx)

	// Create two separate rooms via separate handlers.
	mux := http.NewServeMux()
	for _, room := range []string{"alpha", "beta"} {
		r := room
		mux.HandleFunc(fmt.Sprintf("/%s", r), func(w http.ResponseWriter, req *http.Request) {
			client, err := hub.Upgrade(w, req)
			if err != nil {
				return
			}
			hub.Join(client, r)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dialRoom := func(room string) *websocket.Conn {
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/" + room
		conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
		require.NoError(t, err)
		return conn
	}

	ca := dialRoom("alpha")
	cb := dialRoom("beta")
	defer ca.Close(websocket.StatusNormalClosure, "")
	defer cb.Close(websocket.StatusNormalClosure, "")
	time.Sleep(50 * time.Millisecond)

	assert.Len(t, hub.Presence("alpha"), 1)
	assert.Len(t, hub.Presence("beta"), 1)

	_ = hub.Broadcast(ctx, "alpha", []byte("to-alpha"))

	rctx, rcancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer rcancel()
	_, msg, err := ca.Read(rctx)
	require.NoError(t, err)
	assert.Equal(t, "to-alpha", string(msg), "alpha client should receive message")
}
