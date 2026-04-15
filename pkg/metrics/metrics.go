// Package metrics provides Prometheus instrumentation for the axe HTTP server.
// It exposes a middleware that records request counts, latencies, and error rates.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ── Metric definitions ────────────────────────────────────────────────────────

var (
	// RequestsTotal counts HTTP requests by method, path, and status code.
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "axe",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	// RequestDuration records request durations in seconds (histogram).
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "axe",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latencies in seconds.",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		},
		[]string{"method", "path"},
	)

	// RequestsInFlight tracks currently active HTTP requests.
	RequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "axe",
			Name:      "http_requests_in_flight",
			Help:      "Current number of HTTP requests being served.",
		},
	)

	// DBQueryDuration records database query durations.
	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "axe",
			Name:      "db_query_duration_seconds",
			Help:      "Database query latencies in seconds.",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5},
		},
		[]string{"operation"},
	)

	// WorkerTasksTotal counts Asynq task executions by queue and status.
	WorkerTasksTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "axe",
			Name:      "worker_tasks_total",
			Help:      "Total number of background tasks processed.",
		},
		[]string{"queue", "type", "status"},
	)
)

// ── Middleware ────────────────────────────────────────────────────────────────

// Middleware returns a Chi-compatible middleware that records Prometheus metrics.
// It intercepts the response writer to capture the status code.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		RequestsInFlight.Inc()
		defer RequestsInFlight.Dec()

		// Wrap writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(rw.statusCode)
		path := sanitizePath(r)

		RequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		RequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// Handler returns the Prometheus HTTP handler for the /metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// ── responseWriter ────────────────────────────────────────────────────────────

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// sanitizePath returns a stable path label.
// For route groups, returns the Chi route pattern if available, else the raw path.
// Limits label cardinality by capping unknown paths.
func sanitizePath(r *http.Request) string {
	// Use chi route pattern if set (avoids UUIDs in labels)
	if pattern := r.Pattern; pattern != "" {
		return pattern
	}
	// Fallback for non-chi routes
	path := r.URL.Path
	if len(path) > 64 {
		return path[:64]
	}
	return path
}
