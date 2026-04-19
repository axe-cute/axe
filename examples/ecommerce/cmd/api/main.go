package main

import (
	"context"
	"encoding/json"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/axe-cute/examples-ecommerce/config"
	"github.com/axe-cute/examples-ecommerce/pkg/devroutes"
	"github.com/axe-cute/examples-ecommerce/pkg/logger"
	"github.com/axe-cute/examples-ecommerce/pkg/ws"
	"github.com/axe-cute/examples-ecommerce/pkg/cache"
	"github.com/axe-cute/examples-ecommerce/pkg/ratelimit"
	"github.com/redis/go-redis/v9"
	"github.com/axe-cute/examples-ecommerce/pkg/worker"
	"github.com/axe-cute/examples-ecommerce/pkg/jwtauth"
	"github.com/axe-cute/examples-ecommerce/pkg/metrics"
	"github.com/axe-cute/examples-ecommerce/internal/handler"
	"github.com/axe-cute/examples-ecommerce/internal/repository"
	"github.com/axe-cute/examples-ecommerce/internal/service"
	entsql "entgo.io/ent/dialect/sql"
	ent "github.com/axe-cute/examples-ecommerce/ent"
	// axe:wire:import

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver for database/sql

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
	log.Info("ecommerce starting", "port", cfg.ServerPort, "env", cfg.Environment)

	// ── Redis cache ───────────────────────────────────────────────────────────
	cacheClient, err := cache.New(cache.Config{
		Addr:   cfg.RedisAddr(),
		Prefix: "ecommerce:" + cfg.Environment + ":",
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

	// ── Rate Limiter ─────────────────────────────────────────────────────────
	var redisForRL *redis.Client
	if cacheClient != nil {
		redisForRL = cacheClient.Redis()
	}
	limiter := ratelimit.New(redisForRL)
	_ = limiter

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

	_ = log

	// ── Database ─────────────────────────────────────────────────────────────
	sqlDB, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(cfg.DatabaseMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DatabaseMaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.DatabaseConnMaxLifetimeMins) * time.Minute)
	log.Info("database connected")

	drv := entsql.OpenDB("postgres", sqlDB)
	entClient := ent.NewClient(ent.Driver(drv))
	defer entClient.Close()
	// axe:wire:db
	_ = sqlDB // used by ent client (injected by axe generate resource)

	// ── JWT service ───────────────────────────────────────────────────────────
	jwtSvc := jwtauth.New(cfg.JWTSecret, cfg.AccessTokenTTL(), cfg.RefreshTokenTTL())
	_ = jwtSvc

	// ── WebSocket Hub ─────────────────────────────────────────────────────────
	// Shared across all WebSocket handlers. Start the event loop before the server.
	wsHub := ws.NewHub(ws.WithLogger(log))
	wsTracker := ws.NewUserConnTracker()
	_ = wsTracker

	// ── REST router (chi with full middleware stack) ───────────────────────────
	// NOTE: chimiddleware.Compress wraps http.ResponseWriter and strips
	// http.Hijacker, which nhooyr.io/websocket requires for the WS upgrade.
	// Keep Compress ONLY on the REST router; never add it to the WS router.
	restRouter := chi.NewRouter()
	restRouter.Use(chimiddleware.Recoverer)
	restRouter.Use(chimiddleware.RequestID)
	restRouter.Use(chimiddleware.Logger)
	restRouter.Use(metrics.Middleware)
	restRouter.Use(chimiddleware.Compress(5))

	productRepo := repository.NewProductRepo(entClient)
	productSvc := service.NewProductService(productRepo)
	orderRepo := repository.NewOrderRepo(entClient)
	orderSvc := service.NewOrderService(orderRepo, productRepo)
	reviewRepo := repository.NewReviewRepo(entClient)
	reviewSvc := service.NewReviewService(reviewRepo)
	// axe:wire:repo
	productHandler := handler.NewProductHandler(productSvc)
	orderHandler := handler.NewOrderHandler(orderSvc)
	reviewHandler := handler.NewReviewHandler(reviewSvc)
	// axe:wire:handler

	restRouter.Get("/health", healthHandler)
	restRouter.Get("/ready", readyHandler(sqlDB))
	restRouter.Handle("/metrics", metrics.Handler())

	restRouter.Route("/api/v1", func(r chi.Router) {
		r.Mount("/products", productHandler.Routes())
		r.Mount("/orders", orderHandler.Routes())
		r.Mount("/reviews", reviewHandler.Routes())
		// axe:wire:route
	})


	// ── WebSocket router (bare chi — NO response-wrapping middleware) ──────────
	// Wrapping middleware (Logger, Compress, Recoverer) all break http.Hijacker.
	// Only add non-wrapping middleware here (e.g. ws.WSAuth).
	wsRouter := chi.NewRouter()
	wsRouter.Use(chimiddleware.RequestID) // safe: does not wrap ResponseWriter

	// axe:wire:ws-route

	// ── Dev routes (Rails-like route listing on 404) ─────────────────────────
	restRouter.Get("/debug/routes", devroutes.DebugRoutesHandler(cfg.IsDevelopment(), restRouter, wsRouter))
	restRouter.NotFound(devroutes.NotFoundHandler(cfg.IsDevelopment(), restRouter, wsRouter))

	// ── Top-level mux: routes /ws/* to wsRouter, everything else to restRouter ─
	mux := http.NewServeMux()
	mux.Handle("/ws/", wsRouter)
	mux.Handle("/", restRouter)

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,  // MUST be 0 to support WebSocket connections
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	bgCtx, cancelBg := context.WithCancel(context.Background())

	// Start Hub event loop
	go wsHub.Run(bgCtx)

	go func() {
		if err := workerSrv.Start(); err != nil {
			log.Warn("worker server error (may be expected if no Redis)", "error", err)
		}
	}()

	go func() {
		if cfg.IsDevelopment() {
			devroutes.PrintRoutes(restRouter, wsRouter)
		}
		log.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	log.Info("shutdown signal received — draining...")

	cancelBg()
	wsHub.Shutdown()
	workerSrv.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	log.Info("server stopped cleanly")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, map[string]string{"status": "ok", "service": "ecommerce"})
}

func readyHandler(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"status": "ok"}

		if err := sqlDB.PingContext(r.Context()); err != nil {
			resp["db"] = "error: " + err.Error()
			resp["status"] = "degraded"
		} else {
			resp["db"] = "ok"
		}

		status := http.StatusOK
		if resp["status"] == "degraded" {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, resp)
		_ = status
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}
