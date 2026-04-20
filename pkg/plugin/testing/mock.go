// Package plugintest provides test utilities for axe plugins.
//
// Use [NewMockApp] to create a lightweight App host in unit tests — no Docker,
// no real database, no network required.
//
//	func TestMyPlugin_Register(t *testing.T) {
//	    app := plugintest.NewMockApp()
//	    p, err := myplugin.New(myplugin.Config{...})
//	    require.NoError(t, err)
//	    require.NoError(t, p.Register(context.Background(), app))
//	    // assert routes, services, etc.
//	}
package plugintest

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/axe-cute/axe/pkg/plugin"
)

// NewMockApp returns a minimal plugin.App suitable for unit testing.
//
// Resources:
//   - Router: real chi.Router (routes can be asserted via httptest)
//   - Logger: slog.Default() (output to stderr, visible in test -v)
//   - DB: nil — tests that need DB should use testcontainers or SQLite
//   - Cache: nil — tests that need cache should mock or use miniredis
//   - Hub: nil — tests that need WebSocket should use ws.NewHub()
//
// This ensures plugin unit tests are fast and hermetic.
func NewMockApp() *plugin.App {
	return plugin.NewApp(plugin.AppConfig{
		Router: chi.NewRouter(),
		Logger: slog.Default(),
		// DB, Cache, Hub intentionally nil — inject only what the plugin under
		// test actually requires to keep tests minimal and fast.
	})
}
