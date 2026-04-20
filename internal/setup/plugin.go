package setup

import (
	"context"
	"fmt"

	"github.com/axe-cute/axe/config"
	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/storage"

	// axe:wire:import
	stripeplug "github.com/axe-cute/axe/pkg/plugin/payment/stripe"
)

// RegisterPlugins loads and registers all configured plugins.
//
// This is the Plugin Leader — it knows how to initialize plugins and nothing else.
// Decoupled from: Routes, Event Hooks, Service wiring.
func RegisterPlugins(_ context.Context, app *plugin.App, cfg *config.Config) error {
	// ── Storage plugin ────────────────────────────────────────────────────
	storagePlug, err := storage.New(storage.Config{
		Backend:     cfg.StorageBackend,
		MountPath:   cfg.StorageMountPath,
		MaxFileSize: cfg.StorageMaxFileSize,
		URLPrefix:   cfg.StorageURLPrefix,
	})
	if err != nil {
		return fmt.Errorf("storage plugin: %w", err)
	}
	if err := app.Use(storagePlug); err != nil {
		return fmt.Errorf("storage plugin: %w", err)
	}

	// axe:wire:plugin

	// ── Stripe Payment ──────────────────────────────────────────────────────
	stripeExt, err := stripeplug.New(stripeplug.Config{
		APIKey:        cfg.StripeSecretKey,
		WebhookSecret: cfg.StripeWebhookSecret,
	})
	if err == nil {
		if err := app.Use(stripeExt); err != nil {
			return fmt.Errorf("stripe plugin: %w", err)
		}
	}

	return nil
}
