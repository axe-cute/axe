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
//	app.Start(ctx)           // version check → DAG → wave-parallel Register
//	...app running...
//	app.Shutdown(ctx)        // calls Shutdown in LIFO order
//
// Parallel startup (Story 8.11):
//
//	Plugins without dependencies start in Wave 0 concurrently.
//	Plugins depending on Wave N start in Wave N+1.
//	All plugins in a wave run in parallel goroutines.
//
// Event Bus (Story 8.12):
//
//	app.Events.Subscribe("storage.uploaded", handler)
//	app.Events.Publish(ctx, events.Event{Topic: "storage.uploaded", ...})
//
// Typed Service Locator:
//
//	plugin.Provide[MyService](app, "my-service", svc)   // in Register()
//	svc := plugin.MustResolve[MyService](app, "my-service")
//
// Optional interfaces (implement to participate in quality gates):
//
//	Dependent     — declare required plugins; checked before any Register()
//	HealthChecker — contribute to GET /ready health aggregation
//	Versioned     — declare minimum axe version required
package plugin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/axe-cute/axe/config"
	"github.com/axe-cute/axe/pkg/cache"
	"github.com/axe-cute/axe/pkg/plugin/events"
	"github.com/axe-cute/axe/pkg/ws"
	"github.com/go-chi/chi/v5"
)

// AxeVersion is the current framework version.
// Checked against [Versioned.MinAxeVersion] during [App.Start].
// Bump this constant on every release.
const AxeVersion = "v1.0.0"

// ── Core interface ────────────────────────────────────────────────────────────

// Plugin is the contract every axe plugin must satisfy.
type Plugin interface {
	// Name returns a unique, human-readable identifier (e.g. "storage", "email").
	Name() string

	// Register is called once during app startup. The plugin receives the App
	// host and may register routes, provide services, subscribe to events, etc.
	Register(ctx context.Context, app *App) error

	// Shutdown is called during graceful shutdown with a timeout context.
	Shutdown(ctx context.Context) error
}

// ── Optional interfaces ───────────────────────────────────────────────────────

// Dependent is an optional interface for plugins that require other plugins
// to be registered first. Implement this to participate in dependency
// validation. [App.Start] checks all dependencies before calling any Register.
//
//	func (p *OAuth2Plugin) DependsOn() []string { return []string{"auth"} }
type Dependent interface {
	DependsOn() []string
}

// HealthChecker is an optional interface for plugins that can report their
// own health. The /ready endpoint aggregates health from all registered
// HealthChecker plugins.
//
//	func (p *StoragePlugin) HealthCheck(ctx context.Context) HealthStatus { ... }
type HealthChecker interface {
	HealthCheck(ctx context.Context) HealthStatus
}

// HealthStatus is the result of a single plugin health check.
type HealthStatus struct {
	Plugin  string
	OK      bool
	Message string        // "connected", "timeout after 2s"
	Latency time.Duration // optional: round-trip time to external dependency
}

// Versioned is an optional interface for plugins that require a minimum
// axe version. [App.Start] checks this before validateDAG so incompatible
// plugins are rejected at the earliest possible moment.
//
//	func (p *AIPlugin) MinAxeVersion() string { return "v1.5.0" }
type Versioned interface {
	MinAxeVersion() string
}

// ── App host ──────────────────────────────────────────────────────────────────

// AppConfig is the configuration passed to [NewApp].
type AppConfig struct {
	Router    chi.Router
	Config    *config.Config
	Logger    *slog.Logger
	DB        *sql.DB
	Cache     *cache.Client // may be nil if Redis is unavailable
	Hub       *ws.Hub       // may be nil if WebSocket is disabled
}

