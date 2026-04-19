// Package ws provides a production-ready WebSocket hub.
package ws

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	wsActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "axe", Subsystem: "ws", Name: "active_connections",
		Help: "Number of currently active WebSocket connections.",
	})
	wsMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "axe", Subsystem: "ws", Name: "messages_total",
		Help: "Total number of WebSocket messages handled by the hub.",
	}, []string{"direction"})
	wsRoomsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "axe", Subsystem: "ws", Name: "rooms_active",
		Help: "Number of currently active WebSocket rooms.",
	})
	wsConnectRejectedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "axe", Subsystem: "ws", Name: "connect_rejected_total",
		Help: "Total number of rejected WebSocket upgrade attempts.",
	})
)
