package email

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rewriteTransport redirects all HTTP requests to a test server URL.
type rewriteTransport struct {
	base      http.RoundTripper
	targetURL string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = rt.targetURL[len("http://"):]
	return rt.base.RoundTrip(req)
}

// ── sendgridSender.Send — actual method (not just helper functions) ──────────

func TestSendGrid_FullSend_PlainTextBody(t *testing.T) {
	var gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(202)
	}))
	defer srv.Close()

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &rewriteTransport{base: http.DefaultTransport, targetURL: srv.URL},
	}
	defer func() { http.DefaultClient = origClient }()

	s := &sendgridSender{apiKey: "SG.full-test", from: "sender@x.com"}
	err := s.Send(context.Background(), Message{
		To:      "rcpt@y.com",
		Subject: "Full Send Test",
		Body:    "plain content",
	})
	require.NoError(t, err)
	assert.Contains(t, gotBody, "rcpt@y.com")
	assert.Contains(t, gotBody, "text/plain")
}

func TestSendGrid_FullSend_HTMLBody(t *testing.T) {
	var gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(202)
	}))
	defer srv.Close()

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &rewriteTransport{base: http.DefaultTransport, targetURL: srv.URL},
	}
	defer func() { http.DefaultClient = origClient }()

	s := &sendgridSender{apiKey: "key", from: "a@b.com"}
	err := s.Send(context.Background(), Message{
		To:      "c@d.com",
		Subject: "HTML",
		HTML:    "<b>bold</b>",
	})
	require.NoError(t, err)
	assert.Contains(t, gotBody, "text/html")
}

func TestSendGrid_FullSend_ServerRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &rewriteTransport{base: http.DefaultTransport, targetURL: srv.URL},
	}
	defer func() { http.DefaultClient = origClient }()

	s := &sendgridSender{apiKey: "key", from: "a@b.com"}
	err := s.Send(context.Background(), Message{To: "c@d.com", Subject: "Fail", Body: "x"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sendgrid")
}

// ── smtpSender.Send — actual method ─────────────────────────────────────────

func TestSMTP_FullSend_ConnectionRefused(t *testing.T) {
	s := &smtpSender{host: "127.0.0.1", port: 59999, from: "x@y.com"}
	err := s.Send(context.Background(), Message{To: "a@b.com", Subject: "T", Body: "b"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "smtp")
}

func TestSMTP_FullSend_WithAuthAndHTML(t *testing.T) {
	s := &smtpSender{host: "127.0.0.1", port: 59999, user: "u", pass: "p", from: "x@y.com"}
	err := s.Send(context.Background(), Message{To: "a@b.com", Subject: "H", HTML: "<p>hi</p>"})
	assert.Error(t, err) // connection fails, but exercises auth + HTML path
}
