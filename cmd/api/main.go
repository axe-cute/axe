package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver as database/sql

	ent "github.com/axe-go/axe/ent"
	"github.com/axe-go/axe/config"
	"github.com/axe-go/axe/internal/handler"
	"github.com/axe-go/axe/internal/handler/middleware"
	"github.com/axe-go/axe/internal/repository"
	"github.com/axe-go/axe/internal/service"
	"github.com/axe-go/axe/pkg/logger"
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.LoadFromFile(".env")
	if err != nil {
		cfg, err = config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: config: %v\n", err)
			os.Exit(1)
		}
	}

	// ── Logger ────────────────────────────────────────────────────────────────
	log := logger.New(cfg.Environment)
	slog.SetDefault(log)
	log.Info("axe starting",
		"port", cfg.ServerPort,
		"env", cfg.Environment,
	)

	// ── Database — shared *sql.DB pool ───────────────────────────────────────
	db, err := openDB(cfg)
	if err != nil {
		log.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// ── Ent client — wraps the shared pool ───────────────────────────────────
	drv := entsql.OpenDB(dialect.Postgres, db)
	entClient := ent.NewClient(ent.Driver(drv))
	defer entClient.Close()

	// ── Auto-migrate (dev) — replace with Atlas in production ────────────────
	if cfg.IsDevelopment() {
		if err := entClient.Schema.Create(context.Background()); err != nil {
			log.Error("ent schema migration failed", "error", err)
			os.Exit(1)
		}
	}

	// ── Composition Root — wire all layers ───────────────────────────────────
	userRepo := repository.NewUserRepo(entClient)
	userSvc := service.NewUserService(userRepo)
	userHandler := handler.NewUserHandler(userSvc)

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware stack (order matters)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(chimiddleware.Compress(5))

	// System endpoints
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		middleware.WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "axe",
		})
	})

	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			middleware.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "error",
				"db":     err.Error(),
			})
			return
		}
		middleware.WriteJSON(w, http.StatusOK, map[string]string{
			"status": "ok",
			"db":     "ok",
		})
	})

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		r.Mount("/users", userHandler.Routes())
		// TODO (Story 1.9+): mount additional domain handlers here
	})

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	log.Info("shutdown signal received, draining connections...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("server stopped cleanly")
}

// openDB opens and configures a *sql.DB connection pool using the pgx driver.
func openDB(cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(cfg.DatabaseMaxOpenConns)
	db.SetMaxIdleConns(cfg.DatabaseMaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.DatabaseConnMaxLifetimeMins) * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return db, nil
}
