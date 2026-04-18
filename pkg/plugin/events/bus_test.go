package events

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── TopicMatches ──────────────────────────────────────────────────────────────

func TestTopicMatches_Exact(t *testing.T) {
	assert.True(t, topicMatches("storage.uploaded", "storage.uploaded"))
	assert.False(t, topicMatches("storage.uploaded", "storage.deleted"))
}

func TestTopicMatches_Wildcard(t *testing.T) {
	assert.True(t, topicMatches("storage.*", "storage.uploaded"))
	assert.True(t, topicMatches("storage.*", "storage.deleted"))
	assert.False(t, topicMatches("storage.*", "user.registered"))
}

func TestTopicMatches_GlobalWildcard(t *testing.T) {
	assert.True(t, topicMatches("*", "storage.uploaded"))
	assert.True(t, topicMatches("*", "user.registered"))
	assert.True(t, topicMatches("*", "anything"))
}

func TestTopicMatches_NoMatch(t *testing.T) {
	assert.False(t, topicMatches("user.*", "storage.uploaded"))
}

// ── Sync delivery ─────────────────────────────────────────────────────────────

func TestBus_Sync_HandlerReceivesEvent(t *testing.T) {
	b := NewInProcessBus(nil)

	var received Event
	b.Subscribe(TopicStorageUploaded, func(_ context.Context, e Event) error {
		received = e
		return nil
	})

	evt := Event{
		Topic:   TopicStorageUploaded,
		Payload: map[string]any{"key": "uploads/photo.jpg"},
		Meta:    EventMeta{PluginSource: "storage"},
	}
	require.NoError(t, b.Publish(context.Background(), evt))

	assert.Equal(t, TopicStorageUploaded, received.Topic)
	assert.Equal(t, "uploads/photo.jpg", received.Payload["key"])
}

func TestBus_Sync_BlocksUntilHandlerReturns(t *testing.T) {
	b := NewInProcessBus(nil)

	done := false
	b.Subscribe(TopicUserRegistered, func(_ context.Context, _ Event) error {
		time.Sleep(10 * time.Millisecond)
		done = true
		return nil
	})

	_ = b.Publish(context.Background(), Event{Topic: TopicUserRegistered})
	assert.True(t, done, "sync handler must complete before Publish returns")
}

func TestBus_Sync_MultipleSubscribers(t *testing.T) {
	b := NewInProcessBus(nil)
	var count atomic.Int32

	for i := 0; i < 3; i++ {
		b.Subscribe(TopicUserLogin, func(_ context.Context, _ Event) error {
			count.Add(1)
			return nil
		})
	}
	_ = b.Publish(context.Background(), Event{Topic: TopicUserLogin})
	assert.Equal(t, int32(3), count.Load())
}

// ── Async delivery ────────────────────────────────────────────────────────────

func TestBus_Async_NonBlocking(t *testing.T) {
	b := NewInProcessBus(nil)

	ready := make(chan struct{})
	b.SubscribeAsync(TopicJobEnqueued, func(_ context.Context, _ Event) error {
		// Signal handler has run.
		close(ready)
		return nil
	})

	start := time.Now()
	_ = b.Publish(context.Background(), Event{Topic: TopicJobEnqueued})
	elapsed := time.Since(start)

	// Publish should return before the 100ms sleep in the handler.
	assert.Less(t, elapsed, 50*time.Millisecond, "Async Publish must return immediately")

	// Handler should eventually run.
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("async handler never ran")
	}
}

// ── Wildcard subscription ─────────────────────────────────────────────────────

func TestBus_Wildcard_MatchesAllStorageEvents(t *testing.T) {
	b := NewInProcessBus(nil)

	var topics []string
	b.Subscribe("storage.*", func(_ context.Context, e Event) error {
		topics = append(topics, e.Topic)
		return nil
	})

	_ = b.Publish(context.Background(), Event{Topic: "storage.uploaded"})
	_ = b.Publish(context.Background(), Event{Topic: "storage.deleted"})
	_ = b.Publish(context.Background(), Event{Topic: "user.registered"}) // should NOT match

	require.Len(t, topics, 2)
	assert.Equal(t, "storage.uploaded", topics[0])
	assert.Equal(t, "storage.deleted", topics[1])
}

// ── Error handling ────────────────────────────────────────────────────────────

func TestBus_Sync_HandlerError_DoesNotPanic(t *testing.T) {
	b := NewInProcessBus(nil)

	b.Subscribe(TopicEmailFailed, func(_ context.Context, _ Event) error {
		return errors.New("handler failed")
	})

	// Must not panic, errors are logged.
	require.NotPanics(t, func() {
		_ = b.Publish(context.Background(), Event{Topic: TopicEmailFailed})
	})
}

// ── Timestamp auto-fill ───────────────────────────────────────────────────────

func TestBus_AutoFillsTimestamp(t *testing.T) {
	b := NewInProcessBus(nil)

	var received Event
	b.Subscribe(TopicPaymentSucceeded, func(_ context.Context, e Event) error {
		received = e
		return nil
	})

	_ = b.Publish(context.Background(), Event{Topic: TopicPaymentSucceeded})
	assert.False(t, received.Meta.Timestamp.IsZero(), "timestamp must be set by Publish")
}

// ── NoopBus ───────────────────────────────────────────────────────────────────

func TestNoopBus_NeverPanics(t *testing.T) {
	var b NoopBus
	require.NotPanics(t, func() {
		b.Subscribe("any", func(_ context.Context, _ Event) error { return nil })
		_ = b.Publish(context.Background(), Event{Topic: "any"})
	})
}

// ── Standard topic constants ──────────────────────────────────────────────────

func TestTopicConstants_HaveCorrectFormat(t *testing.T) {
	topics := []string{
		TopicStorageUploaded, TopicStorageDeleted,
		TopicUserRegistered, TopicUserDeleted, TopicUserLogin,
		TopicJobEnqueued, TopicJobCompleted, TopicJobFailed,
		TopicEmailSent, TopicEmailFailed,
		TopicPaymentSucceeded, TopicPaymentFailed,
	}
	for _, topic := range topics {
		t.Run(topic, func(t *testing.T) {
			assert.Contains(t, topic, ".", "topics must follow {plugin}.{event} format")
		})
	}
}
