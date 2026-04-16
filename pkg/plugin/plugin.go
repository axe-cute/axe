// Package plugin provides the axe plugin system.
//
// A Plugin integrates third-party or optional functionality into an axe app
// without modifying core framework code. Plugins receive an [App] host during
// registration, which exposes the router, database, cache, logger, and a typed
// service locator for cross-plugin communication.
//
// Lifecycle:
//
//	app.Use(myPlugin)        // register (before Start)
//	app.Start(ctx)           // calls Register on each plugin in FIFO order
//	...app running...
//	app.Shutdown(ctx)        // calls Shutdown in LIFO order
//
// Typed Service Locator:
//
//	plugin.Provide[MyService](app, "my-service", svc)   // in Register()
//	svc := plugin.MustResolve[MyService](app, "my-service")
package plugin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	ent "github.com/axe-cute/axe/ent"
	"github.com/axe-cute/axe/config"
	"github.com/axe-cute/axe/pkg/cache"
	"github.com/axe-cute/axe/pkg/ws"
	"github.com/go-chi/chi/v5"
)

// Plugin is the contract every axe plugin must satisfy.
type Plugin interface {
	// Name returns a unique, human-readable identifier (e.g. "storage", "email").
	Name() string

	// Register is called once during app startup. The plugin receives the App
	// host and may register routes, provide services, etc.
	Register(ctx context.Context, app *App) error

	// Shutdown is called during graceful shutdown with a timeout context.
	Shutdown(ctx context.Context) error
}

// AppConfig is the configuration passed to [NewApp].
type AppConfig struct {
	Router    chi.Router
	Config    *config.Config
	Logger    *slog.Logger
	DB        *sql.DB
	EntClient *ent.Client
	Cache     *cache.Client // may be nil if Redis is unavailable
	Hub       *ws.Hub       // may be nil if WebSocket is disabled
}

// App is the host that plugins receive during registration.
// It exposes axe infrastructure without leaking internals.
type App struct {
	// Router is the Chi router for registering HTTP routes.
	Router chi.Router
	// Config is the application configuration.
	Config *config.Config
	// Logger is the structured logger.
	Logger *slog.Logger
	// DB is the shared *sql.DB connection pool.
	DB *sql.DB
	// EntClient is the Ent ORM client.
	EntClient *ent.Client
	// Cache is the Redis cache client (may be nil).
	Cache *cache.Client
	// Hub is the WebSocket hub (may be nil).
	Hub *ws.Hub

	mu       sync.RWMutex
	plugins  []Plugin
	names    map[string]bool
	services map[string]any
	started  bool
}

// NewApp creates a new plugin host from the given infrastructure.
func NewApp(cfg AppConfig) *App {
	return &App{
		Router:    cfg.Router,
		Config:    cfg.Config,
		Logger:    cfg.Logger,
		DB:        cfg.DB,
		EntClient: cfg.EntClient,
		Cache:     cfg.Cache,
		Hub:       cfg.Hub,
		names:     make(map[string]bool),
		services:  make(map[string]any),
	}
}

// ── Typed Service Locator ─────────────────────────────────────────────────────

// Provide registers a typed service by key.
// Panics if the key is already registered (fail-fast at startup).
//
//	plugin.Provide[storage.Store](app, storage.ServiceKey, store)
func Provide[T any](app *App, key string, svc T) {
	app.mu.Lock()
	defer app.mu.Unlock()
	if _, exists := app.services[key]; exists {
		panic(fmt.Sprintf("plugin: service %q already provided", key))
	}
	app.services[key] = svc
}

// Resolve retrieves a typed service by key.
// Returns (zero, false) if the key is not found or the type does not match.
//
//	store, ok := plugin.Resolve[storage.Store](app, storage.ServiceKey)
func Resolve[T any](app *App, key string) (T, bool) {
	app.mu.RLock()
	defer app.mu.RUnlock()
	v, ok := app.services[key]
	if !ok {
		var zero T
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return typed, true
}

// MustResolve retrieves a typed service by key or panics.
// Use for required dependencies that must be present.
//
//	store := plugin.MustResolve[storage.Store](app, storage.ServiceKey)
func MustResolve[T any](app *App, key string) T {
	svc, ok := Resolve[T](app, key)
	if !ok {
		panic(fmt.Sprintf("plugin: service %q not found or type mismatch", key))
	}
	return svc
}

// ── Plugin Lifecycle ──────────────────────────────────────────────────────────

// Use registers a plugin. Must be called before [App.Start].
// Returns an error if a plugin with the same Name() is already registered.
func (a *App) Use(p Plugin) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		return fmt.Errorf("plugin: cannot Use(%q) after Start()", p.Name())
	}
	if a.names[p.Name()] {
		return fmt.Errorf("plugin: duplicate plugin name %q", p.Name())
	}

	a.plugins = append(a.plugins, p)
	a.names[p.Name()] = true
	return nil
}

// Start calls Register on all plugins in FIFO order.
// If plugin N fails, plugins 0..N-1 are Shutdown in reverse order.
func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()

	for i, p := range a.plugins {
		if err := p.Register(ctx, a); err != nil {
			// Rollback: shutdown already-registered plugins in reverse
			a.Logger.Error("plugin registration failed — rolling back",
				"plugin", p.Name(), "error", err)
			for j := i - 1; j >= 0; j-- {
				if shutErr := a.plugins[j].Shutdown(ctx); shutErr != nil {
					a.Logger.Error("plugin rollback shutdown error",
						"plugin", a.plugins[j].Name(), "error", shutErr)
				}
			}
			return fmt.Errorf("plugin: %s: Register: %w", p.Name(), err)
		}
		a.Logger.Info("plugin registered", "plugin", p.Name())
	}

	return nil
}

// Shutdown calls Shutdown on all registered plugins in LIFO order.
// Collects all errors (does not stop at first failure).
func (a *App) Shutdown(ctx context.Context) error {
	var errs []error
	for i := len(a.plugins) - 1; i >= 0; i-- {
		p := a.plugins[i]
		if err := p.Shutdown(ctx); err != nil {
			a.Logger.Error("plugin shutdown error", "plugin", p.Name(), "error", err)
			errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
		} else {
			a.Logger.Info("plugin shutdown", "plugin", p.Name())
		}
	}
	return errors.Join(errs...)
}

// Plugins returns the names of all registered plugins (for logging/debug).
func (a *App) Plugins() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := make([]string, len(a.plugins))
	for i, p := range a.plugins {
		names[i] = p.Name()
	}
	return names
}
