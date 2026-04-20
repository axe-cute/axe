package otel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"

	"github.com/axe-cute/axe/pkg/plugin"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_MissingServiceName(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ServiceName")
}

func TestNew_ValidConfig(t *testing.T) {
	p, err := New(Config{ServiceName: "test-svc"})
	require.NoError(t, err)
	assert.Equal(t, "otel", p.Name())
}

func TestNew_Defaults(t *testing.T) {
	p, err := New(Config{ServiceName: "svc"})
	require.NoError(t, err)
	assert.Equal(t, "unknown", p.cfg.ServiceVersion)
	assert.Equal(t, 1.0, p.cfg.SampleRate)
}

// ── Plugin lifecycle ──────────────────────────────────────────────────────────

func TestRegister_DevMode_NoEndpoint(t *testing.T) {
	p, err := New(Config{ServiceName: "test-svc"})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	// Dev mode: no endpoint → stdout exporter.
	err = p.Register(t.Context(), app)
	require.NoError(t, err)
	assert.NotNil(t, p.provider)
}

func TestRegister_ProvidesTracer(t *testing.T) {
	p, err := New(Config{ServiceName: "test-svc"})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	tracer, ok := plugin.Resolve[trace.Tracer](app, ServiceKey)
	require.True(t, ok, "trace.Tracer must be resolvable via service locator")
	assert.NotNil(t, tracer)
}

func TestMinAxeVersion(t *testing.T) {
	p, _ := New(Config{ServiceName: "s"})
	assert.NotEmpty(t, p.MinAxeVersion())
}

// ── Shutdown ──────────────────────────────────────────────────────────────────

func TestShutdown_AfterRegister(t *testing.T) {
	p, _ := New(Config{ServiceName: "svc"})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
	require.NoError(t, p.Shutdown(t.Context()))
}

func TestShutdown_BeforeRegister_NoError(t *testing.T) {
	// Shutdown without Register must not panic (provider is nil).
	p, _ := New(Config{ServiceName: "svc"})
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Middleware: trace propagation ─────────────────────────────────────────────

func TestMiddleware_InjectsTraceContext(t *testing.T) {
	p, _ := New(Config{ServiceName: "test-svc"})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	// Register a route that reads the trace ID from context.
	var capturedTraceID string
	app.Router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		capturedTraceID = span.SpanContext().TraceID().String()
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(app.Router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// With OTel middleware active, the span context must be populated.
	assert.NotEqual(t, "00000000000000000000000000000000", capturedTraceID,
		"trace ID must be non-zero when otel middleware is active")
}

func TestMiddleware_PropagatesIncomingTrace(t *testing.T) {
	p, _ := New(Config{ServiceName: "test-svc"})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	var capturedTraceID string
	app.Router.Get("/trace", func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		capturedTraceID = span.SpanContext().TraceID().String()
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(app.Router)
	defer srv.Close()

	// Inject a W3C traceparent header.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/trace", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// The trace ID from the header must be propagated into the span context.
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", capturedTraceID,
		"incoming traceparent header must set the trace ID in the span context")
}

// ── StartSpan helper ──────────────────────────────────────────────────────────

func TestStartSpan_ReturnsSpan(t *testing.T) {
	p, _ := New(Config{ServiceName: "svc"})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	ctx, span := StartSpan(t.Context(), "test-operation")
	defer span.End()

	assert.NotNil(t, span)
	assert.True(t, span.SpanContext().IsValid(), "span must have valid context")
	assert.NotNil(t, ctx)
}

// ── ServiceKey ────────────────────────────────────────────────────────────────

func TestServiceKey_IsOtel(t *testing.T) {
	p, _ := New(Config{ServiceName: "svc"})
	assert.Equal(t, p.Name(), ServiceKey)
}
