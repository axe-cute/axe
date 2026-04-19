// Package worker provides Asynq-based background job processing for ecommerce.
package worker

import (
	"log/slog"

	"github.com/hibiken/asynq"
)

// Config holds Asynq worker configuration.
type Config struct {
	RedisAddr   string
	Concurrency int
	Queues      map[string]int
}

// Server wraps an Asynq server.
type Server struct {
	srv *asynq.Server
	mux *asynq.ServeMux
	log *slog.Logger
}

// New creates a new Asynq worker server.
func New(cfg Config, log *slog.Logger) *Server {
	srv := asynq.NewServer(asynq.RedisClientOpt{Addr: cfg.RedisAddr}, asynq.Config{
		Concurrency: cfg.Concurrency,
		Queues:      cfg.Queues,
	})
	return &Server{srv: srv, mux: asynq.NewServeMux(), log: log}
}

// Register registers a handler for a task type.
func (s *Server) Register(taskType string, handler asynq.HandlerFunc) {
	s.mux.HandleFunc(taskType, handler)
}

// Start begins processing background tasks.
func (s *Server) Start() error {
	s.log.Info("asynq worker starting")
	return s.srv.Run(s.mux)
}

// Shutdown gracefully stops the worker.
func (s *Server) Shutdown() {
	s.srv.Shutdown()
	s.log.Info("asynq worker stopped")
}
