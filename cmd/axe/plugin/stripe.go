package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ── Stripe plugin wiring ──────────────────────────────────────────────────────

func addStripe() error {

	setupPluginPath := filepath.Join("internal", "setup", "plugin.go")
	hookLeaderPath := filepath.Join("internal", "handler", "hook", "hook.go")
	envPath := ".env"
	configPath := filepath.Join("config", "config.go")
	hookDir := filepath.Join("internal", "handler", "hook")
	hookFile := filepath.Join(hookDir, "stripe.go")

	fmt.Println("\n📦 Adding Stripe Payment plugin...")

	// 1. Inject config fields
	configData, _ := os.ReadFile(configPath)
	if strings.Contains(string(configData), "StripeSecretKey") {
		fmt.Printf("   ⏭  config/config.go already has Stripe fields\n")
	} else if err := injectContentAfterMarker(configPath, "// axe:plugin:config", stripeConfigFields(), "StripeSecretKey"); err != nil {
		fmt.Printf("   ⚠️  Could not auto-inject config fields. Add these to config/config.go manually:\n")
		fmt.Println(stripeConfigFields())
	} else {
		fmt.Printf("   ✓ config/config.go (Stripe fields injected)\n")
	}

	// 2. Inject plugin init into setup/plugin.go (Plugin Leader)
	setupData, _ := os.ReadFile(setupPluginPath)
	if strings.Contains(string(setupData), "stripeplug") {
		fmt.Printf("   ⏭  internal/setup/plugin.go already has Stripe\n")
	} else {
		// Inject import
		stripeImport := "\tstripeplug \"github.com/axe-cute/axe/pkg/plugin/payment/stripe\""
		_ = injectContentAfterMarker(setupPluginPath, "// axe:wire:import", stripeImport, "stripeplug")

		// Inject plugin registration
		if err := injectContentAfterMarker(setupPluginPath, "// axe:wire:plugin", stripePluginInit(), "stripeplug.New("); err == nil {
			fmt.Printf("   ✓ internal/setup/plugin.go (Plugin Leader — Stripe registered here)\n")
		} else {
			fmt.Printf("   ⚠️  Could not inject into setup/plugin.go: %v\n", err)
		}
	}

	// 3. Generate hook business logic file
	if err := os.MkdirAll(hookDir, 0755); err == nil {
		if _, err := os.Stat(hookFile); os.IsNotExist(err) {
			_ = os.WriteFile(hookFile, []byte(stripeHooksCode()), 0644)
			fmt.Printf("   ✓ %s (Business logic — write your code here!)\n", hookFile)
		} else {
			fmt.Printf("   ⏭  %s already exists\n", hookFile)
		}
	}

	// 4. Inject hook registration into hook/hook.go (Hook Leader)
	hookData, _ := os.ReadFile(hookLeaderPath)
	if strings.Contains(string(hookData), "RegisterStripeHooks") {
		fmt.Printf("   ⏭  internal/handler/hook/hook.go already has Stripe hooks\n")
	} else {
		hookImport := "\t\"github.com/axe-cute/axe/pkg/plugin/events\""
		_ = injectContentAfterMarker(hookLeaderPath, "// axe:wire:import", hookImport, "events")

		hookCall := "\tRegisterStripeHooks(bus)"
		if err := injectContentAfterMarker(hookLeaderPath, "// axe:wire:hook", hookCall, "RegisterStripeHooks"); err == nil {
			fmt.Printf("   ✓ internal/handler/hook/hook.go (Hook Leader — RegisterStripeHooks wired)\n")
		} else {
			fmt.Printf("   ⚠️  Could not inject into hook.go: %v\n", err)
		}
	}

	// 5. Update .env
	if err := appendToFile(envPath, stripeEnvVars(), "STRIPE_SECRET_KEY"); err == nil {
		fmt.Println("   ✓ .env updated")
	}

	// 6. Print architecture mini-map + getting started
	fmt.Println("\n🏗  Your app architecture:")
	fmt.Println()
	fmt.Println("   cmd/api/main.go (Orchestrator — calls Leaders, contains no logic)")
	fmt.Println("   │")
	fmt.Println("   ├── setup.RegisterPlugins()          ← 🔌 internal/setup/plugin.go")
	fmt.Println("   │      └── Stripe initialized here")
	fmt.Println("   │")
	fmt.Println("   ├── hook.RegisterAll()               ← 🎣 internal/handler/hook/hook.go")
	fmt.Println("   │      └── RegisterStripeHooks(bus)")
	fmt.Println("   │             └── Your logic here     ← ✏️  hook/stripe.go")
	fmt.Println("   │")
	fmt.Println("   └── Routes                           ← 🗺  cmd/api/main.go (router section)")

	fmt.Println("\n🔑 Getting Started:")
	fmt.Println("   1. Get your Secret Key: Stripe Dashboard → Developers → API Keys")
	fmt.Println("      STRIPE_SECRET_KEY=sk_test_...")
	fmt.Println()
	fmt.Println("   2. Local testing:")
	fmt.Println("      make run")
	fmt.Println("      stripe listen --forward-to localhost:8080/webhooks/stripe")
	fmt.Println("      Copy whsec_... into .env")
	fmt.Println()
	fmt.Println("   3. Production:")
	fmt.Println("      Stripe Dashboard → Developers → Webhooks")
	fmt.Println("      Add endpoint: https://api.yourdomain.com/webhooks/stripe")
	fmt.Println("      Copy webhook secret into your production .env")
	fmt.Println()
	fmt.Println("   💡 Write business logic: internal/handler/hook/stripe.go")
	fmt.Println("   💡 See plugin setup:     internal/setup/plugin.go")
	fmt.Println()

	return nil
}

func stripeConfigFields() string {
	return `
	// Stripe Payment
	StripeSecretKey     string ` + "`env:\"STRIPE_SECRET_KEY\"`" + `
	StripeWebhookSecret string ` + "`env:\"STRIPE_WEBHOOK_SECRET\"`" + `
`
}

func stripeEnvVars() string {
	return `
# Stripe Payment Plugin (API Keys)
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
`
}

func stripePluginInit() string {
	return `
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
`
}

func stripeHooksCode() string {
	return `package hook

import (
	"context"

	"github.com/axe-cute/axe/pkg/logger"
	"github.com/axe-cute/axe/pkg/plugin/events"
)

// RegisterStripeHooks sets up business logic handlers for Stripe webhooks.
func RegisterStripeHooks(bus events.Bus) {
	bus.Subscribe(events.TopicPaymentSucceeded, func(ctx context.Context, e events.Event) error {
		log := logger.FromCtx(ctx)
		log.Info("🎉 Payment succeeded", 
			"trace_id", e.Meta.TraceID, 
		)
		// TODO: Add your business logic here. Example:
		// orderID := e.Payload["metadata"].(map[string]any)["order_id"]
		return nil
	})

	bus.Subscribe(events.TopicPaymentFailed, func(ctx context.Context, e events.Event) error {
		log := logger.FromCtx(ctx)
		log.Warn("⚠️ Payment failed", 
			"trace_id", e.Meta.TraceID)
		return nil
	})
}
`
}