// App is the host that plugins receive during registration.
// It exposes axe infrastructure without leaking internals.
//
// Layer 6 rule: plugins MUST use these shared fields rather than opening
// their own connections (100 plugins × 10 conns = DB crash).
type App struct {
	// Router is the Chi router for registering HTTP routes.
	Router chi.Router
	// Config is the application configuration.
	Config *config.Config
	// Logger is the structured logger. Tag with plugin name:
	//   p.log = app.Logger.With("plugin", p.Name())
	Logger *slog.Logger
	DB *sql.DB
	// Cache is the Redis cache client (may be nil).
	Cache *cache.Client
	// Hub is the WebSocket hub (may be nil).
	Hub *ws.Hub
	// Events is the plugin event bus for decoupled cross-plugin communication.
	// Subscribe in Register(), Publish anywhere after registration.
	Events events.Bus

	mu       sync.RWMutex
	plugins  []Plugin
	names    map[string]bool
	services map[string]any
	started  bool
}

// NewApp creates a new plugin host from the given infrastructure.
func NewApp(cfg AppConfig) *App {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &App{
		Router:    cfg.Router,
		Config:    cfg.Config,
		Logger:    log,
		DB:        cfg.DB,
		Cache:     cfg.Cache,
		Hub:       cfg.Hub,
		Events:    events.NewInProcessBus(log),
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
// Returns an error if a plugin with the same Name() is already registered,
// or if called after Start.
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

// Start runs pre-flight checks then registers all plugins using wave-based
// parallelism (Story 8.11).
//
// Order of operations:
//  1. Check [Versioned.MinAxeVersion] for plugins that declare it.
//  2. Run [validateDAG]: detect missing dependencies and circular deps.
//  3. Build dependency waves via [buildWaves].
//  4. Each wave starts all its plugins in parallel goroutines.
//     On any failure in a wave, all previously completed waves are shut down
//     in LIFO order (rollback).
func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()

	// Step 1: version compatibility — earliest possible rejection.
	for _, p := range a.plugins {
		v, ok := p.(Versioned)
		if !ok {
			continue
		}
		if !semverAtLeast(AxeVersion, v.MinAxeVersion()) {
			return fmt.Errorf(
				"plugin %q requires axe >= %s, running %s — update axe or use an older plugin version",
				p.Name(), v.MinAxeVersion(), AxeVersion,
			)
		}
	}

	// Step 2: dependency graph validation.
	if err := a.validateDAG(); err != nil {
		return err
	}

	// Step 3: build waves and register in parallel.
	waves := buildWaves(a.plugins)
	registered := make([][]Plugin, 0, len(waves)) // track completed waves for rollback

	for waveIdx, wave := range waves {
		waveStart := time.Now()
		waveNames := make([]string, len(wave))
		for i, p := range wave {
			waveNames[i] = p.Name()
		}
		a.Logger.Info("plugin wave starting", "wave", waveIdx, "plugins", waveNames)

		var wg sync.WaitGroup
		errs := make(chan error, len(wave))

		for _, p := range wave {
			wg.Add(1)
			go func(plug Plugin) {
				defer wg.Done()
				if err := plug.Register(ctx, a); err != nil {
					errs <- fmt.Errorf("%s: %w", plug.Name(), err)
					return
				}
				a.Logger.Info("plugin registered", "plugin", plug.Name())
			}(p)
		}
		wg.Wait()
		close(errs)

		// Collect errors from this wave.
		var waveErrs []error
		for err := range errs {
			waveErrs = append(waveErrs, err)
		}

		if len(waveErrs) > 0 {
			a.Logger.Error("plugin wave failed — rolling back",
				"wave", waveIdx, "errors", len(waveErrs))
			// Rollback all previously fully-completed waves (LIFO).
			for i := len(registered) - 1; i >= 0; i-- {
				for j := len(registered[i]) - 1; j >= 0; j-- {
					p := registered[i][j]
					if shutErr := p.Shutdown(ctx); shutErr != nil {
						a.Logger.Error("rollback shutdown error",
							"plugin", p.Name(), "error", shutErr)
					}
				}
			}
			return fmt.Errorf("plugin: wave %d failed: %w", waveIdx, errors.Join(waveErrs...))
		}

		registered = append(registered, wave)
		a.Logger.Info("plugin wave complete",
			"wave", waveIdx,
			"plugins", len(wave),
			"duration", time.Since(waveStart))
	}

	return nil
}

// buildWaves groups plugins into dependency-depth waves.
// Wave 0 contains plugins with no declared dependencies.
// Wave N contains plugins whose all dependencies are in waves < N.
// Plugins are guaranteed to be registered (wave N) only after wave N-1 fully completes.
func buildWaves(plugins []Plugin) [][]Plugin {
	if len(plugins) == 0 {
		return nil
	}

	// Compute the depth (wave index) of each plugin.
	depth := make(map[string]int, len(plugins))
	for _, p := range plugins {
		depth[p.Name()] = 0
	}

	// Propagate depth: a plugin's wave = max(dependency waves) + 1.
	changed := true
	for changed {
		changed = false
		for _, p := range plugins {
			dep, ok := p.(Dependent)
			if !ok {
				continue
			}
			for _, need := range dep.DependsOn() {
				if d, ok := depth[need]; ok && d+1 > depth[p.Name()] {
					depth[p.Name()] = d + 1
					changed = true
				}
			}
		}
	}

	// Find max depth.
	maxDepth := 0
	for _, d := range depth {
		if d > maxDepth {
			maxDepth = d
		}
	}

	// Group into waves preserving registration order within each wave.
	waves := make([][]Plugin, maxDepth+1)
	for _, p := range plugins {
		d := depth[p.Name()]
		waves[d] = append(waves[d], p)
	}
	return waves
}

// validateDAG checks the plugin dependency graph for:
//   - Missing plugins: plugin A declares DependsOn("B") but B was not added via Use().
//   - Circular dependencies: A→B→C→A detected via Kahn's topological sort.
//
// Called by Start before any Register() is invoked — fail-fast at startup.
func (a *App) validateDAG() error {
	inDegree := make(map[string]int, len(a.plugins))
	edges := make(map[string][]string) // provider → dependant

	for _, p := range a.plugins {
		inDegree[p.Name()] = 0
	}

	for _, p := range a.plugins {
		dep, ok := p.(Dependent)
		if !ok {
			continue
		}
		for _, need := range dep.DependsOn() {
			if !a.names[need] {
				return fmt.Errorf(
					"plugin %q requires %q — add app.Use(%s.New(...)) before app.Start()",
					p.Name(), need, need,
				)
			}
			edges[need] = append(edges[need], p.Name())
			inDegree[p.Name()]++
		}
	}

	// Kahn's algorithm: repeatedly remove nodes with in-degree 0.
	queue := make([]string, 0, len(a.plugins))
	for _, p := range a.plugins {
		if inDegree[p.Name()] == 0 {
			queue = append(queue, p.Name())
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range edges[node] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if visited != len(a.plugins) {
		return fmt.Errorf(
			"plugin: circular dependency detected — check DependsOn() declarations",
		)
	}
	return nil
}

// Shutdown calls Shutdown on all registered plugins in LIFO order.
// Collects all errors rather than stopping at the first failure.
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

// Plugins returns the names of all registered plugins in registration order.
// Intended for logging and diagnostics.
func (a *App) Plugins() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := make([]string, len(a.plugins))
	for i, p := range a.plugins {
		names[i] = p.Name()
	}
	return names
}

// AllPlugins returns all registered Plugin instances in registration order.
// Used by the admin plugin to discover [Contributor] and [HealthChecker]
// implementations via type assertion, without going through the service locator.
func (a *App) AllPlugins() []Plugin {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]Plugin, len(a.plugins))
	copy(result, a.plugins)
	return result
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// semverAtLeast reports whether running >= required.
// Supports "vMAJOR.MINOR.PATCH" format only.
// Returns true if either version is unparseable (fail-open to avoid blocking startup).
func semverAtLeast(running, required string) bool {
	var rMaj, rMin, rPat int
	var qMaj, qMin, qPat int
	if _, err := fmt.Sscanf(running, "v%d.%d.%d", &rMaj, &rMin, &rPat); err != nil {
		return true // unparseable running version — don't block startup
	}
	if _, err := fmt.Sscanf(required, "v%d.%d.%d", &qMaj, &qMin, &qPat); err != nil {
		return true // unparseable requirement — don't block startup
	}
	if rMaj != qMaj {
		return rMaj > qMaj
	}
	if rMin != qMin {
		return rMin > qMin
	}
	return rPat >= qPat
}
