package storage

import (
	"context"
	"log/slog"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/go-chi/chi/v5"
)

// Plugin implements [plugin.Plugin] for file storage.
type Plugin struct {
	cfg   Config
	store Store
	log   *slog.Logger
}

// New creates a storage plugin with the given configuration.
func New(cfg Config) *Plugin {
	cfg.defaults()
	return &Plugin{cfg: cfg}
}

// Name returns the plugin identifier.
func (p *Plugin) Name() string { return "storage" }

// Register initializes the filesystem store, provides it to the service locator,
// and registers HTTP routes for file upload/download/delete.
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	p.log = app.Logger.With("plugin", "storage")

	store, err := NewFSStore(p.cfg)
	if err != nil {
		return err
	}
	p.store = store

	// Provide store to typed service locator so other plugins can resolve it.
	plugin.Provide[Store](app, ServiceKey, p.store)

	// Register HTTP routes
	h := &handler{
		store:   p.store,
		cfg:     p.cfg,
		log:     p.log,
	}

	app.Router.Route(p.cfg.URLPrefix, func(r chi.Router) {
		r.Post("/", h.handleUpload)
		r.Get("/{key:.*}", h.handleServe)
		r.Delete("/{key:.*}", h.handleDelete)
	})

	p.log.Info("storage plugin registered",
		"backend", p.cfg.Backend,
		"mount_path", p.cfg.MountPath,
		"max_file_size", p.cfg.MaxFileSize,
		"url_prefix", p.cfg.URLPrefix,
	)

	return nil
}

// Shutdown performs graceful cleanup. FSStore has no resources to release.
func (p *Plugin) Shutdown(_ context.Context) error {
	if p.log != nil {
		p.log.Info("storage plugin shutdown")
	}
	return nil
}
