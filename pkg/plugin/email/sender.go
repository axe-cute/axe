package email

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

// newSender creates the correct Sender implementation based on Config.Provider.
func newSender(cfg Config, log *slog.Logger) (Sender, error) {
	switch cfg.Provider {
	case "sendgrid":
		return newSendGridSender(cfg), nil
	case "smtp":
		return newSMTPSender(cfg), nil
	case "log":
		return newLogSender(log), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}

// ── Log sender (dev / testing) ────────────────────────────────────────────────

// logSender logs emails instead of sending them.
// Compatible with MailHog — just point MailHog at the same log stream.
type logSender struct {
	log *slog.Logger
}

func newLogSender(log *slog.Logger) *logSender {
	return &logSender{log: log}
}

func (s *logSender) Send(_ context.Context, msg Message) error {
	s.log.Info("email (dev mode — not sent)",
		"to", msg.To,
		"subject", msg.Subject,
		"body_len", len(msg.Body),
	)
	return nil
}

// ── SendGrid sender ───────────────────────────────────────────────────────────

// sendgridSender sends email via the SendGrid API v3.
// Uses net/http directly — no SDK dependency.
type sendgridSender struct {
	apiKey string
	from   string
}

func newSendGridSender(cfg Config) *sendgridSender {
	return &sendgridSender{
		apiKey: cfg.APIKey,
		from:   cfg.From,
	}
}

func (s *sendgridSender) Send(ctx context.Context, msg Message) error {
	// build JSON payload for SendGrid /v3/mail/send
	body := msg.Body
	msgType := "text/plain"
	if msg.HTML != "" {
		body = msg.HTML
		msgType = "text/html"
	}

	payload := fmt.Sprintf(`{
		"personalizations": [{"to": [{"email": %q}]}],
		"from": {"email": %q},
		"subject": %q,
		"content": [{"type": %q, "value": %q}]
	}`, msg.To, s.from, msg.Subject, msgType, body)

	req, err := newJSONRequest(ctx, "POST",
		"https://api.sendgrid.com/v3/mail/send",
		strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("email: sendgrid: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	if err := doRequest(req, 202); err != nil {
		return fmt.Errorf("email: sendgrid: %w", err)
	}
	return nil
}

// ── SMTP sender ───────────────────────────────────────────────────────────────

// smtpSender sends email via SMTP (stdlib net/smtp).
type smtpSender struct {
	host string
	port int
	user string
	pass string
	from string
}

func newSMTPSender(cfg Config) *smtpSender {
	return &smtpSender{
		host: cfg.SMTPHost,
		port: cfg.SMTPPort,
		user: cfg.SMTPUser,
		pass: cfg.SMTPPass,
		from: cfg.From,
	}
}

func (s *smtpSender) Send(_ context.Context, msg Message) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	body := msg.Body
	if msg.HTML != "" {
		body = msg.HTML
	}

	raw := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		msg.To, s.from, msg.Subject, body)

	var auth smtp.Auth
	if s.user != "" {
		auth = smtp.PlainAuth("", s.user, s.pass, s.host)
	}

	if err := smtp.SendMail(addr, auth, s.from, []string{msg.To}, []byte(raw)); err != nil {
		return fmt.Errorf("email: smtp: %w", err)
	}
	return nil
}
