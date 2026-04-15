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
	_ "github.com/jackc/pgx/v5/stdlib"

	ent "github.com/axe-cute/axe/ent"
	"github.com/axe-cute/axe/config"
	"github.com/axe-cute/axe/internal/handler"
	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/internal/repository"
	"github.com/axe-cute/axe/internal/service"
	"github.com/axe-cute/axe/pkg/cache"
	"github.com/axe-cute/axe/pkg/jwtauth"
	"github.com/axe-cute/axe/pkg/logger"
	"github.com/axe-cute/axe/pkg/metrics"
	"github.com/axe-cute/axe/pkg/outbox"
	"github.com/axe-cute/axe/pkg/ratelimit"
	"github.com/axe-cute/axe/pkg/worker"
	"github.com/redis/go-redis/v9"
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
	log.Info("axe starting", "port", cfg.ServerPort, "env", cfg.Environment)

	// ── Database ──────────────────────────────────────────────────────────────
	db, err := openDB(cfg)
	if err != nil {
		log.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	drv := entsql.OpenDB(dialect.Postgres, db)
	entClient := ent.NewClient(ent.Driver(drv))
	defer entClient.Close()

	if cfg.IsDevelopment() {
		if err := entClient.Schema.Create(context.Background()); err != nil {
			log.Error("ent schema migration failed", "error", err)
			os.Exit(1)
		}
	}

	// ── Redis cache ───────────────────────────────────────────────────────────
	cacheClient, err := cache.New(cache.Config{
		Addr:   cfg.RedisAddr(),
		Prefix: "axe:" + cfg.Environment + ":",
	})
	if err != nil {
		if cfg.IsProduction() {
			log.Error("redis connection failed", "error", err)
			os.Exit(1)
		}
		log.Warn("redis unavailable — cache disabled", "error", err)
		cacheClient = nil
	} else {
		defer cacheClient.Close()
		log.Info("redis connected", "addr", cfg.RedisAddr())
	}

	// ── JWT service ───────────────────────────────────────────────────────────
	jwtSvc := jwtauth.New(cfg.JWTSecret, cfg.AccessTokenTTL(), cfg.RefreshTokenTTL())

	// ── Rate Limiter (Redis sliding window) ───────────────────────────────────
	var redisForRL *redis.Client
	if cacheClient != nil {
		redisForRL = cacheClient.Redis()
	}
	limiter := ratelimit.New(redisForRL)

	// ── Background Worker (Asynq) ─────────────────────────────────────────────
	workerSrv := worker.New(worker.Config{
		RedisAddr:   cfg.RedisAddr(),
		Concurrency: cfg.AsynqConcurrency,
		Queues: map[string]int{
			cfg.AsynqQueueCritical: 6,
			cfg.AsynqQueueDefault:  3,
			"low":                  1,
		},
	}, log)
	workerSrv.Register(worker.TypeSendWelcomeEmail, worker.NewWelcomeEmailHandler(log))
	workerSrv.Register(worker.TypeProcessOutboxEvent, worker.NewOutboxEventHandler(log))

	// ── Outbox Poller ─────────────────────────────────────────────────────────
	outboxPoller := outbox.New(db, cfg.RedisAddr(), outbox.Config{
		Interval:  5 * time.Second,
		BatchSize: 50,
	}, log)

	// ── Composition Root ──────────────────────────────────────────────────────
	userRepo := repository.NewUserRepo(entClient)
	userSvc := service.NewUserService(userRepo)
	userHandler := handler.NewUserHandler(userSvc)
	authHandler := handler.NewAuthHandler(userSvc, jwtSvc, cacheClient) // cacheClient implements TokenBlocker
	docsHandler := handler.NewOpenAPIHandler()

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware stack
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(metrics.Middleware)          // Prometheus instrumentation
	r.Use(chimiddleware.Compress(5))

	// System endpoints (no auth, no rate limit)
	r.Get("/health", healthHandler)
	r.Get("/ready", readyHandler(db, cacheClient))
	r.Handle("/metrics", metrics.Handler()) // Prometheus scrape endpoint

	// API docs (no auth)
	r.Get("/openapi.yaml", docsHandler.Spec)
	r.Get("/docs", docsHandler.SwaggerUI)
	r.Get("/docs/redoc", docsHandler.Redoc)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(limiter.Global()) // 100 req/min per IP

		// Public auth (strict rate limit: 10 req/min)
		r.With(limiter.Strict()).Mount("/auth", authHandler.Routes())

		// Protected (JWT + blocklist check)
		r.Group(func(r chi.Router) {
			r.Use(middleware.JWTAuth(jwtSvc, cacheClient))
			r.Mount("/users", userHandler.Routes())
		})
	})

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Start background services
	ctx, cancelBg := context.WithCancel(context.Background())

	go func() {
		log.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	go func() {
		if err := workerSrv.Start(); err != nil {
			log.Warn("worker server error (may be expected if no Redis)", "error", err)
		}
	}()

	go outboxPoller.Start(ctx)

	log.Info("all services started",
		"http", fmt.Sprintf(":%d", cfg.ServerPort),
		"metrics", fmt.Sprintf(":%d/metrics", cfg.ServerPort),
		"worker", "asynq",
	)

	<-quit
	log.Info("shutdown signal received — draining...")

	cancelBg() // stop outbox poller

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	workerSrv.Shutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	log.Info("server stopped cleanly")
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func healthHandler(w http.ResponseWriter, r *http.Request) {
	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "axe",
	})
}

func readyHandler(db *sql.DB, cacheClient *cache.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"status": "ok"}

		if err := db.PingContext(r.Context()); err != nil {
			resp["db"] = "error: " + err.Error()
			resp["status"] = "degraded"
		} else {
			resp["db"] = "ok"
		}

		if cacheClient != nil {
			if err := cacheClient.Ping(r.Context()); err != nil {
				resp["cache"] = "error: " + err.Error()
				resp["status"] = "degraded"
			} else {
				resp["cache"] = "ok"
			}
		} else {
			resp["cache"] = "disabled"
		}

		status := http.StatusOK
		if resp["status"] == "degraded" {
			status = http.StatusServiceUnavailable
		}
		middleware.WriteJSON(w, status, resp)
	}
}

// ── DB helpers ────────────────────────────────────────────────────────────────

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
