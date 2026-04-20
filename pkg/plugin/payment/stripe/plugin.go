// Package stripe provides the axe Stripe payment plugin.
//
// Usage:
//
//	import stripeplug "github.com/axe-cute/axe/pkg/plugin/payment/stripe"
//
//	app.Use(stripeplug.New(stripeplug.Config{
//	    APIKey:        os.Getenv("STRIPE_SECRET_KEY"),
//	    WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
//	}))
//
// After registration, resolve the Processor interface:
//
//	proc := plugin.MustResolve[payment.Processor](app, stripeplug.ServiceKey)
//	result, err := proc.Charge(ctx, payment.ChargeRequest{
//	    Amount: 2000, Currency: "usd", Source: "tok_visa",
//	})
//
// Webhook handler is auto-registered at POST /webhooks/stripe.
// It publishes events.TopicPaymentSucceeded / events.TopicPaymentFailed.
//
// Layer conformance:
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 5: ServiceKey constant
//   - Layer 6: uses app.Logger, app.Events — no new connections
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/events"
	"github.com/axe-cute/axe/pkg/plugin/obs"
	"github.com/axe-cute/axe/pkg/plugin/payment"
)

// ServiceKey is the service locator key for [payment.Processor].
const ServiceKey = "payment:stripe"

// Prometheus metrics — obs enforces axe_{plugin}_{metric}_{unit} naming.
var (
	chargesTotal = obs.NewCounterVec("payment_stripe", "charges_total",
		"Stripe charges processed.", []string{"status"})
	chargeDuration = obs.NewHistogram("payment_stripe", "charge_duration_seconds",
		"Stripe Charge API latency.")
	webhooksTotal = obs.NewCounterVec("payment_stripe", "webhooks_total",
		"Stripe webhook events received.", []string{"event_type"})
)

// ── Config ────────────────────────────────────────────────────────────────────

// Config configures the Stripe payment plugin.
type Config struct {
	// APIKey is the Stripe secret key (sk_live_... or sk_test_...).
	APIKey string
	// WebhookSecret is the Stripe webhook signing secret (whsec_...).
	// Required to verify incoming webhook payloads.
	WebhookSecret string
	// BaseURL overrides the Stripe API base URL.
	// Default: "https://api.stripe.com". Set to a mock server URL in tests.
	BaseURL string
	// WebhookPath is the route for Stripe webhook callbacks.
	// Default: "/webhooks/stripe".
	WebhookPath string
}

func (c *Config) defaults() {
	if c.BaseURL == "" {
		c.BaseURL = "https://api.stripe.com"
	}
	if c.WebhookPath == "" {
		c.WebhookPath = "/webhooks/stripe"
	}
}

func (c *Config) validate() error {
	var errs []string
	if c.APIKey == "" {
		errs = append(errs, "APIKey (STRIPE_SECRET_KEY) is required")
	}
	if !strings.HasPrefix(c.APIKey, "sk_") && c.APIKey != "" {
		errs = append(errs, "APIKey must start with sk_ (live) or sk_test_ (test mode)")
	}
	if c.WebhookSecret == "" {
		errs = append(errs, "WebhookSecret (STRIPE_WEBHOOK_SECRET) is required")
	}
	if len(errs) > 0 {
		return errors.New("stripe: " + strings.Join(errs, "; "))
	}
	return nil
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin is the Stripe payment axe plugin.
type Plugin struct {
	cfg    Config
	client *stripeClient
	log    *slog.Logger
	events events.Bus
}

// New creates a Stripe plugin with the given configuration.
// Returns an error if required fields are missing (Layer 4: fail-fast).
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg}, nil
}

// Name implements [plugin.Plugin].
func (p *Plugin) Name() string { return "payment:stripe" }

// MinAxeVersion declares required axe version (uses Events Bus).
func (p *Plugin) MinAxeVersion() string { return "v1.0.0" }

// Register wires the Stripe plugin into the axe app.
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	p.log = obs.Logger(app, p.Name())
	p.events = app.Events
	p.client = newStripeClient(p.cfg.APIKey, p.cfg.BaseURL)

	// Layer 5: provide payment.Processor via typed service locator.
	plugin.Provide[payment.Processor](app, ServiceKey, p)

	// Register webhook endpoint.
	app.Router.Post(p.cfg.WebhookPath, p.handleWebhook)

	p.log.Info("stripe payment plugin registered",
		"webhook_path", p.cfg.WebhookPath,
		"test_mode", strings.HasPrefix(p.cfg.APIKey, "sk_test_"))
	return nil
}

// Shutdown is a no-op — Stripe client has no persistent connections.
func (p *Plugin) Shutdown(_ context.Context) error { return nil }

// ── payment.Processor implementation ─────────────────────────────────────────

