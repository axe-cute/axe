// Package metrics provides Prometheus instrumentation middleware for ecommerce.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

// Handler returns the Prometheus metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Middleware records request count and latency for each HTTP handler.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timer := prometheus.NewTimer(httpDuration.WithLabelValues(r.Method, r.URL.Path))
		defer timer.ObserveDuration()

		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		httpRequests.WithLabelValues(r.Method, r.URL.Path, http.StatusText(rw.status)).Inc()
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
