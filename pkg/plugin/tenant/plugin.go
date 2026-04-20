// Package tenant provides lightweight multi-tenancy middleware for axe.
//
// It extracts a tenant identifier from each HTTP request and places it in the
// request context. Downstream handlers and other plugins read it via [FromCtx].
//
// Tenant sources (checked in priority order):
//  1. Header: X-Tenant-ID
//  2. Subdomain: {tenant}.example.com → tenant = "tenant"
//  3. JWT claim: "tenant_id" in the access token
//
// Usage:
//
//	app.Use(tenant.New(tenant.Config{Source: tenant.SourceHeader}))
//
// Reading tenant in a handler:
//
//	t, ok := tenant.FromCtx(r.Context())
//	if !ok { /* no tenant — public route */ }
//
// Per-tenant cache key namespacing:
//
//	key := tenant.CacheKey(ctx, "user:42") // → "acme:user:42"
//
// Layer conformance (Story 8.10):
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 5: ServiceKey for cross-plugin resolution
//   - Layer 6: no self-created connections
package tenant

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/axe-cute/axe/pkg/plugin"
)

// ServiceKey is the typed service locator key for the tenant [Middleware].
const ServiceKey = "tenant"

// Source defines how the tenant ID is extracted from a request.
type Source string

const (
	// SourceHeader reads X-Tenant-ID header.
	SourceHeader Source = "header"
	// SourceSubdomain reads the first subdomain component of the Host header.
	// e.g. "acme.example.com" → tenant = "acme"
	SourceSubdomain Source = "subdomain"
	// SourceJWT reads the "tenant_id" claim from the JWT token in the request context.
	SourceJWT Source = "jwt"
	// SourceAuto tries header, then subdomain, then JWT (in that order).
	SourceAuto Source = "auto"
)

// ctxKey is the context key type to avoid collisions.
type ctxKey struct{}

// ── Config ────────────────────────────────────────────────────────────────────

// Config configures the tenant middleware plugin.
type Config struct {
	// Source selects how tenant ID is extracted. Default: SourceAuto.
	Source Source

	// Required controls whether requests without a tenant ID are rejected (401)
	// or passed through with an empty tenant (useful for public/marketing routes).
	// Default: false — allow requests without a tenant.
	Required bool

	// DefaultTenant is returned when no tenant ID is found and Required is false.
	// Leave empty to pass "" to handlers (they can decide what to do).
	DefaultTenant string
}

func (c *Config) defaults() {
	if c.Source == "" {
		c.Source = SourceAuto
	}
}

func (c *Config) validate() error {
	switch c.Source {
	case SourceHeader, SourceSubdomain, SourceJWT, SourceAuto:
		return nil
	default:
		return fmt.Errorf("tenant: unknown source %q — must be header, subdomain, jwt, or auto", c.Source)
	}
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin implements [plugin.Plugin] for multi-tenancy middleware.
type Plugin struct {
	cfg Config
}

// New creates a tenant plugin with the given configuration.
// Returns an error if config is invalid (Layer 4: fail-fast in New).
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg}, nil
}

// Name returns the plugin identifier.
func (p *Plugin) Name() string { return "tenant" }

// Register mounts the tenant middleware globally on the router.
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	mw := p.middleware()
	app.Router.Use(mw)

	// Provide the middleware function so other plugins can apply it selectively.
	plugin.Provide[Middleware](app, ServiceKey, mw)

	if app.Logger != nil {
		app.Logger.With("plugin", p.Name()).Info("tenant middleware registered",
			"source", string(p.cfg.Source),
			"required", p.cfg.Required,
		)
	}
	return nil
}

// Shutdown is a no-op — middleware has no resources to release.
func (p *Plugin) Shutdown(_ context.Context) error { return nil }

// ── Middleware ────────────────────────────────────────────────────────────────

// Middleware is a chi-compatible HTTP middleware function.
type Middleware func(http.Handler) http.Handler

// middleware returns the configured chi.Middleware.
func (p *Plugin) middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := p.extract(r)

			if id == "" && p.cfg.Required {
				http.Error(w, `{"error":"tenant required"}`, http.StatusUnauthorized)
				return
			}
			if id == "" {
				id = p.cfg.DefaultTenant
			}

			ctx := context.WithValue(r.Context(), ctxKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extract returns the tenant ID from the request using the configured source.
func (p *Plugin) extract(r *http.Request) string {
	switch p.cfg.Source {
	case SourceHeader:
		return fromHeader(r)
	case SourceSubdomain:
		return fromSubdomain(r)
	case SourceJWT:
		return fromJWT(r)
	default: // SourceAuto
		if id := fromHeader(r); id != "" {
			return id
		}
		if id := fromSubdomain(r); id != "" {
			return id
		}
		return fromJWT(r)
	}
}

// ── Extraction helpers ────────────────────────────────────────────────────────

func fromHeader(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
}

func fromSubdomain(r *http.Request) string {
	host := r.Host
	// Strip port if present.
	if i := strings.LastIndex(host, ":"); i != -1 {
		host = host[:i]
	}
	parts := strings.Split(host, ".")
	// Need at least {tenant}.{domain}.{tld} = 3 parts.
	if len(parts) < 3 {
		return ""
	}
	return parts[0]
}

// fromJWT reads the "tenant_id" claim from context (set by jwtauth middleware).
// Returns "" if no claim is found — does not parse the JWT itself.
func fromJWT(r *http.Request) string {
	// The jwtauth middleware stores claims in context under a known key.
	// We use a type assertion to avoid importing jwtauth (Layer 6: no coupling).
	type claimsMap interface {
		Get(string) (interface{}, bool)
	}
	claims, ok := r.Context().Value("jwt_claims").(claimsMap)
	if !ok {
		return ""
	}
	v, ok := claims.Get("tenant_id")
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// ── Public API ────────────────────────────────────────────────────────────────

// FromCtx returns the tenant ID stored in the context by the middleware.
// Returns ("", false) for requests that passed through without a tenant.
func FromCtx(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// MustFromCtx returns the tenant ID or panics.
// Use only in handlers behind [Required] middleware where a tenant is guaranteed.
func MustFromCtx(ctx context.Context) string {
	id, ok := FromCtx(ctx)
	if !ok {
		panic("tenant: no tenant ID in context — is the tenant middleware registered?")
	}
	return id
}

// CacheKey returns a per-tenant prefixed cache key: "{tenant}:{key}".
// If no tenant is in context, returns the key as-is (global scope).
//
//	key := tenant.CacheKey(ctx, "user:42") // → "acme:user:42"
func CacheKey(ctx context.Context, key string) string {
	id, ok := FromCtx(ctx)
	if !ok {
		return key
	}
	return id + ":" + key
}

// Router is a helper to group routes under a tenant-scoped sub-router.
// All routes in the group will have a tenant in context or receive 401.
//
//	tenant.Router(app.Router, func(r chi.Router) {
//	    r.Get("/dashboard", myHandler)
//	})
func Router(r chi.Router, fn func(chi.Router)) {
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := FromCtx(r.Context()); !ok {
					http.Error(w, `{"error":"tenant required"}`, http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		})
		fn(r)
	})
}

// ErrNoTenant is returned when a tenant is required but not found.
var ErrNoTenant = errors.New("tenant: no tenant ID in request context")
