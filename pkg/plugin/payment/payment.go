// Package payment defines the shared PaymentPlugin interface and common types
// shared by all payment provider implementations (Stripe, PayOS, etc.).
//
// This package itself has zero external dependencies — it is the interface layer.
// Import a specific provider for actual payment processing:
//
//	import "github.com/axe-cute/axe/pkg/plugin/payment/stripe"
//
// Example:
//
//	app.Use(stripe.New(stripe.Config{
//	    APIKey:        os.Getenv("STRIPE_SECRET_KEY"),
//	    WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
//	}))
//
//	svc := plugin.MustResolve[payment.Processor](app, stripe.ServiceKey)
//	result, err := svc.Charge(ctx, payment.ChargeRequest{
//	    Amount:   2000,  // in minor units (cents)
//	    Currency: "usd",
//	    Source:   "tok_visa",
//	})
package payment

import (
	"context"
	"time"
)

// ── Shared interface ──────────────────────────────────────────────────────────

// Processor is the common interface all payment provider plugins expose
// via the service locator. Switch providers without changing business logic.
type Processor interface {
	// Charge creates a one-time payment.
	Charge(ctx context.Context, req ChargeRequest) (*ChargeResult, error)
	// CreateCustomer creates a customer record in the payment provider.
	CreateCustomer(ctx context.Context, email, name string) (*Customer, error)
	// Subscribe creates a recurring subscription for a customer.
	Subscribe(ctx context.Context, customerID, planID string) (*Subscription, error)
	// CancelSubscription cancels an active subscription immediately.
	CancelSubscription(ctx context.Context, subscriptionID string) error
}

// ── Request / response types ──────────────────────────────────────────────────

// ChargeRequest describes a payment charge.
type ChargeRequest struct {
	// Amount is in the currency's smallest unit (cents for USD, đồng for VND).
	Amount   int64
	Currency string // ISO 4217: "usd", "vnd", "eur"
	// Source is the payment method token returned by the frontend SDK.
	Source      string
	Description string
	// CustomerID links this charge to an existing customer (optional).
	CustomerID string
	// Metadata is passed verbatim to the provider for lookup later.
	Metadata map[string]string
}

// ChargeResult is the successful outcome of a Charge call.
type ChargeResult struct {
	ID          string
	Amount      int64
	Currency    string
	Status      string // "succeeded", "pending", "failed"
	CreatedAt   time.Time
	ReceiptURL  string
	ProviderRaw map[string]any // raw provider response for advanced use
}

// Customer is a stored customer profile in the payment provider.
type Customer struct {
	ID        string
	Email     string
	Name      string
	CreatedAt time.Time
}

// Subscription represents a recurring billing subscription.
type Subscription struct {
	ID               string
	CustomerID       string
	PlanID           string
	Status           string // "active", "past_due", "canceled"
	CurrentPeriodEnd time.Time
}
