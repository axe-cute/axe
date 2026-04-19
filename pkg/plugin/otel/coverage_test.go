package otel

import (
	"testing"

	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── buildExporter: OTLP branch ───────────────────────────────────────────────

func TestRegister_WithEndpoint(t *testing.T) {
	// OTLP exporter creation succeeds even with unreachable endpoint
	// (failures are deferred to actual export).
	p, err := New(Config{
		ServiceName: "otlp-test",
		Endpoint:    "http://localhost:14318", // non-existent collector
	})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	err = p.Register(t.Context(), app)
	require.NoError(t, err)
	assert.NotNil(t, p.provider)

	// Clean up.
	require.NoError(t, p.Shutdown(t.Context()))
}

func TestRegister_WithEndpointAndHeaders(t *testing.T) {
	p, err := New(Config{
		ServiceName: "header-test",
		Endpoint:    "http://localhost:14318",
		Headers:     map[string]string{"Authorization": "Bearer test-token"},
	})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	err = p.Register(t.Context(), app)
	require.NoError(t, err)

	require.NoError(t, p.Shutdown(t.Context()))
}

func TestRegister_CustomSampleRate(t *testing.T) {
	p, err := New(Config{
		ServiceName: "sample-test",
		SampleRate:  0.5,
	})
	require.NoError(t, err)
	assert.Equal(t, 0.5, p.cfg.SampleRate)

	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
	require.NoError(t, p.Shutdown(t.Context()))
}

func TestConfig_CustomVersion(t *testing.T) {
	p, err := New(Config{
		ServiceName:    "v-test",
		ServiceVersion: "v2.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", p.cfg.ServiceVersion)
}
