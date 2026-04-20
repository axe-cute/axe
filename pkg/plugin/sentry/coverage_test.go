package sentry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
)

// ── Shutdown: DSN set (exercises flush path) ─────────────────────────────────

func TestShutdown_WithDSN_Flushes(t *testing.T) {
	transport := &mockTransport{}
	p, _ := New(Config{
		DSN:       "http://key@localhost/1",
		Transport: transport,
	})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
	require.NoError(t, p.Shutdown(t.Context()))
}

func TestShutdown_WithContextDeadline(t *testing.T) {
	transport := &mockTransport{}
	p, _ := New(Config{
		DSN:          "http://key@localhost/1",
		Transport:    transport,
		FlushTimeout: 10 * time.Second,
	})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	// Use a short context deadline — exercises the ctxTimeout < timeout branch.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.NoError(t, p.Shutdown(ctx))
}

func TestShutdown_LargeContextDeadline(t *testing.T) {
	transport := &mockTransport{}
	p, _ := New(Config{
		DSN:          "http://key@localhost/1",
		Transport:    transport,
		FlushTimeout: 50 * time.Millisecond,
	})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	// Context deadline is longer than FlushTimeout — uses FlushTimeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(ctx))
}

// ── Config edge cases ────────────────────────────────────────────────────────

func TestNew_CustomConfig(t *testing.T) {
	p, err := New(Config{
		DSN:              "http://key@localhost/1",
		Environment:      "production",
		Release:          "v1.0.0",
		FlushTimeout:     5 * time.Second,
		TracesSampleRate: 0.5,
	})
	require.NoError(t, err)
	require.Equal(t, "production", p.cfg.Environment)
	require.Equal(t, "v1.0.0", p.cfg.Release)
	require.Equal(t, 5*time.Second, p.cfg.FlushTimeout)
}

func TestRegister_WithRepanic(t *testing.T) {
	transport := &mockTransport{}
	p, _ := New(Config{
		DSN:       "http://key@localhost/1",
		Transport: transport,
		Repanic:   boolPtr(true),
	})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
	require.NoError(t, p.Shutdown(t.Context()))
}
