// Package ws provides WebSocket hub, client, room, and pub/sub adapter
// primitives for the axe framework. It ships with built-in Prometheus metrics
// and supports both in-memory (single-instance) and Redis (multi-instance)
// backends via the Adapter interface.
package ws

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ── Prometheus metrics ────────────────────────────────────────────────────────

var (
	// wsActiveConnections tracks the number of currently open WebSocket connections.
	wsActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "axe",
		Subsystem: "ws",
		Name:      "active_connections",
		Help:      "Number of currently active WebSocket connections.",
	})

	// wsMessagesTotal counts total messages routed by the hub.
	// label direction = "inbound" | "outbound"
	wsMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "axe",
		Subsystem: "ws",
		Name:      "messages_total",
		Help:      "Total number of WebSocket messages handled by the hub.",
	}, []string{"direction"})

	// wsRoomsActive tracks the total number of live rooms.
	wsRoomsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "axe",
		Subsystem: "ws",
		Name:      "rooms_active",
		Help:      "Number of currently active WebSocket rooms.",
	})

	// wsConnectRejectedTotal counts WebSocket upgrade failures.
	wsConnectRejectedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "axe",
		Subsystem: "ws",
		Name:      "connect_rejected_total",
		Help:      "Total number of rejected WebSocket upgrade attempts.",
	})
)
