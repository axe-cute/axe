// Package logger provides a context-aware structured logger built on top of
// the standard library's log/slog package.
//
// Design:
//   - A *slog.Logger is stored in context.Context so that request-scoped fields
//     (request_id, user_id, trace_id) propagate automatically through the call stack.
//   - No global logger state is mutated after startup.
//
// Usage:
//
//	// In middleware: inject a logger with request_id
//	ctx = logger.WithFields(r.Context(), "request_id", requestID)
//
//	// In service/repository: retrieve and log
//	log := logger.FromCtx(ctx)
//	log.Info("creating user", "email", email)
package logger

import (
	"context"
	"log/slog"
	"os"
)

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

// New creates a new *slog.Logger.
// In production (JSON), it outputs structured JSON.
// In development (text), it outputs human-readable text.
func New(env string) *slog.Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: false,
	}

	if env == "production" || env == "staging" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		// Development: coloured text output
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: false,
		})
	}

	return slog.New(handler)
}

// WithLogger stores a *slog.Logger in the context.
// Call this once in the request middleware with the request-scoped logger.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromCtx retrieves the *slog.Logger from the context.
// If no logger is found, it returns the default slog logger.
// This is safe to call anywhere without a nil check.
func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithFields returns a new context containing a logger enriched with
// additional key-value pairs. It does NOT modify the existing logger.
//
//	ctx = logger.WithFields(ctx, "user_id", userID, "order_id", orderID)
func WithFields(ctx context.Context, args ...any) context.Context {
	l := FromCtx(ctx).With(args...)
	return WithLogger(ctx, l)
}

// WithRequestID injects the request ID into the logger in the context.
// Call this in the RequestID middleware.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return WithFields(ctx, "request_id", requestID)
}

// WithTraceID injects trace/span IDs into the logger in the context.
// Call this in the OpenTelemetry middleware (Story 3.2).
func WithTraceID(ctx context.Context, traceID, spanID string) context.Context {
	return WithFields(ctx, "trace_id", traceID, "span_id", spanID)
}

// WithUserID injects the authenticated user ID into the logger in the context.
// Call this in the auth middleware after token validation.
func WithUserID(ctx context.Context, userID string) context.Context {
	return WithFields(ctx, "user_id", userID)
}
