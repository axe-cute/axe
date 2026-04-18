package sentry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_ValidConfig(t *testing.T) {
	p, err := New(Config{DSN: "http://key@localhost/1"})
	require.NoError(t, err)
	assert.Equal(t, "sentry", p.Name())
}

func TestNew_Defaults(t *testing.T) {
	p, err := New(Config{})
	require.NoError(t, err)
	assert.Equal(t, "development", p.cfg.Environment)
	assert.Equal(t, 2*time.Second, p.cfg.FlushTimeout)
	assert.Contains(t, p.cfg.Release, "axe@v")
}

// ── Plugin lifecycle ──────────────────────────────────────────────────────────

func TestRegister_NoOpWhenEmptyDSN(t *testing.T) {
	p, err := New(Config{DSN: ""})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	err = p.Register(t.Context(), app)
	require.NoError(t, err)
}

func TestMinAxeVersion(t *testing.T) {
	p, _ := New(Config{})
	assert.NotEmpty(t, p.MinAxeVersion())
}

func TestShutdown_EmptyDSN(t *testing.T) {
	p, _ := New(Config{DSN: ""})
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Middleware ────────────────────────────────────────────────────────────────

func TestMiddleware_Captures5xx(t *testing.T) {
	// Initialize Sentry with a mock transport to verify captured events.
	transport := &mockTransport{}

	p, _ := New(Config{
		DSN:       "http://key@localhost/1",
		Transport: transport,
		GetUserID: func(ctx context.Context) string {
			return "user-123"
		},
	})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	// Register a route that returns 500
	app.Router.Get("/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(app.Router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/fail")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// Wait briefly for the event to be delivered to the mock transport
	sentry.Flush(2 * time.Second)

	require.Len(t, transport.events, 1, "Expected one 5xx event to be captured")
	event := transport.events[0]
	assert.Contains(t, event.Message, "HTTP 500 on GET /fail")
	assert.Equal(t, "user-123", event.User.ID, "User ID should be enriched")
}

// TestMiddleware_CapturesPanic verifies that a panic in a handler is captured
// by Sentry. We use httptest.NewRecorder to drive the request in-process so
// we have full control over the goroutine lifecycle — avoiding the race between
// the Go http.Server recover and sentry's flush.
func TestMiddleware_CapturesPanic(t *testing.T) {
	transport := &mockTransport{}

	p, _ := New(Config{
		DSN:       "http://key@localhost/1",
		Transport: transport,
		Repanic:   boolPtr(false), // don't re-panic so we can inspect the event
	})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	app.Router.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Drive the request in-process: no separate goroutine, no server recover race.
	r := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()

	// Sentry's wrapper catches the panic, captures it, and (with Repanic=false)
	// returns normally so we can inspect the transport immediately.
	app.Router.ServeHTTP(w, r)

	sentry.Flush(2 * time.Second)

	require.Len(t, transport.events, 1, "Expected one panic event to be captured")
	event := transport.events[0]
	t.Logf("event level=%s message=%q exception_count=%d", event.Level, event.Message, len(event.Exception))
	assert.Equal(t, sentry.LevelFatal, event.Level, "Panic event should be fatal")
	// sentry-go may capture the panic value in Exception or Message depending on version.
	if len(event.Exception) > 0 {
		assert.Equal(t, "test panic", event.Exception[0].Value)
	} else {
		assert.Contains(t, event.Message, "test panic")
	}
}

func boolPtr(v bool) *bool { return &v }



// ── Mock Transport ────────────────────────────────────────────────────────

type mockTransport struct {
	events []*sentry.Event
}

func (m *mockTransport) Configure(options sentry.ClientOptions) {}
func (m *mockTransport) SendEvent(event *sentry.Event) {
	m.events = append(m.events, event)
}
func (m *mockTransport) Flush(timeout time.Duration) bool { return true }
func (m *mockTransport) FlushWithContext(_ context.Context) bool { return true }
func (m *mockTransport) Close()                            {}
