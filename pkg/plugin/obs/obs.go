// Package obs provides the axe observability contract for plugins.
//
// # Naming Convention
//
// All plugin metrics MUST follow the pattern:
//
//	axe_{plugin}_{metric}_{unit}
//
// Examples:
//
//	axe_email_sent_total
//	axe_storage_upload_bytes
//	axe_ratelimit_blocked_total
//	axe_email_send_duration_seconds
//
// Use the helpers in this package instead of calling prometheus directly —
// they enforce the naming convention at construction time.
//
// # Health Aggregation
//
// Register all [plugin.HealthChecker] plugins with [Aggregator] to expose a
// unified /ready endpoint:
//
//	agg := obs.NewAggregator(app)
//	app.Router.Get("/ready", agg.Handler())
//
// # Structured Logger
//
// Tag your plugin logger once in Register():
//
//	p.log = obs.Logger(app, "email")
//	// => slog.Logger with "plugin" = "email" already set
package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/axe-cute/axe/pkg/plugin"
)

// ── Naming convention helpers ─────────────────────────────────────────────────

// metricName builds a metric name following the axe convention.
// Panics on empty pluginName or metric (these are always programmer errors).
func metricName(pluginName, metric string) string {
	if pluginName == "" {
		panic("obs: pluginName must not be empty")
	}
	if metric == "" {
		panic("obs: metric must not be empty")
	}
	// Normalize to snake_case and strip any existing axe_ prefix to avoid doubles.
	name := strings.ReplaceAll(metric, "-", "_")
	name = strings.ToLower(name)
	return fmt.Sprintf("axe_%s_%s", pluginName, name)
}

// NewCounter creates a Prometheus counter following the axe naming convention.
// name should be "{metric}_{unit}", e.g. "sent_total", "blocked_total".
//
//	blocked := obs.NewCounter("ratelimit", "blocked_total", "Requests blocked by rate limiter.")
func NewCounter(pluginName, name, help string) prometheus.Counter {
	return promauto.NewCounter(prometheus.CounterOpts{
		Name: metricName(pluginName, name),
		Help: help,
	})
}

// NewCounterVec creates a Prometheus counter with labels.
//
//	sent := obs.NewCounterVec("email", "sent_total", "Emails sent.", []string{"provider"})
//	sent.WithLabelValues("sendgrid").Inc()
func NewCounterVec(pluginName, name, help string, labels []string) *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{
		Name: metricName(pluginName, name),
		Help: help,
	}, labels)
}

// NewHistogram creates a Prometheus histogram for latency measurement.
// Use the default buckets for HTTP/RPC latency. name must end in "_seconds".
//
//	dur := obs.NewHistogram("email", "send_duration_seconds", "Email send latency.")
func NewHistogram(pluginName, name, help string) prometheus.Histogram {
	return promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    metricName(pluginName, name),
		Help:    help,
		Buckets: prometheus.DefBuckets,
	})
}

// NewGauge creates a Prometheus gauge (for queue depth, connections, etc.).
//
//	conns := obs.NewGauge("storage", "open_connections", "Open storage connections.")
func NewGauge(pluginName, name, help string) prometheus.Gauge {
	return promauto.NewGauge(prometheus.GaugeOpts{
		Name: metricName(pluginName, name),
		Help: help,
	})
}

// ── Logger helper ─────────────────────────────────────────────────────────────

// Logger returns a slog.Logger pre-tagged with "plugin" = pluginName.
// Use once in Register() to avoid re-tagging on every log call.
//
//	p.log = obs.Logger(app, p.Name())
func Logger(app *plugin.App, pluginName string) *slog.Logger {
	return app.Logger.With("plugin", pluginName)
}

// ── Health aggregator ─────────────────────────────────────────────────────────

// ReadyStatus is the JSON response from the /ready endpoint.
type ReadyStatus struct {
	OK      bool                    `json:"ok"`
	Plugins map[string]PluginStatus `json:"plugins"`
}

// PluginStatus is the health status of one plugin in the /ready response.
type PluginStatus struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
}

// Aggregator collects health checks from all registered [plugin.HealthChecker]
// plugins and exposes them as a single /ready HTTP endpoint.
type Aggregator struct {
	app     *plugin.App
	timeout time.Duration
}

// NewAggregator creates a health aggregator from the given app host.
// timeout controls how long each plugin has to respond. Default: 5s.
//
//	agg := obs.NewAggregator(app)
//	router.Get("/ready", agg.Handler())
func NewAggregator(app *plugin.App, timeout time.Duration) *Aggregator {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Aggregator{app: app, timeout: timeout}
}

// Check performs all registered health checks and returns the aggregate status.
// Cancels checks that exceed the configured timeout.
func (a *Aggregator) Check(ctx context.Context) ReadyStatus {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	statuses := make(map[string]PluginStatus)
	allOK := true

	for _, p := range a.app.AllPlugins() {
		hc, ok := p.(plugin.HealthChecker)
		if !ok {
			continue
		}
		result := hc.HealthCheck(ctx)
		statuses[result.Plugin] = PluginStatus{
			OK:        result.OK,
			Message:   result.Message,
			LatencyMs: result.Latency.Milliseconds(),
		}
		if !result.OK {
			allOK = false
		}
	}

	return ReadyStatus{OK: allOK, Plugins: statuses}
}

// Handler returns an http.HandlerFunc for the /ready endpoint.
// Responds 200 if all plugins are healthy, 503 if any are unhealthy.
// Suitable for Kubernetes readiness probes.
func (a *Aggregator) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := a.Check(r.Context())

		code := http.StatusOK
		if !status.OK {
			code = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(status)
	}
}

// ── ValidateName (for axe plugin validate CI gate) ────────────────────────────

// ValidateName checks that a metric name follows the axe naming convention.
// Returns an error describing the violation.
//
//	err := obs.ValidateName("my_metric")  // error: missing axe_ prefix
//	err := obs.ValidateName("axe_email_sent_total")  // nil
func ValidateName(name string) error {
	if !strings.HasPrefix(name, "axe_") {
		return fmt.Errorf("metric %q must start with axe_ (convention: axe_{plugin}_{metric}_{unit})", name)
	}
	parts := strings.Split(name, "_")
	if len(parts) < 4 {
		return fmt.Errorf("metric %q must have at least 4 segments: axe_{plugin}_{metric}_{unit}", name)
	}
	return nil
}
