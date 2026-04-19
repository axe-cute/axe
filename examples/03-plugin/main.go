// Example 03: Building a custom axe plugin.
//
// This demonstrates the full plugin lifecycle:
//   - Config validation in New() (fail-fast, Layer 4)
//   - Dependency declaration via DependsOn() (Layer 3)
//   - Service registration via Provide/Resolve (Layer 5)
//   - Health check integration (HealthChecker interface)
//   - Wave-based parallel startup
//
// Run: go run ./examples/03-plugin
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/axe-cute/axe/pkg/plugin"
)

// ═══════════════════════════════════════════════════════════════════════════════
// Plugin 1: GreeterPlugin — provides a greeting service.
// ═══════════════════════════════════════════════════════════════════════════════

const GreeterServiceKey = "greeter.service"

// GreeterConfig holds greeter settings. Validated in New() (fail-fast).
type GreeterConfig struct {
	DefaultName string // name to use when no name is provided
}

type Greeter struct {
	defaultName string
}

func (g *Greeter) Hello(name string) string {
	if name == "" {
		name = g.defaultName
	}
	return fmt.Sprintf("Xin chào, %s! 🇻🇳", name)
}

// GreeterPlugin implements plugin.Plugin.
type GreeterPlugin struct {
	cfg     GreeterConfig
	greeter *Greeter
}

// NewGreeterPlugin validates config and returns the plugin (Layer 4: fail-fast).
func NewGreeterPlugin(cfg GreeterConfig) (*GreeterPlugin, error) {
	if cfg.DefaultName == "" {
		return nil, fmt.Errorf("greeter: DefaultName is required")
	}
	return &GreeterPlugin{cfg: cfg}, nil
}

func (p *GreeterPlugin) Name() string { return "greeter" }

func (p *GreeterPlugin) Register(_ context.Context, app *plugin.App) error {
	p.greeter = &Greeter{defaultName: p.cfg.DefaultName}

	// Expose the service via typed service locator (Layer 5).
	plugin.Provide[*Greeter](app, GreeterServiceKey, p.greeter)

	// Register a route.
	app.Router.Get("/hello", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		fmt.Fprintln(w, p.greeter.Hello(name))
	})

	app.Logger.Info("greeter plugin registered", "default_name", p.cfg.DefaultName)
	return nil
}

func (p *GreeterPlugin) Shutdown(_ context.Context) error {
	fmt.Println("  [greeter] shutdown complete")
	return nil
}

// HealthCheck implements plugin.HealthChecker.
func (p *GreeterPlugin) HealthCheck(_ context.Context) plugin.HealthStatus {
	return plugin.HealthStatus{
		Plugin:  "greeter",
		OK:      p.greeter != nil,
		Message: "greeter service initialized",
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Plugin 2: WelcomePlugin — depends on GreeterPlugin.
// ═══════════════════════════════════════════════════════════════════════════════

type WelcomePlugin struct{}

func NewWelcomePlugin() (*WelcomePlugin, error) {
	return &WelcomePlugin{}, nil
}

func (p *WelcomePlugin) Name() string { return "welcome" }

// DependsOn declares that this plugin requires "greeter" (Layer 3: dependency DAG).
// The framework ensures "greeter" is registered BEFORE "welcome" using wave-based startup.
func (p *WelcomePlugin) DependsOn() []string { return []string{"greeter"} }

func (p *WelcomePlugin) Register(_ context.Context, app *plugin.App) error {
	// Resolve the greeter service from the dependency (Layer 5: typed service locator).
	greeter := plugin.MustResolve[*Greeter](app, GreeterServiceKey)

	app.Router.Get("/welcome", func(w http.ResponseWriter, r *http.Request) {
		msg := greeter.Hello("Developer")
		fmt.Fprintf(w, "Welcome Page\n\n%s\n\nThis message comes from the greeter plugin!", msg)
	})

	app.Logger.Info("welcome plugin registered (depends on greeter)")
	return nil
}

func (p *WelcomePlugin) Shutdown(_ context.Context) error {
	fmt.Println("  [welcome] shutdown complete")
	return nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// Main — wire it up.
// ═══════════════════════════════════════════════════════════════════════════════

func main() {
	// Create the app host.
	app := plugin.NewApp(plugin.AppConfig{
		Router: chi.NewRouter(),
		Logger: slog.Default(),
	})

	// Create plugins with config validation (errors caught at startup, not runtime).
	greeter, err := NewGreeterPlugin(GreeterConfig{DefaultName: "Bạn"})
	if err != nil {
		log.Fatalf("greeter config error: %v", err)
	}

	welcome, err := NewWelcomePlugin()
	if err != nil {
		log.Fatalf("welcome config error: %v", err)
	}

	// Register plugins — ORDER DOESN'T MATTER!
	// axe builds a dependency DAG and starts them in the correct wave order.
	app.Use(welcome) // registered first, but depends on greeter
	app.Use(greeter) // registered second, but started first (wave 0)

	// Start — runs version check → DAG validation → wave-parallel Register.
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.Fatalf("app.Start: %v", err)
	}

	// Check health.
	for _, p := range app.AllPlugins() {
		if hc, ok := p.(plugin.HealthChecker); ok {
			status := hc.HealthCheck(ctx)
			fmt.Printf("  Health: %s → ok=%v (%s)\n", status.Plugin, status.OK, status.Message)
		}
	}

	fmt.Println("\n🪓 Example 03: Plugin System")
	fmt.Println("   GET http://localhost:8080/hello          → greeter (direct)")
	fmt.Println("   GET http://localhost:8080/hello?name=Anh → greeter (custom name)")
	fmt.Println("   GET http://localhost:8080/welcome        → welcome (uses greeter dependency)")
	fmt.Println("\n   Startup order: greeter (wave 0) → welcome (wave 1)")

	log.Fatal(http.ListenAndServe(":8080", app.Router))
}
