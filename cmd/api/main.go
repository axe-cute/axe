package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/redis/go-redis/v9"

	"github.com/axe-cute/axe/config"
	ent "github.com/axe-cute/axe/ent"
	"github.com/axe-cute/axe/internal/handler"
	"github.com/axe-cute/axe/internal/handler/hook"
	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/internal/repository"
	"github.com/axe-cute/axe/internal/service"
	"github.com/axe-cute/axe/internal/setup"
	"github.com/axe-cute/axe/pkg/cache"
	"github.com/axe-cute/axe/pkg/db"
	_ "github.com/axe-cute/axe/pkg/db/mysql"    // register MySQL adapter
	_ "github.com/axe-cute/axe/pkg/db/postgres" // register PostgreSQL adapter
	"github.com/axe-cute/axe/pkg/jwtauth"
	"github.com/axe-cute/axe/pkg/logger"
	"github.com/axe-cute/axe/pkg/metrics"
	"github.com/axe-cute/axe/pkg/outbox"
	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/storage"
	"github.com/axe-cute/axe/pkg/ratelimit"
	"github.com/axe-cute/axe/pkg/worker"
	"github.com/axe-cute/axe/pkg/ws"
	// axe:wire:import
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
	log.Info("connecting to database", "driver", cfg.DBDriver)
	sqlDB, entDialect, err := db.Open(cfg.DBDriver, db.AdapterConfig{
		URL:             cfg.DatabaseURL,
		MaxOpenConns:    cfg.DatabaseMaxOpenConns,
		MaxIdleConns:    cfg.DatabaseMaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.DatabaseConnMaxLifetimeMins) * time.Minute,
	})
	if err != nil {
		log.Error("database connection failed", "driver", cfg.DBDriver, "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	drv := entsql.OpenDB(entDialect, sqlDB)
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
	outboxPoller := outbox.NewWithRedis(sqlDB, cfg.RedisAddr(), outbox.Config{
		Interval:  5 * time.Second,
		BatchSize: 50,
		Driver:    cfg.DBDriver,
	}, log)

	// ── WebSocket Hub ─────────────────────────────────────────────────────────────
	var wsAdapter ws.Adapter = ws.MemoryAdapter{}
	if cfg.HubAdapter == "redis" && cacheClient != nil {
		wsAdapter = ws.NewRedisAdapter(cacheClient.Redis(), ws.WithRedisLogger(log))
		log.Info("ws: using Redis adapter", "addr", cfg.RedisAddr())
	} else {
		log.Info("ws: using in-memory adapter (single-instance)")
	}
	wsHub := ws.NewHub(
		ws.WithAdapter(wsAdapter),
		ws.WithLogger(log),
	)
	wsTracker := ws.NewUserConnTracker()

	// ── Composition Root ──────────────────────────────────────────────────────
	userRepo := repository.NewUserRepo(entClient)
	userSvc := service.NewUserService(userRepo)
	userHandler := handler.NewUserHandler(userSvc)
	authHandler := handler.NewAuthHandler(userSvc, jwtSvc, cacheClient) // cacheClient implements TokenBlocker
	docsHandler := handler.NewOpenAPIHandler()
	// axe:wire:repo
	// axe:wire:handler

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware stack
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   strings.Split(cfg.CORSAllowedOrigins, ","),
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(metrics.Middleware) // Prometheus instrumentation
	r.Use(chimiddleware.Compress(5))

	// System endpoints (no auth, no rate limit)
	// Note: readyHandler is wired after plugin startup (line ~240) so it has
	// access to the resolved FSStore for write-verify health check.
	r.Get("/health", healthHandler)
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
			// axe:wire:route
		})
	})

	// ── WebSocket endpoint ────────────────────────────────────────────────────
	// GET /ws — auth-protected, max 5 conns/user.
	// Token accepted via:
	//   Authorization: Bearer <jwt>   (header)
	//   ?token=<jwt>                  (query param — browser WS clients)
	r.With(
		ws.WSAuth(jwtSvc, cacheClient, wsTracker, ws.WithMaxConnsPerUser(5)),
	).Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		client, err := wsHub.UpgradeAuthenticated(w, r, wsTracker)
		if err != nil {
			log.Warn("ws: upgrade failed", "error", err)
			return
		}
		wsHub.Join(client, "lobby")
		client.OnMessage(func(msg []byte) {
			_ = wsHub.Broadcast(r.Context(), "lobby", msg)
		})
	})
	// axe:wire:ws-route

	// ── Plugin Leader ────────────────────────────────────────────────────────
	pluginApp := plugin.NewApp(plugin.AppConfig{
		Router: r,
		Config: cfg,
		Logger: log,
		DB:     sqlDB,
		Cache:  cacheClient,
		Hub:    wsHub,
	})

	if err := setup.RegisterPlugins(context.Background(), pluginApp, cfg); err != nil {
		log.Error("plugin setup failed", "error", err)
		os.Exit(1)
	}

	if err := pluginApp.Start(context.Background()); err != nil {
		log.Error("plugin startup failed", "error", err)
		os.Exit(1)
	}

	// ── Hook Leader ─────────────────────────────────────────────────────────
	hook.RegisterAll(pluginApp.Events)

	// Resolve FSStore for /ready health check AFTER plugins are started.
	// Uses write-verify cycle (write+read+delete sentinel file) instead of
	// os.Stat — catches stale/read-only FUSE mounts that Stat would miss.
	var storageHealthFn func(context.Context) error
	if fsStore, ok := plugin.Resolve[storage.Store](pluginApp, storage.ServiceKey); ok {
		if hc, ok := fsStore.(interface {
			HealthCheck(context.Context) error
		}); ok {
			storageHealthFn = hc.HealthCheck
		}
	}
	r.Get("/ready", readyHandler(sqlDB, cacheClient, storageHealthFn))

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

	go wsHub.Run(ctx)

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
		"ws", fmt.Sprintf(":%d/ws", cfg.ServerPort),
	)

	<-quit
	log.Info("shutdown signal received — draining...")

	cancelBg() // stop outbox poller + ws hub

	wsHub.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown plugins (LIFO order) before HTTP server
	if err := pluginApp.Shutdown(shutdownCtx); err != nil {
		log.Error("plugin shutdown error", "error", err)
	}

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

func readyHandler(sqlDB *sql.DB, cacheClient *cache.Client, storageHealthFn func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"status": "ok"}

		if err := sqlDB.PingContext(r.Context()); err != nil {
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

		// Storage mount health check — write-verify cycle (not just os.Stat).
		// Catches stale FUSE mounts and read-only JuiceFS conditions.
		if storageHealthFn != nil {
			if err := storageHealthFn(r.Context()); err != nil {
				resp["storage"] = "error: " + err.Error()
				resp["status"] = "degraded"
			} else {
				resp["storage"] = "ok"
			}
		}

		status := http.StatusOK
		if resp["status"] == "degraded" {
			status = http.StatusServiceUnavailable
		}
		middleware.WriteJSON(w, status, resp)
	}
}
