// Package email provides the axe email plugin.
//
// Supports SendGrid (production) and SMTP (self-hosted / dev).
// In dev mode, emails are logged instead of sent — MailHog compatible.
//
// Usage:
//
//	app.Use(email.New(email.Config{
//	    Provider: "sendgrid",
//	    APIKey:   os.Getenv("SENDGRID_API_KEY"),
//	    From:     "noreply@example.com",
//	}))
//
// Other plugins resolve the sender via the typed service locator:
//
//	svc := plugin.MustResolve[email.Sender](app, email.ServiceKey)
//	svc.Send(ctx, email.Message{To: "user@example.com", Subject: "Welcome"})
//
// Layer conformance (Story 8.10):
//   - Layer 1: implements plugin.Plugin (compiler-enforced)
//   - Layer 3: no DependsOn — standalone plugin
//   - Layer 4: config validated in New(), not Register()
//   - Layer 5: ServiceKey constant for cross-plugin service resolution
//   - Layer 6: uses app.Logger only — no self-created DB/Redis connections
package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/axe-cute/axe/pkg/plugin"
)

// ServiceKey is the typed service locator key for [Sender].
// Use plugin.MustResolve[email.Sender](app, email.ServiceKey) to access
// the email service from other plugins.
const ServiceKey = "email"

// ── Sender interface ──────────────────────────────────────────────────────────

// Sender is the public interface exposed by the email plugin.
// Other plugins depend on this interface, not the concrete implementation.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Message is a single email to be sent.
type Message struct {
	To      string // recipient address
	Subject string
	Body    string // plain text body
	HTML    string // optional HTML body (falls back to Body if empty)
}

// ── Config ────────────────────────────────────────────────────────────────────

// Config configures the email plugin.
type Config struct {
	// Provider selects the sending backend: "sendgrid", "smtp", or "log".
	// "log" mode logs emails to stdout — useful for dev/testing (MailHog compatible).
	// Default: "log" if APIKey and SMTPHost are both empty.
	Provider string

	// APIKey is the SendGrid API key (required when Provider == "sendgrid").
	APIKey string

	// From is the sender address shown in the email To field.
	// Required for all providers.
	From string

	// SMTPHost is the SMTP server hostname (required when Provider == "smtp").
	SMTPHost string
	// SMTPPort is the SMTP server port. Default: 587.
	SMTPPort int
	// SMTPUser is the SMTP authentication username.
	SMTPUser string
	// SMTPPass is the SMTP authentication password.
	SMTPPass string
}

// validate checks that required config fields are present.
// Called by New() — fail-fast before app.Start() (Layer 4).
func (c *Config) validate() error {
	var errs []string

	if c.From == "" {
		errs = append(errs, "From address is required")
	} else if _, err := mail.ParseAddress(c.From); err != nil {
		errs = append(errs, fmt.Sprintf("From %q is not a valid email address", c.From))
	}

	switch c.Provider {
	case "sendgrid":
		if c.APIKey == "" {
			errs = append(errs, "APIKey is required for sendgrid provider (set SENDGRID_API_KEY)")
		}
	case "smtp":
		if c.SMTPHost == "" {
			errs = append(errs, "SMTPHost is required for smtp provider")
		}
	case "log":
		// dev mode — no additional fields required
	default:
		errs = append(errs, fmt.Sprintf("unknown provider %q — must be sendgrid, smtp, or log", c.Provider))
	}

	if len(errs) > 0 {
		return errors.New("email: " + strings.Join(errs, "; "))
	}
	return nil
}

func (c *Config) defaults() {
	if c.Provider == "" {
		c.Provider = "log"
	}
	if c.SMTPPort == 0 {
		c.SMTPPort = 587
	}
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin implements [plugin.Plugin] for email sending.
type Plugin struct {
	cfg    Config
	sender Sender
	log    *slog.Logger
}

// New creates an email plugin with the given configuration.
// Returns an error if required config fields are missing (Layer 4: fail-fast in New).
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg}, nil
}

// Name returns the plugin identifier.
func (p *Plugin) Name() string { return "email" }

// Register initializes the email sender and provides it to the service locator.
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	p.log = app.Logger.With("plugin", p.Name())

	sender, err := newSender(p.cfg, p.log)
	if err != nil {
		return fmt.Errorf("email: create sender: %w", err)
	}
	p.sender = sender

	// Layer 5: provide via typed service locator using ServiceKey constant.
	plugin.Provide[Sender](app, ServiceKey, p.sender)

	p.log.Info("email plugin registered",
		"provider", p.cfg.Provider,
		"from", p.cfg.From,
	)
	return nil
}

// Shutdown performs graceful cleanup.
func (p *Plugin) Shutdown(_ context.Context) error {
	if p.log != nil {
		p.log.Info("email plugin shutdown")
	}
	return nil
}
