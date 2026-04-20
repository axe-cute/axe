package obs_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/axe-cute/axe/pkg/plugin/obs"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
)

// ── metricName (tested via NewCounter/NewGauge etc.) ─────────────────────────

func TestNewCounter(t *testing.T) {
	c := obs.NewCounter("testctr", "ops_total", "Test counter.")
	assert.NotNil(t, c)
	c.Inc()
	// No panic = pass. Metric name: axe_testctr_ops_total
}

func TestNewCounterVec(t *testing.T) {
	cv := obs.NewCounterVec("testcvec", "events_total", "Test counter vec.", []string{"type"})
	assert.NotNil(t, cv)
	cv.WithLabelValues("click").Inc()
}

func TestNewHistogram(t *testing.T) {
	h := obs.NewHistogram("testhist", "latency_seconds", "Test histogram.")
	assert.NotNil(t, h)
	h.Observe(0.042)
}

func TestNewGauge(t *testing.T) {
	g := obs.NewGauge("testgauge", "connections", "Test gauge.")
	assert.NotNil(t, g)
	g.Set(42)
}

// ── metricName panics ────────────────────────────────────────────────────────

func TestMetricName_PanicOnEmptyPlugin(t *testing.T) {
	assert.Panics(t, func() {
		obs.NewCounter("", "ops_total", "should panic")
	})
}

func TestMetricName_PanicOnEmptyMetric(t *testing.T) {
	assert.Panics(t, func() {
		obs.NewCounter("plugin", "", "should panic")
	})
}

// ── Logger helper ────────────────────────────────────────────────────────────

func TestLogger(t *testing.T) {
	app := plugintest.NewMockApp()
	app.Logger = slog.Default()

	log := obs.Logger(app, "myplugin")
	assert.NotNil(t, log)
	// Should not panic when used.
	log.Info("test message")
}

// ── NewAggregator default timeout ────────────────────────────────────────────

func TestNewAggregator_ZeroTimeout(t *testing.T) {
	app := plugintest.NewMockApp()
	agg := obs.NewAggregator(app, 0) // should use default 5s
	assert.NotNil(t, agg)
}

func TestNewAggregator_NegativeTimeout(t *testing.T) {
	app := plugintest.NewMockApp()
	agg := obs.NewAggregator(app, -1) // should use default 5s
	assert.NotNil(t, agg)
}
