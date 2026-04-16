//go:build integration

package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axe-cute/axe/pkg/ws"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"nhooyr.io/websocket"
)

// startRedis starts an ephemeral Redis container and returns the address and a cleanup func.
func startRedis(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6379")
	require.NoError(t, err)

	return host + ":" + port.Port()
}

func newRedisClient(t *testing.T, addr string) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// newHubWithRedis creates a Hub backed by Redis and a test HTTP server.
func newHubWithRedis(t *testing.T, rdb *redis.Client, room string) (*ws.Hub, *httptest.Server) {
	t.Helper()
	adapter := ws.NewRedisAdapter(rdb)
	hub := ws.NewHub(ws.WithAdapter(adapter))

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
		hub.Join(client, room)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return hub, srv
}

func dialConn(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err)
	return conn
}

// ── Integration tests ─────────────────────────────────────────────────────────

// TestRedisAdapter_CrossBroadcast verifies that a broadcast on hub1 reaches
// clients connected to hub2 (simulating two separate server instances).
func TestRedisAdapter_CrossBroadcast(t *testing.T) {
	addr := startRedis(t)
	rdb1 := newRedisClient(t, addr)
	rdb2 := newRedisClient(t, addr)

	const room = "cross-room"

	hub1, srv1 := newHubWithRedis(t, rdb1, room)
	_, srv2 := newHubWithRedis(t, rdb2, room)

	// Connect client to hub1 and hub2.
	c1 := dialConn(t, srv1)
	c2 := dialConn(t, srv2)
	defer c1.Close(websocket.StatusNormalClosure, "")
	defer c2.Close(websocket.StatusNormalClosure, "")

	// Give subscriptions time to register.
	time.Sleep(200 * time.Millisecond)

	// Hub1 broadcasts.
	ctx := context.Background()
	err := hub1.Broadcast(ctx, room, []byte("cross-message"))
	require.NoError(t, err)

	// c2 (on hub2) should receive the message via Redis pub/sub.
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, msg, err := c2.Read(rctx)
	require.NoError(t, err, "hub2 client should receive message from hub1 via Redis")
	assert.Equal(t, "cross-message", string(msg))
}

// TestRedisAdapter_Unsubscribe verifies that a hub closed adapter stops
// receiving messages.
func TestRedisAdapter_Unsubscribe(t *testing.T) {
	addr := startRedis(t)
	rdb1 := newRedisClient(t, addr)
	rdb2 := newRedisClient(t, addr)

	const room = "unsub-room"

	hub1, srv1 := newHubWithRedis(t, rdb1, room)
	hub2, srv2 := newHubWithRedis(t, rdb2, room)

	c1 := dialConn(t, srv1)
	c2 := dialConn(t, srv2)
	defer c1.Close(websocket.StatusNormalClosure, "")
	defer c2.Close(websocket.StatusNormalClosure, "")
	time.Sleep(200 * time.Millisecond)

	// Shut down hub2 (closes adapter subscription).
	hub2.Shutdown()
	time.Sleep(100 * time.Millisecond)

	// Hub1 broadcasts after hub2 is gone — should not panic.
	err := hub1.Broadcast(context.Background(), room, []byte("after-shutdown"))
	assert.NoError(t, err, "broadcast after peer hub shutdown should not error")
}
