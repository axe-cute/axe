package storage

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// storageMetrics holds Prometheus counters scoped to a specific backend.
// Initialized at Register time so the "backend" label (local/juicefs) is baked in.
type storageMetrics struct {
	uploadBytes  prometheus.Counter
	uploadErrors *prometheus.CounterVec
	ops          *prometheus.CounterVec
}

func newMetrics(backend string) *storageMetrics {
	return &storageMetrics{
		uploadBytes: promauto.NewCounter(prometheus.CounterOpts{
			Namespace:   "axe",
			Subsystem:   "storage",
			Name:        "upload_bytes_total",
			Help:        "Total bytes uploaded via the storage plugin.",
			ConstLabels: prometheus.Labels{"backend": backend},
		}),
		uploadErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "axe",
			Subsystem:   "storage",
			Name:        "upload_errors_total",
			Help:        "Total upload errors by reason.",
			ConstLabels: prometheus.Labels{"backend": backend},
		}, []string{"reason"}),
		ops: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "axe",
			Subsystem:   "storage",
			Name:        "operations_total",
			Help:        "Total storage operations by operation and status.",
			ConstLabels: prometheus.Labels{"backend": backend},
		}, []string{"operation", "status"}),
	}
}
