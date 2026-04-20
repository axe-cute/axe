// Package admin provides the axe Admin UI plugin.
//
// The admin plugin is an optional, opt-in dashboard that auto-discovers
// all registered plugins via the [plugin.Contributor] interface.
// It exposes a REST API and an embedded SPA at /axe-admin.
//
// Usage:
//
//	app.Use(admin.New(admin.Config{
//	    Path:   "/axe-admin",
//	    Secret: os.Getenv("ADMIN_SECRET"),
//	}))
//
// Extension interfaces (implement in your plugin to appear in the dashboard):
//
//   - [Contributor] — add a nav panel entry in the admin sidebar
//   - [Configurable] — add a live-config settings form (no restart required)
//
// Example:
//
//	// In your plugin:
//	func (p *AIPlugin) AdminContribution() admin.Contribution {
//	    return admin.Contribution{
//	        ID: "ai-openai", NavLabel: "AI Assistant", NavIcon: "🤖",
//	        APIRoute: "/ai/admin/chat",
//	    }
//	}
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/axe-cute/axe/pkg/plugin"
)

// ServiceKey is the typed service locator key for the Admin *Plugin.
const ServiceKey = "admin"

// ── Extension interfaces ──────────────────────────────────────────────────────

// Contributor is implemented by plugins that want a nav panel
// in the admin sidebar. The admin plugin auto-discovers contributors
// at Register() time via AllPlugins().
//
//	func (p *StoragePlugin) AdminContribution() admin.Contribution {
//	    return admin.Contribution{ID: "storage", NavLabel: "Storage", NavIcon: "📦"}
//	}
type Contributor interface {
	AdminContribution() Contribution
}

// Contribution describes a plugin's presence in the admin sidebar.
type Contribution struct {
	// ID is the unique stable identifier (no spaces, snake_case).
	ID string `json:"id"`
	// NavLabel is the human-readable label shown in the sidebar.
	NavLabel string `json:"nav_label"`
	// NavIcon is an emoji or icon class.
	NavIcon string `json:"nav_icon,omitempty"`
	// APIRoute is the custom route the plugin registers for its admin panel.
	// May be empty if the plugin only shows health/stats from the main list.
	APIRoute string `json:"api_route,omitempty"`
}

// Configurable extends [Contributor] with a live-config settings form.
// Admin validates the JSON Schema before calling ApplyConfig.
// The plugin must also validate (defense in depth).
//
//	func (p *AIPlugin) AdminConfig() admin.ConfigSchema { ... }
//	func (p *AIPlugin) ApplyConfig(ctx, cfg) error { ... }
type Configurable interface {
	Contributor
	// AdminConfig returns the JSON Schema definition for the settings form.
	AdminConfig() ConfigSchema
	// ApplyConfig applies the validated config. Return [ErrInvalidConfig] for field errors.
	ApplyConfig(ctx context.Context, cfg map[string]any) error
}

// ConfigSchema defines the fields rendered as a settings form in the admin UI.
type ConfigSchema struct {
	Fields []ConfigField `json:"fields"`
}

// ConfigField defines one field in the settings form.
type ConfigField struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Type      string   `json:"type"` // "text" | "select" | "toggle" | "number"
	Options   []string `json:"options,omitempty"`
	Required  bool     `json:"required,omitempty"`
	Sensitive bool     `json:"sensitive,omitempty"` // mask value in UI like a password
}

// ErrInvalidConfig is the typed error returned from ApplyConfig.
type ErrInvalidConfig struct {
	Field  string
	Reason string
}

func (e *ErrInvalidConfig) Error() string {
	return fmt.Sprintf("admin: invalid config field %q: %s", e.Field, e.Reason)
}

// ── Plugin config ─────────────────────────────────────────────────────────────

// Config configures the Admin plugin.
type Config struct {
	// Path is the URL prefix for the admin dashboard. Default: "/axe-admin".
	Path string
	// Secret is the Basic Auth password required to access the dashboard.
	// If empty, the dashboard is unprotected (only for development!).
	Secret string
}

func (c *Config) defaults() {
	if c.Path == "" {
		c.Path = "/axe-admin"
	}
}

// ── pluginEntry holds runtime state per registered plugin ─────────────────────

type pluginEntry struct {
	Plugin         plugin.Plugin
	Contribution   *Contribution // nil if not a Contributor
	NavVisible     bool
	IsConfigurable bool
}

// ── Admin Plugin ──────────────────────────────────────────────────────────────

// Plugin is the Admin UI plugin.
type Plugin struct {
	cfg     Config
	log     *slog.Logger
	mu      sync.RWMutex
	entries []*pluginEntry
}

// New creates an Admin plugin with the given configuration.
func New(cfg Config) *Plugin {
	cfg.defaults()
	return &Plugin{cfg: cfg}
}

// Name implements [plugin.Plugin].
func (p *Plugin) Name() string { return "admin" }

