package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test Helpers ──────────────────────────────────────────────────────────────

// mockPlugin is a test double that records lifecycle calls.
type mockPlugin struct {
	name        string
	registerErr error
	shutdownErr error

	mu             sync.Mutex
	registered     bool
	shutdown       bool
	registerOrder  int
	shutdownOrder  int
}

func (m *mockPlugin) Name() string { return m.name }

func (m *mockPlugin) Register(_ context.Context, _ *App) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registered = true
	if m.registerErr != nil {
		return m.registerErr
	}
	return nil
}

func (m *mockPlugin) Shutdown(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdown = true
	if m.shutdownErr != nil {
		return m.shutdownErr
	}
	return nil
}

func newTestApp() *App {
	return NewApp(AppConfig{
		Logger: slog.Default(),
	})
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestPluginRegistrationOrder(t *testing.T) {
	app := newTestApp()

	// Use a mutex-protected slice since wave-based startup runs goroutines.
	var (mu sync.Mutex; order []string)
	makePlugin := func(name string) Plugin {
		return &orderPlugin{name: name, order: &order, mu: &mu}
	}
	p1 := makePlugin("first")
	p2 := makePlugin("second")
	p3 := makePlugin("third")

	require.NoError(t, app.Use(p1))
	require.NoError(t, app.Use(p2))
	require.NoError(t, app.Use(p3))

	// Start — all in Wave 0 (no deps), run concurrently → order not guaranteed
	err := app.Start(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"first:register", "second:register", "third:register"}, order,
		"all 3 plugins must register, but parallel order is non-deterministic")

	// Shutdown → LIFO (sequential, strictly ordered)
	mu.Lock()
	order = nil
	mu.Unlock()
	err = app.Shutdown(context.Background())
	require.NoError(t, err)
	assert.Equal(t,
		[]string{"third:shutdown", "second:shutdown", "first:shutdown"}, order,
		"shutdown must be LIFO")
}

func TestPluginStartError(t *testing.T) {
	app := newTestApp()

	p1 := &mockPlugin{name: "good"}
	p2 := &mockPlugin{name: "bad", registerErr: fmt.Errorf("boom")}
	// p1, p2, p3 have no deps → same wave → run concurrently
	// p3 may or may not be registered before p2's error is collected.
	p3 := &mockPlugin{name: "also-good"}

	require.NoError(t, app.Use(p1))
	require.NoError(t, app.Use(p2))
	require.NoError(t, app.Use(p3))

	err := app.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad")
	assert.Contains(t, err.Error(), "boom")

	// p1 ran (was in same wave) — its registered flag was set.
	assert.True(t, p1.registered, "p1 should have been attempted in the wave")
	// p2 failed registration.
	assert.True(t, p2.registered, "p2.Register was called")
	// After wave failure: rollback shuts down all completed waves (wave 0 failed,
	// no prior waves → nothing to roll back in this case).
	// p1 and p3 may have registered before the error was collected; they are NOT
	// shut down individually (only completed prior waves are rolled back).
}

func TestProvideResolve(t *testing.T) {
	app := newTestApp()

	type MyService struct{ Value string }
	svc := &MyService{Value: "hello"}

	Provide[*MyService](app, "my-svc", svc)

	got, ok := Resolve[*MyService](app, "my-svc")
	require.True(t, ok)
	assert.Equal(t, "hello", got.Value)
}

func TestProvideResolveInterface(t *testing.T) {
	app := newTestApp()

	// Provide a concrete fmt.Stringer, resolve as interface
	var svc fmt.Stringer = mockStringer("hello")
	Provide[fmt.Stringer](app, "greeter", svc)

	got, ok := Resolve[fmt.Stringer](app, "greeter")
	require.True(t, ok)
	assert.Equal(t, "hello", got.String())
}

type mockStringer string

func (m mockStringer) String() string { return string(m) }

func TestProvideResolveTypeMismatch(t *testing.T) {
	app := newTestApp()

	Provide[string](app, "name", "alice")

	// Try to resolve as int → should fail
	_, ok := Resolve[int](app, "name")
	assert.False(t, ok, "type mismatch should return false")
}

func TestProvideResolveNotFound(t *testing.T) {
	app := newTestApp()

	_, ok := Resolve[string](app, "nonexistent")
	assert.False(t, ok, "missing key should return false")
}

