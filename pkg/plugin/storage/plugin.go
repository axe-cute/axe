package storage

import (
	"context"
	"log/slog"

	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/pkg/jwtauth"
	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/go-chi/chi/v5"
)

// Plugin implements [plugin.Plugin] for file storage.
type Plugin struct {
	cfg       Config
	store     Store
	log       *slog.Logger
	jwtSvc    *jwtauth.Service
	blocklist middleware.Blocklist // may be nil
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
//
// Auth model:
//   - Write routes (POST upload, DELETE) ALWAYS require JWT authentication.
//   - Read routes (GET serve) are public by default.
//   - Set Config.RequireAuth=true to also protect reads (private/internal files).
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	p.log = app.Logger.With("plugin", "storage")

	store, err := NewFSStore(p.cfg)
	if err != nil {
		return err
	}
	p.store = store

	// Provide store to typed service locator so other plugins can resolve it.
	plugin.Provide[Store](app, ServiceKey, p.store)

	// Build JWT middleware for auth-protected routes.
	p.jwtSvc = jwtauth.New(
		app.Config.JWTSecret,
		app.Config.AccessTokenTTL(),
		app.Config.RefreshTokenTTL(),
	)
	if app.Cache != nil {
		p.blocklist = app.Cache
	}
	authMiddleware := middleware.JWTAuth(p.jwtSvc, p.blocklist)

	// Register HTTP routes
	m := newMetrics(p.cfg.Backend)
	h := &handler{
		store:   p.store,
		cfg:     p.cfg,
		log:     p.log,
		metrics: m,
	}

	app.Router.Route(p.cfg.URLPrefix, func(r chi.Router) {
		// Read (serve): public by default, auth-protected when RequireAuth=true
		if p.cfg.RequireAuth {
			r.With(authMiddleware).Get("/*", h.handleServe)
		} else {
			r.Get("/*", h.handleServe)
		}

		// Write (upload + delete): ALWAYS require JWT — secure by design
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware)
			r.Post("/", h.handleUpload)
			r.Delete("/*", h.handleDelete)
		})
	})

	p.log.Info("storage plugin registered",
		"backend", p.cfg.Backend,
		"mount_path", p.cfg.MountPath,
		"max_file_size", p.cfg.MaxFileSize,
		"url_prefix", p.cfg.URLPrefix,
		"require_auth_reads", p.cfg.RequireAuth,
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
