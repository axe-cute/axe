// Package logger provides a context-aware structured slog logger.
package logger

import (
	"context"
	"log/slog"
	"os"
)

type contextKey string

const (
	loggerKey    contextKey = "logger"
	requestIDKey contextKey = "request_id"
)

// New returns a *slog.Logger for the given environment (production=JSON, else text).
func New(env string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	var h slog.Handler
	if env == "production" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// WithLogger stores a logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromCtx retrieves the logger from ctx, falling back to slog.Default().
func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithRequestID stores a request ID and adds it to the logger in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	l := FromCtx(ctx).With("request_id", id)
	ctx = context.WithValue(ctx, requestIDKey, id)
	return WithLogger(ctx, l)
}

// WithFields returns a ctx with additional slog attributes attached to the logger.
func WithFields(ctx context.Context, args ...any) context.Context {
	return WithLogger(ctx, FromCtx(ctx).With(args...))
}