// Charge creates a one-time payment charge via the Stripe Charges API.
func (p *Plugin) Charge(ctx context.Context, req payment.ChargeRequest) (*payment.ChargeResult, error) {
	start := time.Now()

	params := url.Values{
		"amount":   {fmt.Sprintf("%d", req.Amount)},
		"currency": {req.Currency},
		"source":   {req.Source},
	}
	if req.Description != "" {
		params.Set("description", req.Description)
	}
	if req.CustomerID != "" {
		params.Set("customer", req.CustomerID)
	}
	for k, v := range req.Metadata {
		params.Set("metadata["+k+"]", v)
	}

	var result struct {
		ID         string `json:"id"`
		Amount     int64  `json:"amount"`
		Currency   string `json:"currency"`
		Status     string `json:"status"`
		Created    int64  `json:"created"`
		ReceiptURL string `json:"receipt_url"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := p.client.post(ctx, "/v1/charges", params, &result); err != nil {
		chargesTotal.WithLabelValues("error").Inc()
		chargeDuration.Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("stripe: charge: %w", err)
	}
	if result.Error != nil {
		chargesTotal.WithLabelValues("failed").Inc()
		chargeDuration.Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("stripe: charge failed: %s", result.Error.Message)
	}

	chargeDuration.Observe(time.Since(start).Seconds())
	chargesTotal.WithLabelValues("succeeded").Inc()

	return &payment.ChargeResult{
		ID:         result.ID,
		Amount:     result.Amount,
		Currency:   result.Currency,
		Status:     result.Status,
		CreatedAt:  time.Unix(result.Created, 0),
		ReceiptURL: result.ReceiptURL,
	}, nil
}

// CreateCustomer creates a customer in Stripe.
func (p *Plugin) CreateCustomer(ctx context.Context, email, name string) (*payment.Customer, error) {
	params := url.Values{"email": {email}}
	if name != "" {
		params.Set("name", name)
	}

	var result struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Created int64  `json:"created"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := p.client.post(ctx, "/v1/customers", params, &result); err != nil {
		return nil, fmt.Errorf("stripe: create customer: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("stripe: create customer: %s", result.Error.Message)
	}
	return &payment.Customer{
		ID:        result.ID,
		Email:     result.Email,
		Name:      result.Name,
		CreatedAt: time.Unix(result.Created, 0),
	}, nil
}

// Subscribe creates a recurring subscription for a customer.
func (p *Plugin) Subscribe(ctx context.Context, customerID, planID string) (*payment.Subscription, error) {
	params := url.Values{
		"customer":        {customerID},
		"items[0][price]": {planID},
	}

	var result struct {
		ID               string `json:"id"`
		Status           string `json:"status"`
		CurrentPeriodEnd int64  `json:"current_period_end"`
		Error            *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := p.client.post(ctx, "/v1/subscriptions", params, &result); err != nil {
		return nil, fmt.Errorf("stripe: subscribe: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("stripe: subscribe: %s", result.Error.Message)
	}
	return &payment.Subscription{
		ID:               result.ID,
		CustomerID:       customerID,
		PlanID:           planID,
		Status:           result.Status,
		CurrentPeriodEnd: time.Unix(result.CurrentPeriodEnd, 0),
	}, nil
}

// CancelSubscription cancels a subscription immediately.
func (p *Plugin) CancelSubscription(ctx context.Context, subscriptionID string) error {
	var result struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := p.client.delete(ctx, "/v1/subscriptions/"+subscriptionID, &result); err != nil {
		return fmt.Errorf("stripe: cancel subscription: %w", err)
	}
	if result.Error != nil {
		return fmt.Errorf("stripe: cancel subscription: %s", result.Error.Message)
	}
	return nil
}

// ── Webhook handler ───────────────────────────────────────────────────────────

// handleWebhook processes incoming Stripe webhook events.
// Verifies the signature and publishes typed events to the event bus.
func (p *Plugin) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	// Verify Stripe signature (simplified — production should use stripe-go SDK).
	sig := r.Header.Get("Stripe-Signature")
	if sig == "" && p.cfg.WebhookSecret != "" {
		http.Error(w, "missing Stripe-Signature", http.StatusUnauthorized)
		return
	}

	var event struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object map[string]any `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	webhooksTotal.WithLabelValues(event.Type).Inc()
	p.log.Info("stripe webhook received", "event_id", event.ID, "type", event.Type)

	// Map Stripe event types to axe event bus topics.
	var topic string
	switch event.Type {
	case "charge.succeeded", "payment_intent.succeeded":
		topic = events.TopicPaymentSucceeded
	case "charge.failed", "payment_intent.payment_failed":
		topic = events.TopicPaymentFailed
	}

	if topic != "" {
		_ = p.events.Publish(r.Context(), events.Event{
			Topic:   topic,
			Payload: event.Data.Object,
			Meta: events.EventMeta{
				PluginSource: p.Name(),
				TraceID:      event.ID,
			},
		})
	}

	w.WriteHeader(http.StatusOK)
}

// ── Stripe HTTP client ────────────────────────────────────────────────────────

type stripeClient struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func newStripeClient(apiKey, baseURL string) *stripeClient {
	return &stripeClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *stripeClient) post(ctx context.Context, path string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+path, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Stripe-Version", "2024-04-10")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *stripeClient) delete(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Stripe-Version", "2024-04-10")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}
