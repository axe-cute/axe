package storage

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricsUploadBytes = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "axe",
		Subsystem: "storage",
		Name:      "upload_bytes_total",
		Help:      "Total bytes uploaded via the storage plugin.",
	})

	metricsUploadErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "axe",
		Subsystem: "storage",
		Name:      "upload_errors_total",
		Help:      "Total upload errors by reason.",
	}, []string{"reason"})

	metricsOps = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "axe",
		Subsystem: "storage",
		Name:      "operations_total",
		Help:      "Total storage operations by operation and status.",
	}, []string{"operation", "status"})
)
