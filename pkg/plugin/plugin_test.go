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

	var order []string
	p1 := &orderPlugin{name: "first", order: &order}
	p2 := &orderPlugin{name: "second", order: &order}
	p3 := &orderPlugin{name: "third", order: &order}

	require.NoError(t, app.Use(p1))
	require.NoError(t, app.Use(p2))
	require.NoError(t, app.Use(p3))

	// Start → FIFO
	err := app.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"first:register", "second:register", "third:register"}, order)

	// Shutdown → LIFO
	order = nil
	err = app.Shutdown(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"third:shutdown", "second:shutdown", "first:shutdown"}, order)
}

func TestPluginStartError(t *testing.T) {
	app := newTestApp()

	p1 := &mockPlugin{name: "good"}
	p2 := &mockPlugin{name: "bad", registerErr: fmt.Errorf("boom")}
	p3 := &mockPlugin{name: "never"}

	require.NoError(t, app.Use(p1))
	require.NoError(t, app.Use(p2))
	require.NoError(t, app.Use(p3))

	err := app.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad")
	assert.Contains(t, err.Error(), "boom")

	// p1 was registered, should be rolled back (shutdown)
	assert.True(t, p1.registered)
	assert.True(t, p1.shutdown, "p1 should be shutdown on rollback")

	// p2 failed registration
	assert.True(t, p2.registered) // Register was called but returned error

	// p3 was never reached
	assert.False(t, p3.registered)
	assert.False(t, p3.shutdown)
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
}

func (p *orderPlugin) Name() string { return p.name }

func (p *orderPlugin) Register(_ context.Context, _ *App) error {
	*p.order = append(*p.order, p.name+":register")
	return nil
}

func (p *orderPlugin) Shutdown(_ context.Context) error {
	*p.order = append(*p.order, p.name+":shutdown")
	return nil
}
