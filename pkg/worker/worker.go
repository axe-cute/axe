// Package worker provides Asynq-based background job processing for axe.
// Workers run in a separate goroutine alongside the HTTP server.
//
// Architecture:
//   - Tasks are defined as typed constants (task type strings)
//   - Handlers implement asynq.Handler interface
//   - The Server wraps asynq.Server with graceful shutdown
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
)

// ── Task type constants ───────────────────────────────────────────────────────

const (
	// TypeSendWelcomeEmail is triggered when a new user registers.
	TypeSendWelcomeEmail = "email:welcome"

	// TypeProcessOutboxEvent processes a pending outbox_events row.
	TypeProcessOutboxEvent = "outbox:process"
)

// ── Task payload types ────────────────────────────────────────────────────────

// WelcomeEmailPayload holds data for the welcome email task.
type WelcomeEmailPayload struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// OutboxEventPayload holds data for the outbox processor task.
type OutboxEventPayload struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Aggregate string `json:"aggregate"`
}

// ── Task factories ────────────────────────────────────────────────────────────

// NewWelcomeEmailTask creates a new welcome email task.
func NewWelcomeEmailTask(userID, email, name string) (*asynq.Task, error) {
	payload, err := json.Marshal(WelcomeEmailPayload{
		UserID: userID,
		Email:  email,
		Name:   name,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal WelcomeEmailPayload: %w", err)
	}
	return asynq.NewTask(TypeSendWelcomeEmail, payload, asynq.MaxRetry(3)), nil
}

// NewOutboxEventTask creates a task to process an outbox event.
func NewOutboxEventTask(eventID, eventType, aggregate string) (*asynq.Task, error) {
	payload, err := json.Marshal(OutboxEventPayload{
		EventID:   eventID,
		EventType: eventType,
		Aggregate: aggregate,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal OutboxEventPayload: %w", err)
	}
	return asynq.NewTask(TypeProcessOutboxEvent, payload, asynq.MaxRetry(5)), nil
}

// ── Server ────────────────────────────────────────────────────────────────────

// Server wraps an asynq.Server for background job processing.
type Server struct {
	srv *asynq.Server
	mux *asynq.ServeMux
	log *slog.Logger
}

// Config holds worker server settings.
type Config struct {
	RedisAddr     string
	RedisPassword string
	Concurrency   int
	Queues        map[string]int // queue name → priority weight
}

// New creates a new worker Server.
func New(cfg Config, log *slog.Logger) *Server {
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 10
	}
	if cfg.Queues == nil {
		cfg.Queues = map[string]int{
			"critical": 6,
			"default":  3,
			"low":      1,
		}
	}

	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		},
		asynq.Config{
			Concurrency: cfg.Concurrency,
			Queues:      cfg.Queues,
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				log.Error("task failed",
					"type", task.Type(),
					"payload", string(task.Payload()),
					"error", err,
				)
			}),
		},
	)

	mux := asynq.NewServeMux()
	return &Server{srv: srv, mux: mux, log: log}
}

// Register adds a task handler to the worker mux.
func (s *Server) Register(taskType string, h asynq.Handler) {
	s.mux.Handle(taskType, h)
}

// Start runs the worker server.
// It blocks until the context is cancelled.
func (s *Server) Start() error {
	s.log.Info("worker server starting")
	return s.srv.Run(s.mux)
}

// Shutdown gracefully stops the worker server.
func (s *Server) Shutdown() {
	s.log.Info("worker shutting down")
	s.srv.Shutdown()
}

// ── Handler implementations ───────────────────────────────────────────────────

// WelcomeEmailHandler handles the TypeSendWelcomeEmail task.
type WelcomeEmailHandler struct {
	log *slog.Logger
}

// NewWelcomeEmailHandler creates a new WelcomeEmailHandler.
func NewWelcomeEmailHandler(log *slog.Logger) *WelcomeEmailHandler {
	return &WelcomeEmailHandler{log: log}
}

// ProcessTask implements asynq.Handler.
func (h *WelcomeEmailHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p WelcomeEmailPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal WelcomeEmailPayload: %w", err)
	}

	// Framework reference: logs the email send. Host applications should inject
	// a real email.Sender via the email plugin (pkg/plugin/email).
	h.log.Info("sending welcome email",
		"user_id", p.UserID,
		"email", p.Email,
		"name", p.Name,
	)

	// Simulate email send
	return nil
}

// OutboxEventHandler processes outbox_events table rows.
type OutboxEventHandler struct {
	log *slog.Logger
}

// NewOutboxEventHandler creates a new OutboxEventHandler.
func NewOutboxEventHandler(log *slog.Logger) *OutboxEventHandler {
	return &OutboxEventHandler{log: log}
}

// ProcessTask implements asynq.Handler.
func (h *OutboxEventHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p OutboxEventPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal OutboxEventPayload: %w", err)
	}

	// Framework reference: logs the event. Host applications should implement
	// an EventRouter to dispatch events to downstream handlers (message bus, webhook, etc.).
	h.log.Info("processing outbox event",
		"event_id", p.EventID,
		"event_type", p.EventType,
		"aggregate", p.Aggregate,
	)
	return nil
}
