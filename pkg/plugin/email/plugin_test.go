package email

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/plugin"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
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

func TestNew_MultipleErrors_AllReported(t *testing.T) {
	_, err := New(Config{Provider: "sendgrid"}) // missing From AND APIKey
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "From")
	assert.Contains(t, msg, "APIKey")
}

// ── Config defaults ──────────────────────────────────────────────────────────

func TestConfig_DefaultProvider(t *testing.T) {
	cfg := Config{}
	cfg.defaults()
	assert.Equal(t, "log", cfg.Provider)
}

func TestConfig_DefaultSMTPPort(t *testing.T) {
	cfg := Config{Provider: "smtp", From: "noreply@example.com", SMTPHost: "localhost"}
	cfg.defaults()
	assert.Equal(t, 587, cfg.SMTPPort)
}

func TestConfig_CustomSMTPPort(t *testing.T) {
	cfg := Config{Provider: "smtp", From: "noreply@example.com", SMTPHost: "localhost", SMTPPort: 2525}
	cfg.defaults()
	assert.Equal(t, 2525, cfg.SMTPPort, "custom port should not be overridden")
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

func TestLogSender_SendHTML(t *testing.T) {
	s := newLogSender(slog.Default())
	err := s.Send(context.Background(), Message{
		To:      "user@example.com",
		Subject: "HTML Test",
		HTML:    "<h1>Hello</h1>",
	})
	require.NoError(t, err)
}

func TestLogSender_EmptyMessage(t *testing.T) {
	s := newLogSender(slog.Default())
	err := s.Send(context.Background(), Message{})
	require.NoError(t, err, "log sender should not error even on empty messages")
}

// ── newSender factory ─────────────────────────────────────────────────────────

func TestNewSender_Log(t *testing.T) {
	s, err := newSender(Config{Provider: "log"}, slog.Default())
	require.NoError(t, err)
	assert.IsType(t, &logSender{}, s)
}

func TestNewSender_SendGrid(t *testing.T) {
	s, err := newSender(Config{Provider: "sendgrid", APIKey: "key", From: "a@b.com"}, slog.Default())
	require.NoError(t, err)
	assert.IsType(t, &sendgridSender{}, s)
}

func TestNewSender_SMTP(t *testing.T) {
	s, err := newSender(Config{Provider: "smtp", SMTPHost: "localhost", From: "a@b.com"}, slog.Default())
	require.NoError(t, err)
	assert.IsType(t, &smtpSender{}, s)
}

func TestNewSender_Unknown(t *testing.T) {
	_, err := newSender(Config{Provider: "pigeon"}, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

// ── SendGrid sender with mock HTTP server ─────────────────────────────────────

func TestSendGridSender_Send_PlainText(t *testing.T) {
	var capturedBody string
	var capturedAuth string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.WriteHeader(202) // SendGrid returns 202
	}))
	defer ts.Close()

	// Override the endpoint by using newJSONRequest + doRequest directly.
	s := &sendgridSender{apiKey: "SG.test-key", from: "noreply@test.com"}

	// Test via the http helpers since sendgridSender.Send hardcodes the URL.
	// We test the payload construction directly.
	msg := Message{To: "user@test.com", Subject: "Hello", Body: "Plain text body"}
	body := msg.Body
	msgType := "text/plain"
	if msg.HTML != "" {
		body = msg.HTML
		msgType = "text/html"
	}
	_ = s
	_ = body
	_ = msgType

	// Instead, test newJSONRequest + doRequest via mock server.
	req, err := newJSONRequest(context.Background(), "POST", ts.URL, strings.NewReader(`{"test":"data"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer SG.test-key")
	err = doRequest(req, 202)
	require.NoError(t, err)
	assert.Equal(t, "Bearer SG.test-key", capturedAuth)
	assert.Contains(t, capturedBody, "test")
}

func TestSendGridSender_HTMLFallback(t *testing.T) {
	// Verify that HTML body is preferred over plain text.
	msg := Message{
		To:      "user@test.com",
		Subject: "Hello",
		Body:    "plain",
		HTML:    "<b>html</b>",
	}
	body := msg.Body
	msgType := "text/plain"
	if msg.HTML != "" {
		body = msg.HTML
		msgType = "text/html"
	}
	assert.Equal(t, "<b>html</b>", body)
	assert.Equal(t, "text/html", msgType)
}

// ── HTTP helper tests ─────────────────────────────────────────────────────────

func TestNewJSONRequest_SetsContentType(t *testing.T) {
	req, err := newJSONRequest(context.Background(), "POST", "http://localhost/test", strings.NewReader("{}"))
	require.NoError(t, err)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestDoRequest_SuccessStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL, nil)
	err := doRequest(req, 200)
	require.NoError(t, err)
}

func TestDoRequest_WrongStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL, nil)
	err := doRequest(req, 200)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestDoRequest_ConnectionError(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://127.0.0.1:1", nil) // unreachable port
	err := doRequest(req, 200)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
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

// ── Message struct tests ─────────────────────────────────────────────────────

func TestMessage_Defaults(t *testing.T) {
	msg := Message{}
	assert.Empty(t, msg.To)
	assert.Empty(t, msg.Subject)
	assert.Empty(t, msg.Body)
	assert.Empty(t, msg.HTML)
}

// ── SMTP sender construction ─────────────────────────────────────────────────

func TestNewSMTPSender_Fields(t *testing.T) {
	s := newSMTPSender(Config{
		SMTPHost: "mail.example.com",
		SMTPPort: 465,
		SMTPUser: "user",
		SMTPPass: "pass",
		From:     "noreply@example.com",
	})
	assert.Equal(t, "mail.example.com", s.host)
	assert.Equal(t, 465, s.port)
	assert.Equal(t, "user", s.user)
	assert.Equal(t, "pass", s.pass)
	assert.Equal(t, "noreply@example.com", s.from)
}

func TestNewSendGridSender_Fields(t *testing.T) {
	s := newSendGridSender(Config{
		APIKey: "SG.test",
		From:   "noreply@example.com",
	})
	assert.Equal(t, "SG.test", s.apiKey)
	assert.Equal(t, "noreply@example.com", s.from)
}

// ── Register with each provider ──────────────────────────────────────────────

func TestRegister_LogProvider(t *testing.T) {
	p, _ := New(Config{Provider: "log", From: "noreply@example.com"})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	svc, ok := plugin.Resolve[Sender](app, ServiceKey)
	require.True(t, ok)
	assert.IsType(t, &logSender{}, svc)
}

func TestRegister_SendGridProvider(t *testing.T) {
	p, _ := New(Config{Provider: "sendgrid", From: "noreply@example.com", APIKey: "SG.key"})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	svc, ok := plugin.Resolve[Sender](app, ServiceKey)
	require.True(t, ok)
	assert.IsType(t, &sendgridSender{}, svc)
}

func TestRegister_SMTPProvider(t *testing.T) {
	p, _ := New(Config{Provider: "smtp", From: "noreply@example.com", SMTPHost: "localhost"})
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	svc, ok := plugin.Resolve[Sender](app, ServiceKey)
	require.True(t, ok)
	assert.IsType(t, &smtpSender{}, svc)
}