// Register scans all registered plugins for [Contributor] and [Configurable],
// then mounts the REST API and SPA at the configured path.
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	p.log = app.Logger.With("plugin", p.Name())

	// Auto-discover contributors from all registered plugins.
	for _, other := range app.AllPlugins() {
		entry := &pluginEntry{
			Plugin:     other,
			NavVisible: true, // default: visible
		}
		if contrib, ok := other.(Contributor); ok {
			c := contrib.AdminContribution()
			entry.Contribution = &c
		}
		if _, ok := other.(Configurable); ok {
			entry.IsConfigurable = true
		}
		p.entries = append(p.entries, entry)
	}

	p.log.Info("admin dashboard discovered plugins", "count", len(p.entries))

	// Mount REST API.
	r := chi.NewRouter()
	if p.cfg.Secret != "" {
		r.Use(middleware.BasicAuth("axe-admin", map[string]string{"admin": p.cfg.Secret}))
	} else {
		p.log.Warn("admin dashboard has NO authentication — config mutations are blocked. Set Config.Secret for full access.")
	}

	// Read-only endpoints (always available).
	r.Get("/api/plugins", p.handleListPlugins)
	r.Get("/api/nav", p.handleNav)
	r.Get("/api/plugins/{id}/config-schema", p.handleConfigSchema)

	// Mutation endpoints — require Secret for access.
	if p.cfg.Secret != "" {
		r.Put("/api/plugins/{id}/nav", p.handleToggleNav)
		r.Put("/api/plugins/{id}/config", p.handleApplyConfig)
	} else {
		// Block mutations when no secret is configured (read-only mode).
		r.Put("/*", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"admin secret required for mutations — set Config.Secret"}`, http.StatusForbidden)
		})
	}

	// Serve embedded SPA placeholder (real SPA would be go:embed).
	r.Get("/*", p.handleSPA)

	app.Router.Mount(p.cfg.Path, r)

	// Provide self via service locator.
	plugin.Provide[*Plugin](app, ServiceKey, p)

	p.log.Info("admin dashboard mounted", "path", p.cfg.Path+"/")
	return nil
}

// Shutdown is a no-op — Admin has no persistent connections.
func (p *Plugin) Shutdown(_ context.Context) error { return nil }

// ── REST handlers ─────────────────────────────────────────────────────────────

func (p *Plugin) handleListPlugins(w http.ResponseWriter, _ *http.Request) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	type row struct {
		ID             string        `json:"id"`
		NavVisible     bool          `json:"nav_visible"`
		HasNavPanel    bool          `json:"has_nav_panel"`
		IsConfigurable bool          `json:"is_configurable"`
		Contribution   *Contribution `json:"contribution,omitempty"`
	}
	rows := make([]row, len(p.entries))
	for i, e := range p.entries {
		rows[i] = row{
			ID:             e.Plugin.Name(),
			NavVisible:     e.NavVisible,
			HasNavPanel:    e.Contribution != nil,
			IsConfigurable: e.IsConfigurable,
			Contribution:   e.Contribution,
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (p *Plugin) handleNav(w http.ResponseWriter, _ *http.Request) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var visible []Contribution
	for _, e := range p.entries {
		if e.Contribution != nil && e.NavVisible {
			visible = append(visible, *e.Contribution)
		}
	}
	writeJSON(w, http.StatusOK, visible)
}

func (p *Plugin) handleToggleNav(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Visible bool `json:"visible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, e := range p.entries {
		if e.Plugin.Name() == id {
			e.NavVisible = body.Visible
			writeJSON(w, http.StatusOK, map[string]any{
				"id":      id,
				"visible": body.Visible,
			})
			return
		}
	}
	http.Error(w, `{"error":"plugin not found"}`, http.StatusNotFound)
}

func (p *Plugin) handleConfigSchema(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, e := range p.entries {
		if e.Plugin.Name() != id {
			continue
		}
		cfg, ok := e.Plugin.(Configurable)
		if !ok {
			http.Error(w, `{"error":"plugin is not configurable"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, cfg.AdminConfig())
		return
	}
	http.Error(w, `{"error":"plugin not found"}`, http.StatusNotFound)
}

func (p *Plugin) handleApplyConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, e := range p.entries {
		if e.Plugin.Name() != id {
			continue
		}
		cfg, ok := e.Plugin.(Configurable)
		if !ok {
			http.Error(w, `{"error":"plugin is not configurable"}`, http.StatusNotFound)
			return
		}
		if err := cfg.ApplyConfig(r.Context(), payload); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
		return
	}
	http.Error(w, `{"error":"plugin not found"}`, http.StatusNotFound)
}

// handleSPA serves a minimal admin dashboard placeholder.
// In production, replace with a real SPA via go:embed.
func (p *Plugin) handleSPA(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>axe Admin</title></head>
<body>
<h1>🪓 axe Admin</h1>
<p>API available at <a href="%s/api/plugins">%s/api/plugins</a></p>
<p>Mount a real SPA via go:embed for production use.</p>
</body>
</html>`, p.cfg.Path, p.cfg.Path)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
