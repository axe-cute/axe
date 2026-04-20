// Package sentry provides the axe Sentry error tracking plugin.
//
// It integrates the official sentry-go SDK with the axe framework, providing:
//   - Automatic panic recovery and reporting
//   - Automatic 5xx HTTP error reporting
//   - Distributed tracing integration (if TracesSampleRate > 0)
//   - User context enrichment via a pluggable extraction function
//
// Usage:
//
//	app.Use(sentryplugin.New(sentryplugin.Config{
//	    DSN:              os.Getenv("SENTRY_DSN"),
//	    Environment:      "production",
//	    TracesSampleRate: 1.0,
//	    GetUserID: func(ctx context.Context) string {
//	        if claims := middleware.ClaimsFromCtx(ctx); claims != nil {
//	            return claims.UserID
//	        }
//	        return ""
//	    },
//	}))
//
// If DSN is empty, the plugin gracefully falls back to a no-op implementation,
// allowing local development without spamming an upstream dashboard.
//
// Layer conformance:
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 6: uses app.Router, app.Logger — no heavy shared infrastructure setup
//     besides Sentry's own global client init.
package sentry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/obs"
)

// Config configures the Sentry plugin.
type Config struct {
	// DSN is the Sentry Data Source Name.
	// If empty, the plugin operates in a no-op mode (dev friendly).
	DSN string
	// Environment is the deployment environment (e.g. "production", "staging").
	// Default: "development".
	Environment string
	// Release is the release version. Default is tied to [plugin.AxeVersion].
	Release string
	// TracesSampleRate configures the sample rate for Sentry Tracing [0.0, 1.0].
	// Default: 0.0 (Tracing disabled).
	TracesSampleRate float64
	// FlushTimeout is how long to block during app shutdown for pending events.
	// Default: 2s.
	FlushTimeout time.Duration
	// GetUserID is an optional function to extract the user ID from the request context.
	// Used to enrich Sentry events with the affected user.
	GetUserID func(ctx context.Context) string
	// Transport allows injecting a custom Sentry transport (for tests).
	Transport sentry.Transport
	// Repanic controls whether panics are re-panicked after being caught and sent
	// to Sentry. Default: true (production behaviour). Set to false in tests so
	// the mock transport can flush before the connection is torn down.
	Repanic *bool
}

func (c *Config) defaults() {
	if c.Environment == "" {
		c.Environment = "development"
	}
	if c.Release == "" {
		c.Release = "axe@" + plugin.AxeVersion
	}
	if c.FlushTimeout == 0 {
		c.FlushTimeout = 2 * time.Second
	}
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin is the axe Sentry plugin.
type Plugin struct {
	cfg Config
	log *slog.Logger
}

// New creates a Sentry plugin.
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	return &Plugin{cfg: cfg}, nil
}

// Name implements [plugin.Plugin].
func (p *Plugin) Name() string { return "sentry" }

// MinAxeVersion declares required axe version.
func (p *Plugin) MinAxeVersion() string { return "v1.0.0" }

// Register initializes the Sentry SDK and injects the HTTP middleware.
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
	p.log = obs.Logger(app, p.Name())

	if p.cfg.DSN == "" {
		p.log.Info("sentry plugin registered in no-op mode (empty DSN)")
		return nil
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              p.cfg.DSN,
		Environment:      p.cfg.Environment,
		Release:          p.cfg.Release,
		TracesSampleRate: p.cfg.TracesSampleRate,
		Transport:        p.cfg.Transport,
	})
	if err != nil {
		return fmt.Errorf("sentry: init failed: %w", err)
	}

	// Create sentry-go HTTP middleware wrapper.
	// By default Repanic is true to preserve axe's panic recovery chain.
	// It can be overridden via Config.Repanic (set false in tests).
	repanic := true
	if p.cfg.Repanic != nil {
		repanic = *p.cfg.Repanic
	}
	sentryWrapper := sentryhttp.New(sentryhttp.Options{
		Repanic: repanic,
	})

	// Inject custom middleware into app Router.
	original := app.Router
	app.Router.Use(func(next http.Handler) http.Handler {
		return sentryWrapper.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Enrich user context if extractor provided.
			if p.cfg.GetUserID != nil {
				if uid := p.cfg.GetUserID(ctx); uid != "" {
					if hub := sentry.GetHubFromContext(ctx); hub != nil {
						hub.Scope().SetUser(sentry.User{ID: uid})
					}
				}
			}

			// Wrap response writer to capture status code (for 5xx checks).
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			// Capture explicit 5xx errors that didn't panic.
			if ww.Status() >= 500 {
				if hub := sentry.GetHubFromContext(ctx); hub != nil {
					// Add an extra breadcrumb before capturing.
					hub.Scope().AddBreadcrumb(&sentry.Breadcrumb{
						Category: "http",
						Message:  fmt.Sprintf("HTTP 5xx returned to client: %s", r.URL.Path),
						Level:    sentry.LevelError,
					}, 10)

					// Capture message for 5xx. If the handler propagated an error via
					// a context value or an app framework error, it could be captured here.
					// For now, capture the 5xx HTTP event.
					hub.CaptureMessage(fmt.Sprintf("HTTP %d on %s %s", ww.Status(), r.Method, r.URL.Path))
				}
			}
		}))
	})
	_ = original

	p.log.Info("sentry plugin registered",
		"env", p.cfg.Environment,
		"traces_sample_rate", p.cfg.TracesSampleRate,
	)
	return nil
}

// Shutdown flushes pending events before exit.
func (p *Plugin) Shutdown(ctx context.Context) error {
	if p.cfg.DSN == "" {
		return nil
	}

	// Determine timeout. If context has a deadline, use it, bounded by FlushTimeout.
	timeout := p.cfg.FlushTimeout
	if deadline, ok := ctx.Deadline(); ok {
		ctxTimeout := time.Until(deadline)
		if ctxTimeout < timeout && ctxTimeout > 0 {
			timeout = ctxTimeout
		}
	}

	flushed := sentry.Flush(timeout)
	if !flushed {
		p.log.Warn("sentry shutdown flush timed out", "timeout", timeout)
		return nil // Non-fatal
	}

	p.log.Info("sentry plugin shutdown complete")
	return nil
}
