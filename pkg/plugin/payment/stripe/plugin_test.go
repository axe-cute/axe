package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/events"
	"github.com/axe-cute/axe/pkg/plugin/payment"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_MissingAPIKey(t *testing.T) {
	_, err := New(Config{WebhookSecret: "whsec_test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APIKey")
}

func TestNew_MissingWebhookSecret(t *testing.T) {
	_, err := New(Config{APIKey: "sk_test_abc123"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WebhookSecret")
}

func TestNew_InvalidAPIKeyPrefix(t *testing.T) {
	_, err := New(Config{APIKey: "not_a_stripe_key", WebhookSecret: "whsec_test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sk_")
}

func TestNew_ValidConfig(t *testing.T) {
	p, err := New(Config{APIKey: "sk_test_valid", WebhookSecret: "whsec_test"})
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "payment:stripe", p.Name())
}

func TestNew_DefaultBaseURL(t *testing.T) {
	p, err := New(Config{APIKey: "sk_test_valid", WebhookSecret: "whsec_test"})
	require.NoError(t, err)
	assert.Equal(t, "https://api.stripe.com", p.cfg.BaseURL)
}

func TestNew_DefaultWebhookPath(t *testing.T) {
	p, err := New(Config{APIKey: "sk_test_valid", WebhookSecret: "whsec_test"})
	require.NoError(t, err)
	assert.Equal(t, "/webhooks/stripe", p.cfg.WebhookPath)
}

// ── Plugin lifecycle ──────────────────────────────────────────────────────────

func TestRegister_ProvidesProcessor(t *testing.T) {
	srv := newMockStripeServer(t)
	defer srv.Close()

	p, err := New(Config{
		APIKey:        "sk_test_valid",
		WebhookSecret: "whsec_test",
		BaseURL:       srv.URL,
	})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	proc, ok := plugin.Resolve[payment.Processor](app, ServiceKey)
	require.True(t, ok, "Processor must be provided via service locator")
	assert.NotNil(t, proc)
}

func TestShutdown_NoError(t *testing.T) {
	p, err := New(Config{APIKey: "sk_test_valid", WebhookSecret: "whsec_test"})
	require.NoError(t, err)
	require.NoError(t, p.Shutdown(t.Context()))
}

func TestMinAxeVersion(t *testing.T) {
	p, _ := New(Config{APIKey: "sk_test_valid", WebhookSecret: "whsec_test"})
	assert.NotEmpty(t, p.MinAxeVersion())
}

// ── Charge (against mock server) ──────────────────────────────────────────────

func TestCharge_Success(t *testing.T) {
	srv := newMockStripeServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	result, err := p.Charge(t.Context(), payment.ChargeRequest{
		Amount:   2000,
		Currency: "usd",
		Source:   "tok_visa",
	})
	require.NoError(t, err)
	assert.Equal(t, "ch_test_123", result.ID)
	assert.Equal(t, int64(2000), result.Amount)
	assert.Equal(t, "succeeded", result.Status)
}

func TestCharge_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"internal error"}}`)
	}))
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	_, err := p.Charge(t.Context(), payment.ChargeRequest{
		Amount: 100, Currency: "usd", Source: "tok_visa",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stripe")
}

// ── CreateCustomer ────────────────────────────────────────────────────────────

func TestCreateCustomer_Success(t *testing.T) {
	srv := newMockStripeServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	customer, err := p.CreateCustomer(t.Context(), "user@example.com", "Test User")
	require.NoError(t, err)
	assert.Equal(t, "cus_test_123", customer.ID)
	assert.Equal(t, "user@example.com", customer.Email)
}

// ── Webhook handler ───────────────────────────────────────────────────────────

func TestWebhook_PublishesPaymentSucceeded(t *testing.T) {
	srv := newMockStripeServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	// Replace NoopBus with real InProcessBus to capture events.
	bus := events.NewInProcessBus(nil)
	p.events = bus

	received := make(chan events.Event, 1)
	bus.Subscribe(events.TopicPaymentSucceeded, func(_ context.Context, e events.Event) error {
		received <- e
		return nil
	})

	payload := `{"id":"evt_123","type":"charge.succeeded","data":{"object":{"amount":2000}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(payload))
	// No Stripe-Signature — WebhookSecret empty in test config forces skip.
	w := httptest.NewRecorder()

	// Bypass signature check — test plugin has non-empty secret, so we clear it.
	p.cfg.WebhookSecret = ""
	p.handleWebhook(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	select {
	case e := <-received:
		assert.Equal(t, events.TopicPaymentSucceeded, e.Topic)
		assert.Equal(t, "payment:stripe", e.Meta.PluginSource)
	default:
		t.Fatal("no event received from webhook handler")
	}
}

func TestWebhook_MissingSignature_Returns401(t *testing.T) {
	srv := newMockStripeServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL) // WebhookSecret is set → requires signature
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
	p.events = events.NewInProcessBus(nil)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe",
		strings.NewReader(`{"id":"x","type":"charge.succeeded","data":{"object":{}}}`))
	// No Stripe-Signature header.
	w := httptest.NewRecorder()
	p.handleWebhook(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestWebhook_UnknownEventType_IsIgnored(t *testing.T) {
	srv := newMockStripeServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	p.cfg.WebhookSecret = "" // skip sig check
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	bus := events.NewInProcessBus(nil)
	p.events = bus
	var received bool
	bus.Subscribe("*", func(_ context.Context, _ events.Event) error {
		received = true
		return nil
	})

	payload := `{"id":"evt_x","type":"customer.created","data":{"object":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(payload))
	w := httptest.NewRecorder()
	p.handleWebhook(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, received, "unknown event types must not publish to the bus")
}

// ── ServiceKey ────────────────────────────────────────────────────────────────

func TestServiceKey_NotEmpty(t *testing.T) {
	assert.NotEmpty(t, ServiceKey)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func mustNewPlugin(t *testing.T, baseURL string) *Plugin {
	t.Helper()
	p, err := New(Config{
		APIKey:        "sk_test_valid",
		WebhookSecret: "whsec_test",
		BaseURL:       baseURL,
	})
	require.NoError(t, err)
	return p
}

// newMockStripeServer creates an httptest.Server that mimics Stripe API responses.
func newMockStripeServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/charges"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": "ch_test_123", "amount": 2000, "currency": "usd",
				"status": "succeeded", "created": 1700000000, "receipt_url": "",
			})
		case strings.HasSuffix(r.URL.Path, "/customers"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": "cus_test_123", "email": "user@example.com",
				"name": "Test User", "created": 1700000000,
			})
		case strings.HasSuffix(r.URL.Path, "/subscriptions"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": "sub_test_123", "status": "active",
				"current_period_end": 1700086400,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":{"message":"not found: %s"}}`, r.URL.Path)
		}
	}))
}
