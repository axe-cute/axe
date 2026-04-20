package stripe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/plugin/payment"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
)

// ── Subscribe ────────────────────────────────────────────────────────────────

func TestSubscribe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/subscriptions", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]any{
			"id": "sub_123", "status": "active", "current_period_end": 1700000000,
		})
	}))
	defer srv.Close()

	p := newTestStripePlugin(t, srv.URL)
	sub, err := p.Subscribe(context.Background(), "cus_111", "price_222")
	require.NoError(t, err)
	assert.Equal(t, "sub_123", sub.ID)
	assert.Equal(t, "active", sub.Status)
	assert.Equal(t, "cus_111", sub.CustomerID)
	assert.Equal(t, "price_222", sub.PlanID)
}

func TestSubscribe_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "customer not found"},
		})
	}))
	defer srv.Close()

	p := newTestStripePlugin(t, srv.URL)
	_, err := p.Subscribe(context.Background(), "bad", "price_1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer not found")
}

// ── CancelSubscription ───────────────────────────────────────────────────────

func TestCancelSubscription_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v1/subscriptions/sub_456", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]any{"id": "sub_456", "status": "canceled"})
	}))
	defer srv.Close()

	p := newTestStripePlugin(t, srv.URL)
	err := p.CancelSubscription(context.Background(), "sub_456")
	require.NoError(t, err)
}

func TestCancelSubscription_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "subscription not found"},
		})
	}))
	defer srv.Close()

	p := newTestStripePlugin(t, srv.URL)
	err := p.CancelSubscription(context.Background(), "sub_bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscription not found")
}

// ── Charge: edge cases ───────────────────────────────────────────────────────

func TestCharge_WithMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		assert.Equal(t, "order-42", r.FormValue("metadata[order_id]"))
		json.NewEncoder(w).Encode(map[string]any{
			"id": "ch_meta", "amount": 1000, "currency": "usd", "status": "succeeded",
			"created": 1700000000, "receipt_url": "https://stripe.com/receipt",
		})
	}))
	defer srv.Close()

	p := newTestStripePlugin(t, srv.URL)
	result, err := p.Charge(context.Background(), payment.ChargeRequest{
		Amount: 1000, Currency: "usd", Source: "tok_visa",
		Metadata: map[string]string{"order_id": "order-42"},
	})
	require.NoError(t, err)
	assert.Equal(t, "ch_meta", result.ID)
}

// ── delete client method ─────────────────────────────────────────────────────

func TestStripeClient_Delete_ConnectionError(t *testing.T) {
	c := newStripeClient("sk_test_key", "http://127.0.0.1:1")
	var out struct{}
	err := c.delete(context.Background(), "/v1/subscriptions/sub_x", &out)
	require.Error(t, err)
}

// ── Helper ───────────────────────────────────────────────────────────────────

func newTestStripePlugin(t *testing.T, baseURL string) *Plugin {
	t.Helper()
	p, err := New(Config{
		APIKey:        "sk_test_fake",
		WebhookSecret: "whsec_fake",
		BaseURL:       baseURL,
	})
	require.NoError(t, err)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(context.Background(), app))
	return p
}
