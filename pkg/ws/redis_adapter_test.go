package ws_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/axe-cute/axe/pkg/ws"
)

// ── RedisAdapter tests ───────────────────────────────────────────────────────

func TestRedisAdapter_NewAndClose(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	adapter := ws.NewRedisAdapter(rdb)
	if adapter == nil {
		t.Fatal("NewRedisAdapter returned nil")
	}

	// Close with no subscriptions should be a no-op.
	if err := adapter.Close(); err != nil {
		t.Errorf("Close (no subs): %v", err)
	}
}

func TestRedisAdapter_WithOptions(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	adapter := ws.NewRedisAdapter(rdb,
		ws.WithRedisPrefix("custom:"),
		ws.WithRedisLogger(slog.Default()),
	)
	if adapter == nil {
		t.Fatal("NewRedisAdapter with options returned nil")
	}
	adapter.Close() //nolint:errcheck
}

func TestRedisAdapter_PublishSubscribe(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	adapter := ws.NewRedisAdapter(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	var received []string

	// Subscribe first.
	err = adapter.Subscribe(ctx, "test-room", func(msg []byte) {
		mu.Lock()
		received = append(received, string(msg))
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Give subscription goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	// Publish a message.
	if err := adapter.Publish(ctx, "test-room", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Wait for message to arrive.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Error("expected to receive at least 1 message, got 0")
	} else if received[0] != "hello" {
		t.Errorf("received[0] = %q, want %q", received[0], "hello")
	}

	// Cleanup.
	if err := adapter.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestRedisAdapter_Publish_NoSubscribers(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	adapter := ws.NewRedisAdapter(rdb)
	defer adapter.Close() //nolint:errcheck

	// Publishing without subscribers should not error.
	if err := adapter.Publish(context.Background(), "empty-room", []byte("orphan")); err != nil {
		t.Errorf("Publish (no subs): %v", err)
	}
}

func TestRedisAdapter_MultipleChannels(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	adapter := ws.NewRedisAdapter(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	ch1Msgs := []string{}
	ch2Msgs := []string{}

	err = adapter.Subscribe(ctx, "ch1", func(msg []byte) {
		mu.Lock()
		ch1Msgs = append(ch1Msgs, string(msg))
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Subscribe ch1: %v", err)
	}

	err = adapter.Subscribe(ctx, "ch2", func(msg []byte) {
		mu.Lock()
		ch2Msgs = append(ch2Msgs, string(msg))
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Subscribe ch2: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	adapter.Publish(ctx, "ch1", []byte("msg1")) //nolint:errcheck
	adapter.Publish(ctx, "ch2", []byte("msg2")) //nolint:errcheck
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(ch1Msgs) != 1 || ch1Msgs[0] != "msg1" {
		t.Errorf("ch1 messages: %v, want [msg1]", ch1Msgs)
	}
	if len(ch2Msgs) != 1 || ch2Msgs[0] != "msg2" {
		t.Errorf("ch2 messages: %v, want [msg2]", ch2Msgs)
	}

	adapter.Close() //nolint:errcheck
}

func TestRedisAdapter_SubscribeCancelled(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	adapter := ws.NewRedisAdapter(rdb)

	ctx, cancel := context.WithCancel(context.Background())

	err = adapter.Subscribe(ctx, "cancel-test", func(msg []byte) {})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Cancel context — subscription goroutine should exit.
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Close should work cleanly.
	adapter.Close() //nolint:errcheck
}
