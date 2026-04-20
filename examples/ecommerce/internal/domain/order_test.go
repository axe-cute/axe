package domain_test

import (
	"testing"

	"github.com/axe-cute/examples-ecommerce/internal/domain"
)

// ── CanTransition ────────────────────────────────────────────────────────────

func TestCanTransition_PendingToConfirmed(t *testing.T) {
	if err := domain.CanTransition(domain.OrderStatusPending, domain.OrderStatusConfirmed); err != nil {
		t.Errorf("expected pending→confirmed to be allowed, got: %v", err)
	}
}

func TestCanTransition_PendingToCancelled(t *testing.T) {
	if err := domain.CanTransition(domain.OrderStatusPending, domain.OrderStatusCancelled); err != nil {
		t.Errorf("expected pending→cancelled to be allowed, got: %v", err)
	}
}

func TestCanTransition_ConfirmedToShipped(t *testing.T) {
	if err := domain.CanTransition(domain.OrderStatusConfirmed, domain.OrderStatusShipped); err != nil {
		t.Errorf("expected confirmed→shipped to be allowed, got: %v", err)
	}
}

func TestCanTransition_ShippedToDelivered(t *testing.T) {
	if err := domain.CanTransition(domain.OrderStatusShipped, domain.OrderStatusDelivered); err != nil {
		t.Errorf("expected shipped→delivered to be allowed, got: %v", err)
	}
}

func TestCanTransition_DeliveredIsFinal(t *testing.T) {
	for _, to := range []string{domain.OrderStatusPending, domain.OrderStatusConfirmed, domain.OrderStatusShipped, domain.OrderStatusCancelled} {
		if err := domain.CanTransition(domain.OrderStatusDelivered, to); err == nil {
			t.Errorf("expected delivered→%s to be rejected, got nil", to)
		}
	}
}

func TestCanTransition_CancelledIsFinal(t *testing.T) {
	for _, to := range []string{domain.OrderStatusPending, domain.OrderStatusConfirmed, domain.OrderStatusShipped, domain.OrderStatusDelivered} {
		if err := domain.CanTransition(domain.OrderStatusCancelled, to); err == nil {
			t.Errorf("expected cancelled→%s to be rejected, got nil", to)
		}
	}
}

func TestCanTransition_InvalidFromStatus(t *testing.T) {
	if err := domain.CanTransition("invalid", domain.OrderStatusConfirmed); err == nil {
		t.Error("expected error for unknown from status")
	}
}

func TestCanTransition_PendingToDelivered_Rejected(t *testing.T) {
	if err := domain.CanTransition(domain.OrderStatusPending, domain.OrderStatusDelivered); err == nil {
		t.Error("expected pending→delivered to be rejected (must go through confirmed+shipped)")
	}
}

// ── ValidOrderStatuses ──────────────────────────────────────────────────────

func TestValidOrderStatuses_AllPresent(t *testing.T) {
	expected := []string{"pending", "confirmed", "shipped", "delivered", "cancelled"}
	for _, s := range expected {
		if !domain.ValidOrderStatuses[s] {
			t.Errorf("expected status %q to be valid", s)
		}
	}
}

func TestValidOrderStatuses_InvalidRejected(t *testing.T) {
	if domain.ValidOrderStatuses["refunded"] {
		t.Error("expected 'refunded' to not be a valid status")
	}
}