func TestProvideDuplicate(t *testing.T) {
	app := newTestApp()

	Provide[string](app, "dup-key", "first")

	assert.Panics(t, func() {
		Provide[string](app, "dup-key", "second")
	}, "duplicate Provide should panic")
}

func TestMustResolvePanics(t *testing.T) {
	app := newTestApp()

	assert.Panics(t, func() {
		MustResolve[string](app, "missing")
	}, "MustResolve on missing key should panic")
}

func TestMustResolveSuccess(t *testing.T) {
	app := newTestApp()
	Provide[int](app, "port", 8080)

	got := MustResolve[int](app, "port")
	assert.Equal(t, 8080, got)
}

func TestDuplicatePluginName(t *testing.T) {
	app := newTestApp()

	p1 := &mockPlugin{name: "storage"}
	p2 := &mockPlugin{name: "storage"}

	require.NoError(t, app.Use(p1))
	err := app.Use(p2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate plugin name")
}

func TestUseAfterStart(t *testing.T) {
	app := newTestApp()
	require.NoError(t, app.Start(context.Background()))

	err := app.Use(&mockPlugin{name: "late"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after Start")
}

func TestPluginNames(t *testing.T) {
	app := newTestApp()
	require.NoError(t, app.Use(&mockPlugin{name: "alpha"}))
	require.NoError(t, app.Use(&mockPlugin{name: "beta"}))

	names := app.Plugins()
	assert.Equal(t, []string{"alpha", "beta"}, names)
}

func TestShutdownCollectsErrors(t *testing.T) {
	app := newTestApp()

	p1 := &mockPlugin{name: "ok"}
	p2 := &mockPlugin{name: "fail1", shutdownErr: fmt.Errorf("err1")}
	p3 := &mockPlugin{name: "fail2", shutdownErr: fmt.Errorf("err2")}

	require.NoError(t, app.Use(p1))
	require.NoError(t, app.Use(p2))
	require.NoError(t, app.Use(p3))
	require.NoError(t, app.Start(context.Background()))

	err := app.Shutdown(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "err1")
	assert.Contains(t, err.Error(), "err2")
}

// ── Order tracking plugin ─────────────────────────────────────────────────────

type orderPlugin struct {
	name  string
	order *[]string
	mu    *sync.Mutex // protects order slice during parallel Register() calls
}

func (p *orderPlugin) Name() string { return p.name }

func (p *orderPlugin) Register(_ context.Context, _ *App) error {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	*p.order = append(*p.order, p.name+":register")
	return nil
}

func (p *orderPlugin) Shutdown(_ context.Context) error {
	// Shutdown is always sequential (LIFO), no lock needed for test assertions.
	*p.order = append(*p.order, p.name+":shutdown")
	return nil
}

// ── Story 8.10 — Dependency Graph (DAG) Tests ─────────────────────────────────

// depPlugin is a mockPlugin that also declares dependencies.
type depPlugin struct {
	mockPlugin
	deps []string
}

func (d *depPlugin) DependsOn() []string { return d.deps }

func TestDAG_MissingDependency(t *testing.T) {
	app := newTestApp()

	// oauth2 requires "auth" — but auth is never added.
	oauth2 := &depPlugin{
		mockPlugin: mockPlugin{name: "oauth2"},
		deps:       []string{"auth"},
	}
	require.NoError(t, app.Use(oauth2))

	err := app.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oauth2")
	assert.Contains(t, err.Error(), "auth")
	assert.Contains(t, err.Error(), "requires")
	// No plugin should have been registered.
	assert.False(t, oauth2.registered)
}

func TestDAG_CircularDependency(t *testing.T) {
	app := newTestApp()

	// A → B → C → A (3-way cycle)
	a := &depPlugin{mockPlugin: mockPlugin{name: "a"}, deps: []string{"c"}}
	b := &depPlugin{mockPlugin: mockPlugin{name: "b"}, deps: []string{"a"}}
	c := &depPlugin{mockPlugin: mockPlugin{name: "c"}, deps: []string{"b"}}

	require.NoError(t, app.Use(a))
	require.NoError(t, app.Use(b))
	require.NoError(t, app.Use(c))

	err := app.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
	// No plugin should have been registered.
	assert.False(t, a.registered)
	assert.False(t, b.registered)
	assert.False(t, c.registered)
}

func TestDAG_LinearChain_Success(t *testing.T) {
	app := newTestApp()

	// auth has no deps; oauth2 depends on auth — valid linear chain.
	auth := &mockPlugin{name: "auth"}
	oauth2 := &depPlugin{
		mockPlugin: mockPlugin{name: "oauth2"},
		deps:       []string{"auth"},
	}

	require.NoError(t, app.Use(auth))
	require.NoError(t, app.Use(oauth2))

	err := app.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, auth.registered)
	assert.True(t, oauth2.registered)
}

func TestDAG_NoDeps_AlwaysValid(t *testing.T) {
	app := newTestApp()

	// Plugins with no DependsOn — always passes validateDAG.
	p1 := &mockPlugin{name: "storage"}
	p2 := &mockPlugin{name: "email"}
	require.NoError(t, app.Use(p1))
	require.NoError(t, app.Use(p2))

	require.NoError(t, app.Start(context.Background()))
	assert.True(t, p1.registered)
	assert.True(t, p2.registered)
}

// ── Story 8.10 — AllPlugins Tests ─────────────────────────────────────────────

func TestAllPlugins(t *testing.T) {
	app := newTestApp()

	p1 := &mockPlugin{name: "alpha"}
	p2 := &mockPlugin{name: "beta"}
	require.NoError(t, app.Use(p1))
	require.NoError(t, app.Use(p2))

	all := app.AllPlugins()
	require.Len(t, all, 2)
	assert.Equal(t, "alpha", all[0].Name())
	assert.Equal(t, "beta", all[1].Name())
}

func TestAllPlugins_IsACopy(t *testing.T) {
	app := newTestApp()
	require.NoError(t, app.Use(&mockPlugin{name: "x"}))

	got := app.AllPlugins()
	// Modifying the returned slice must not affect the internal slice.
	got[0] = &mockPlugin{name: "injected"}
	assert.Equal(t, "x", app.AllPlugins()[0].Name())
}

// ── Story 8.10 — semverAtLeast Tests ─────────────────────────────────────────

func TestSemverAtLeast(t *testing.T) {
	tests := []struct {
		running  string
		required string
		want     bool
	}{
		{"v1.5.0", "v1.0.0", true},
		{"v1.5.0", "v1.5.0", true},
		{"v1.5.0", "v1.6.0", false},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.0", "v2.0.0", false},
		{"v1.5.3", "v1.5.2", true},
		{"v1.5.2", "v1.5.3", false},
		// Unparseable — fail-open.
		{"invalid", "v1.0.0", true},
		{"v1.0.0", "invalid", true},
	}
	for _, tc := range tests {
		t.Run(tc.running+">="+tc.required, func(t *testing.T) {
			got := semverAtLeast(tc.running, tc.required)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ── Story 8.10 — Versioned Interface Tests ────────────────────────────────────

type versionedPlugin struct {
	mockPlugin
	minVersion string
}

func (v *versionedPlugin) MinAxeVersion() string { return v.minVersion }

func TestVersioned_CompatibleVersion(t *testing.T) {
	app := newTestApp()
	// AxeVersion is "v1.0.0"; plugin requires "v1.0.0" — compatible.
	p := &versionedPlugin{
		mockPlugin: mockPlugin{name: "ai"},
		minVersion: "v1.0.0",
	}
	require.NoError(t, app.Use(p))
	require.NoError(t, app.Start(context.Background()))
	assert.True(t, p.registered)
}

func TestVersioned_IncompatibleVersion(t *testing.T) {
	app := newTestApp()
	// AxeVersion is "v1.0.0"; plugin requires "v2.0.0" — incompatible.
	p := &versionedPlugin{
		mockPlugin: mockPlugin{name: "ai"},
		minVersion: "v2.0.0",
	}
	require.NoError(t, app.Use(p))

	err := app.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ai")
	assert.Contains(t, err.Error(), "v2.0.0")
	assert.False(t, p.registered)
}

func TestVersioned_NoImplementation_AlwaysPasses(t *testing.T) {
	app := newTestApp()
	p := &mockPlugin{name: "storage"} // does NOT implement Versioned
	require.NoError(t, app.Use(p))
	require.NoError(t, app.Start(context.Background()))
	assert.True(t, p.registered)
}

