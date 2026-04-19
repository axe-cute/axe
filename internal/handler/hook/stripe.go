package hook

import (
	"context"

	"github.com/axe-cute/axe/pkg/logger"
	"github.com/axe-cute/axe/pkg/plugin/events"
)

// RegisterStripeHooks sets up business logic handlers for Stripe webhooks.
func RegisterStripeHooks(bus events.Bus) {
	bus.Subscribe(events.TopicPaymentSucceeded, func(ctx context.Context, e events.Event) error {
		log := logger.FromCtx(ctx)
		log.Info("payment succeeded",
			"trace_id", e.Meta.TraceID,
		)
		// IMPLEMENT: Add your business logic here. Example:
		// orderID := e.Payload["metadata"].(map[string]any)["order_id"]
		return nil
	})

	bus.Subscribe(events.TopicPaymentFailed, func(ctx context.Context, e events.Event) error {
		log := logger.FromCtx(ctx)
		log.Warn("payment failed",
			"trace_id", e.Meta.TraceID)
		return nil
	})
}
