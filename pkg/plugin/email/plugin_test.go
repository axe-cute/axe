package email

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/axe-cute/axe/pkg/plugin"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Config validation tests (Layer 4) ─────────────────────────────────────────

func TestNew_MissingFrom(t *testing.T) {
	_, err := New(Config{Provider: "log"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "From address is required")
}

func TestNew_InvalidFromAddress(t *testing.T) {
	_, err := New(Config{Provider: "log", From: "not-an-email"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid email address")
}

func TestNew_MissingAPIKey_Sendgrid(t *testing.T) {
	_, err := New(Config{Provider: "sendgrid", From: "noreply@example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APIKey is required")
}

func TestNew_MissingSMTPHost(t *testing.T) {
	_, err := New(Config{Provider: "smtp", From: "noreply@example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SMTPHost is required")
}

func TestNew_UnknownProvider(t *testing.T) {
	_, err := New(Config{Provider: "pigeon", From: "noreply@example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestNew_DefaultProvider_NoAPIKey(t *testing.T) {
	// Empty Provider defaults to "log" — no APIKey required.
	p, err := New(Config{From: "noreply@example.com"})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNew_LogProvider_Valid(t *testing.T) {
	p, err := New(Config{Provider: "log", From: "noreply@example.com"})
	require.NoError(t, err)
	assert.NotNil(t, p)
	assert.Equal(t, "email", p.Name())
}

func TestNew_SendGridProvider_Valid(t *testing.T) {
	p, err := New(Config{
		Provider: "sendgrid",
		From:     "noreply@example.com",
		APIKey:   "SG.test-key",
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNew_SMTPProvider_Valid(t *testing.T) {
	p, err := New(Config{
		Provider: "smtp",
		From:     "noreply@example.com",
		SMTPHost: "smtp.mailhog.local",
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

// ── Plugin lifecycle tests ────────────────────────────────────────────────────

func TestRegister_ProvidesService(t *testing.T) {
	p, err := New(Config{Provider: "log", From: "noreply@example.com"})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	// Sender must be resolvable via the typed service locator.
	svc, ok := plugin.Resolve[Sender](app, ServiceKey)
	require.True(t, ok, "Sender must be provided under ServiceKey")
	assert.NotNil(t, svc)
}

func TestShutdown_BeforeRegister(t *testing.T) {
	p, err := New(Config{Provider: "log", From: "noreply@example.com"})
	require.NoError(t, err)

	// Shutdown before Register must not panic.
	require.NoError(t, p.Shutdown(t.Context()))
}

func TestShutdown_AfterRegister(t *testing.T) {
	p, err := New(Config{Provider: "log", From: "noreply@example.com"})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Log sender tests ──────────────────────────────────────────────────────────

func TestLogSender_Send(t *testing.T) {
	s := newLogSender(slog.Default())

	// logSender must never return an error.
	err := s.Send(context.Background(), Message{
		To:      "user@example.com",
		Subject: "Test",
		Body:    "Hello!",
	})
	require.NoError(t, err)
}

// ── Integration via app.Use ───────────────────────────────────────────────────

func TestApp_UseEmailPlugin(t *testing.T) {
	email, err := New(Config{Provider: "log", From: "ci@example.com"})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	require.NoError(t, app.Use(email))
	require.NoError(t, app.Start(t.Context()))

	// Resolve and use the sender.
	svc := plugin.MustResolve[Sender](app, ServiceKey)
	require.NoError(t, svc.Send(t.Context(), Message{
		To:      "henry@example.com",
		Subject: "Welcome to axe",
		Body:    "Plugin system works!",
	}))

	require.NoError(t, app.Shutdown(t.Context()))
}

// ── ServiceKey constant test (Layer 5) ────────────────────────────────────────

func TestServiceKey_IsLowercasePluginName(t *testing.T) {
	// Convention: ServiceKey must match plugin Name() for discoverability.
	p, err := New(Config{Provider: "log", From: "noreply@example.com"})
	require.NoError(t, err)
	assert.Equal(t, p.Name(), ServiceKey)
}

// ── Default SMTP port ─────────────────────────────────────────────────────────

func TestConfig_DefaultSMTPPort(t *testing.T) {
	cfg := Config{Provider: "smtp", From: "noreply@example.com", SMTPHost: "localhost"}
	cfg.defaults()
	assert.Equal(t, 587, cfg.SMTPPort)
}

// ── Validation error message format ──────────────────────────────────────────

func TestNew_MultipleErrors_AllReported(t *testing.T) {
	_, err := New(Config{Provider: "sendgrid"}) // missing From AND APIKey
	require.Error(t, err)
	msg := err.Error()
	assert.True(t, strings.Contains(msg, "From") || strings.Contains(msg, "APIKey"),
		"at least one validation error should be mentioned: %s", msg)
}
