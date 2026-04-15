package logger_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/axe-go/axe/pkg/logger"
)

func TestFromCtx_Default(t *testing.T) {
	ctx := context.Background()
	l := logger.FromCtx(ctx)
	if l == nil {
		t.Fatal("FromCtx should never return nil")
	}
	// Should return the default slog logger
	if l != slog.Default() {
		t.Error("FromCtx with empty context should return slog.Default()")
	}
}

func TestWithLogger_And_FromCtx(t *testing.T) {
	original := logger.New("development")
	ctx := logger.WithLogger(context.Background(), original)

	retrieved := logger.FromCtx(ctx)
	if retrieved != original {
		t.Error("FromCtx should return the logger stored via WithLogger")
	}
}

func TestWithFields(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.New("development"))
	ctx = logger.WithFields(ctx, "request_id", "abc-123")

	l := logger.FromCtx(ctx)
	if l == nil {
		t.Fatal("logger should not be nil after WithFields")
	}
}

func TestWithRequestID(t *testing.T) {
	ctx := context.Background()
	ctx = logger.WithRequestID(ctx, "req-xyz")

	l := logger.FromCtx(ctx)
	if l == nil {
		t.Fatal("logger should carry request_id")
	}
}

func TestWithFields_DoesNotMutateParent(t *testing.T) {
	base := logger.New("development")
	ctx := logger.WithLogger(context.Background(), base)

	childCtx := logger.WithFields(ctx, "key", "value")

	// Parent context logger should be unchanged (same pointer)
	if logger.FromCtx(ctx) != base {
		t.Error("WithFields should not mutate the parent context logger")
	}
	// Child context should have a different (enriched) logger
	if logger.FromCtx(childCtx) == base {
		t.Error("WithFields should return an enriched logger in child context")
	}
}

func TestNew_Production(t *testing.T) {
	l := logger.New("production")
	if l == nil {
		t.Fatal("New should return a non-nil logger for production")
	}
}

func TestNew_Development(t *testing.T) {
	l := logger.New("development")
	if l == nil {
		t.Fatal("New should return a non-nil logger for development")
	}
}
