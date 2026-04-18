// Package otel provides the axe OpenTelemetry observability plugin.
//
// It instruments HTTP handlers with distributed tracing (W3C TraceContext),
// auto-injects span context into all requests, and exports traces via OTLP/HTTP.
//
// Usage:
//
//	app.Use(otel.New(otel.Config{
//	    ServiceName:    "my-axe-app",
//	    Endpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), // e.g. "http://otel-collector:4318"
//	    SampleRate:     1.0,   // 100% sampling
//	}))
//
// After registration, all chi routes receive automatic span injection:
//
//	GET /users/:id  →  span: "GET /users/{id}"
//	                   attributes: http.method, http.route, http.status_code, ...
//
// Traces are propagated via W3C traceparent/tracestate headers.
// Downstream HTTP calls that forward these headers will appear as
// child spans in Jaeger / Grafana Tempo / Datadog.
//
// Dev mode: set Endpoint="" to use stdout exporter (no infrastructure needed).
//
// Layer conformance:
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 5: ServiceKey constant
//   - Layer 6: uses app.Router, app.Logger — no new DB connections
package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/obs"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// ServiceKey is the service locator key for the OTel [trace.Tracer].
const ServiceKey = "otel"

// ── Config ────────────────────────────────────────────────────────────────────

// Config configures the OpenTelemetry plugin.
type Config struct {
	// ServiceName is the logical service name attached to all spans. Required.
	ServiceName string
	// ServiceVersion is the service version tag. Default: "unknown".
	ServiceVersion string
	// Endpoint is the OTLP/HTTP collector URL.
	// Example: "http://jaeger:4318" or "http://localhost:4318"
	// Leave empty to use stdout exporter (dev mode — logs spans to stdout).
	Endpoint string
	// SampleRate is the fraction of traces to sample [0.0, 1.0].
	// Default: 1.0 (100% — sample everything).
	SampleRate float64
	// Headers sets extra HTTP headers for the OTLP exporter (e.g. auth tokens).
	Headers map[string]string
	// Timeout for the OTLP exporter. Default: 10s.
	Timeout time.Duration
}

func (c *Config) defaults() {
	if c.ServiceVersion == "" {
		c.ServiceVersion = "unknown"
	}
	if c.SampleRate <= 0 {
		c.SampleRate = 1.0
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
}

func (c *Config) validate() error {
	if c.ServiceName == "" {
		return errors.New("otel: ServiceName is required")
	}
	return nil
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin is the axe OpenTelemetry plugin.
type Plugin struct {
	cfg      Config
	provider *sdktrace.TracerProvider
	log      *slog.Logger
}

// New creates an OTel plugin. Returns an error if required config is missing.
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg}, nil
}

// Name implements [plugin.Plugin].
func (p *Plugin) Name() string { return "otel" }

// MinAxeVersion declares required axe version.
func (p *Plugin) MinAxeVersion() string { return "v1.0.0" }

// Register initialises the TracerProvider and wraps the chi router with OTel middleware.
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
	p.log = obs.Logger(app, p.Name())

	// Build the exporter.
	exporter, err := p.buildExporter(ctx)
	if err != nil {
		return fmt.Errorf("otel: build exporter: %w", err)
	}

	// Build the resource.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(p.cfg.ServiceName),
			semconv.ServiceVersion(p.cfg.ServiceVersion),
			attribute.String("axe.version", plugin.AxeVersion),
		),
	)
	if err != nil {
		// Non-fatal — use default resource.
		p.log.Warn("otel: resource.New failed, using default", "error", err)
		res = resource.Default()
	}

	// Build the TracerProvider.
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(p.cfg.SampleRate))
	p.provider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Register as the global provider — all otel.Tracer() calls use this.
	otel.SetTracerProvider(p.provider)

	// Register W3C TraceContext + Baggage propagators.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Layer 5: provide tracer via service locator.
	tracer := p.provider.Tracer(p.cfg.ServiceName)
	plugin.Provide[trace.Tracer](app, ServiceKey, tracer)

	// Wrap the chi router's root handler with OTel HTTP instrumentation.
	// This injects span context into every request context and traces all routes.
	original := app.Router
	app.Router.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "chi",
			otelhttp.WithTracerProvider(p.provider),
			otelhttp.WithPropagators(otel.GetTextMapPropagator()),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				// Use "METHOD /path" for clean span names.
				return r.Method + " " + r.URL.Path
			}),
		)
	})
	_ = original // keeps the reference; middleware added to same router instance

	p.log.Info("otel plugin registered",
		"service_name", p.cfg.ServiceName,
		"endpoint", func() string {
			if p.cfg.Endpoint == "" {
				return "stdout (dev mode)"
			}
			return p.cfg.Endpoint
		}(),
		"sample_rate", p.cfg.SampleRate,
	)
	return nil
}

// Shutdown flushes and shuts down the TracerProvider gracefully.
func (p *Plugin) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	if err := p.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("otel: provider shutdown: %w", err)
	}
	p.log.Info("otel plugin shutdown complete")
	return nil
}

// ── Exporter factory ──────────────────────────────────────────────────────────

func (p *Plugin) buildExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	// Dev mode — stdout exporter requires no infrastructure.
	if p.cfg.Endpoint == "" {
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("stdout exporter: %w", err)
		}
		p.log.Info("otel using stdout exporter (dev mode)")
		return exp, nil
	}

	// Production — OTLP/HTTP exporter.
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(p.cfg.Endpoint),
		otlptracehttp.WithTimeout(p.cfg.Timeout),
	}
	if len(p.cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(p.cfg.Headers))
	}

	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("OTLP HTTP exporter: %w", err)
	}
	return exp, nil
}

// ── Helper: start a span from context ────────────────────────────────────────

// StartSpan is a convenience wrapper for starting a span using the global tracer.
// Use in handler code for business-level tracing:
//
//	ctx, span := otel.StartSpan(ctx, "process-payment")
//	defer span.End()
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer("axe").Start(ctx, name, opts...)
}
