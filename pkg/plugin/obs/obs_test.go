package obs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/obs"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
)

// ── ValidateName ──────────────────────────────────────────────────────────────

func TestValidateName_Valid(t *testing.T) {
	cases := []string{
		"axe_email_sent_total",
		"axe_storage_upload_bytes",
		"axe_ratelimit_blocked_total",
		"axe_email_send_duration_seconds",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, obs.ValidateName(name))
		})
	}
}

func TestValidateName_MissingPrefix(t *testing.T) {
	err := obs.ValidateName("email_sent_total")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "axe_")
}

func TestValidateName_TooFewSegments(t *testing.T) {
	err := obs.ValidateName("axe_email_total")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "4 segments")
}

// ── Aggregator + /ready handler ───────────────────────────────────────────────

// healthyPlugin is a test plugin that implements HealthChecker.
type healthyPlugin struct{ name string }

func (p *healthyPlugin) Name() string                                    { return p.name }
func (p *healthyPlugin) Register(_ context.Context, _ *plugin.App) error { return nil }
func (p *healthyPlugin) Shutdown(_ context.Context) error                { return nil }
func (p *healthyPlugin) HealthCheck(_ context.Context) plugin.HealthStatus {
	return plugin.HealthStatus{Plugin: p.name, OK: true, Message: "ok"}
}

// unhealthyPlugin always returns a failing status.
type unhealthyPlugin struct{ name string }

func (p *unhealthyPlugin) Name() string                                    { return p.name }
func (p *unhealthyPlugin) Register(_ context.Context, _ *plugin.App) error { return nil }
func (p *unhealthyPlugin) Shutdown(_ context.Context) error                { return nil }
func (p *unhealthyPlugin) HealthCheck(_ context.Context) plugin.HealthStatus {
	return plugin.HealthStatus{Plugin: p.name, OK: false, Message: "connection refused"}
}

// noHealthPlugin does NOT implement HealthChecker — should be skipped.
type noHealthPlugin struct{ name string }

func (p *noHealthPlugin) Name() string                                    { return p.name }
func (p *noHealthPlugin) Register(_ context.Context, _ *plugin.App) error { return nil }
func (p *noHealthPlugin) Shutdown(_ context.Context) error                { return nil }

func newTestApp(t *testing.T) *plugin.App {
	t.Helper()
	return plugintest.NewMockApp()
}

func TestAggregator_AllHealthy_Returns200(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.Use(&healthyPlugin{name: "storage"}))
	require.NoError(t, app.Use(&healthyPlugin{name: "email"}))
	require.NoError(t, app.Start(t.Context()))

	agg := obs.NewAggregator(app, 5*time.Second)
	status := agg.Check(t.Context())

	assert.True(t, status.OK)
	assert.Len(t, status.Plugins, 2)
	assert.True(t, status.Plugins["storage"].OK)
	assert.True(t, status.Plugins["email"].OK)
}

func TestAggregator_OneUnhealthy_ReturnsFalse(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.Use(&healthyPlugin{name: "email"}))
	require.NoError(t, app.Use(&unhealthyPlugin{name: "storage"}))
	require.NoError(t, app.Start(t.Context()))

	agg := obs.NewAggregator(app, 5*time.Second)
	status := agg.Check(t.Context())

	assert.False(t, status.OK)
	assert.False(t, status.Plugins["storage"].OK)
	assert.True(t, status.Plugins["email"].OK)
}

func TestAggregator_SkipsNonHealthChecker(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.Use(&noHealthPlugin{name: "noop"}))
	require.NoError(t, app.Start(t.Context()))

	agg := obs.NewAggregator(app, 5*time.Second)
	status := agg.Check(t.Context())

	assert.True(t, status.OK, "no unhealthy plugins — should be ok")
	assert.Empty(t, status.Plugins, "non-HealthChecker plugins must not appear")
}

func TestAggregator_Handler_200_WhenHealthy(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.Use(&healthyPlugin{name: "db"}))
	require.NoError(t, app.Start(t.Context()))

	agg := obs.NewAggregator(app, 5*time.Second)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ready", nil)
	agg.Handler()(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body obs.ReadyStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.True(t, body.OK)
}

func TestAggregator_Handler_503_WhenUnhealthy(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.Use(&unhealthyPlugin{name: "db"}))
	require.NoError(t, app.Start(t.Context()))

	agg := obs.NewAggregator(app, 5*time.Second)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ready", nil)
	agg.Handler()(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var body obs.ReadyStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.False(t, body.OK)
	assert.Equal(t, "connection refused", body.Plugins["db"].Message)
}

func TestAggregator_EmptyApp_Returns200(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.Start(t.Context()))

	agg := obs.NewAggregator(app, 5*time.Second)
	status := agg.Check(t.Context())
	assert.True(t, status.OK, "app with no health-checked plugins = healthy")
}
