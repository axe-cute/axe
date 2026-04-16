// Package config loads application configuration from environment variables.
// It uses cleanenv for struct binding and validation.
//
// Usage:
//
//	cfg, err := config.Load()
//	if err != nil {
//	    log.Fatal("failed to load config:", err)
//	}
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config holds all application configuration.
// Fields are loaded from environment variables (see `env` tags).
// Fields marked `env-required:"true"` will fail validation if unset.
type Config struct {
	// Server
	ServerPort  int    `env:"SERVER_PORT"  env-default:"8080"`
	Environment string `env:"ENVIRONMENT"  env-default:"development"`
	LogLevel    string `env:"LOG_LEVEL"    env-default:"info"`

	// Database
	DBDriver                    string `env:"DB_DRIVER"                          env-default:"postgres"`
	DatabaseURL                string `env:"DATABASE_URL"                       env-required:"true"`
	DatabaseMaxOpenConns       int    `env:"DATABASE_MAX_OPEN_CONNS"            env-default:"25"`
	DatabaseMaxIdleConns       int    `env:"DATABASE_MAX_IDLE_CONNS"            env-default:"5"`
	DatabaseConnMaxLifetimeMins int   `env:"DATABASE_CONN_MAX_LIFETIME_MINUTES" env-default:"30"`

	// Redis
	RedisURL        string `env:"REDIS_URL"         env-default:"redis://localhost:6379/0"`
	RedisMaxRetries int    `env:"REDIS_MAX_RETRIES" env-default:"3"`

	// Auth
	JWTSecret                   string `env:"JWT_SECRET"                        env-required:"true"`
	JWTAccessTokenExpiryMinutes int    `env:"JWT_ACCESS_TOKEN_EXPIRY_MINUTES"   env-default:"15"`
	JWTRefreshTokenExpiryDays   int    `env:"JWT_REFRESH_TOKEN_EXPIRY_DAYS"     env-default:"7"`

	// Asynq
	AsynqConcurrency    int    `env:"ASYNQ_CONCURRENCY"     env-default:"10"`
	AsynqQueueDefault   string `env:"ASYNQ_QUEUE_DEFAULT"   env-default:"default"`
	AsynqQueueCritical  string `env:"ASYNQ_QUEUE_CRITICAL"  env-default:"critical"`

	// WebSocket Hub
	// HubAdapter selects the pub/sub backend for multi-instance WebSocket broadcasting.
	// "memory" (default) is suitable for single-instance deployments.
	// "redis" enables cross-instance broadcast via Redis Pub/Sub.
	HubAdapter string `env:"HUB_ADAPTER" env-default:"memory"`

	// Observability (optional)
	OTELEndpoint    string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" env-default:""`
	OTELServiceName string `env:"OTEL_SERVICE_NAME"           env-default:"axe"`
}

// Load reads configuration from environment variables and validates it.
// It optionally reads a .env file in the current directory (via cleanenv).
func Load() (*Config, error) {
	cfg := &Config{}

	if err := cleanenv.ReadEnv(cfg); err != nil {
		return nil, fmt.Errorf("config: load from env: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("config: validation: %w", err)
	}

	return cfg, nil
}

// LoadFromFile reads configuration from both a .env file and environment variables.
// Environment variables override file values.
func LoadFromFile(path string) (*Config, error) {
	cfg := &Config{}

	if err := cleanenv.ReadConfig(path, cfg); err != nil {
		return nil, fmt.Errorf("config: load from file %q: %w", path, err)
	}

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("config: validation: %w", err)
	}

	return cfg, nil
}

// IsProduction reports whether the application is running in production mode.
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

// IsDevelopment reports whether the application is running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development"
}

// AccessTokenTTL returns the access token expiry as a time.Duration.
func (c *Config) AccessTokenTTL() time.Duration {
	return time.Duration(c.JWTAccessTokenExpiryMinutes) * time.Minute
}

// RefreshTokenTTL returns the refresh token expiry as a time.Duration.
func (c *Config) RefreshTokenTTL() time.Duration {
	return time.Duration(c.JWTRefreshTokenExpiryDays) * 24 * time.Hour
}

// RedisAddr extracts host:port from REDIS_URL (strips redis:// scheme).
// e.g. "redis://localhost:6379/0" → "localhost:6379"
func (c *Config) RedisAddr() string {
	u := c.RedisURL
	u = strings.TrimPrefix(u, "redis://")
	u = strings.TrimPrefix(u, "rediss://")
	if idx := strings.LastIndex(u, "/"); idx != -1 {
		u = u[:idx] // strip /db number
	}
	return u
}

func validate(cfg *Config) error {
	if cfg.ServerPort < 1 || cfg.ServerPort > 65535 {
		return fmt.Errorf("SERVER_PORT must be between 1 and 65535, got %d", cfg.ServerPort)
	}

	validEnvs := map[string]bool{"development": true, "staging": true, "production": true}
	if !validEnvs[cfg.Environment] {
		return fmt.Errorf("ENVIRONMENT must be one of [development, staging, production], got %q", cfg.Environment)
	}

	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[cfg.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be one of [debug, info, warn, error], got %q", cfg.LogLevel)
	}

	if cfg.JWTAccessTokenExpiryMinutes < 1 {
		return fmt.Errorf("JWT_ACCESS_TOKEN_EXPIRY_MINUTES must be >= 1")
	}

	validDrivers := map[string]bool{"postgres": true, "mysql": true, "sqlite3": true}
	if !validDrivers[cfg.DBDriver] {
		return fmt.Errorf("DB_DRIVER must be one of [postgres, mysql, sqlite3], got %q", cfg.DBDriver)
	}

	validHubAdapters := map[string]bool{"memory": true, "redis": true}
	if !validHubAdapters[cfg.HubAdapter] {
		return fmt.Errorf("HUB_ADAPTER must be one of [memory, redis], got %q", cfg.HubAdapter)
	}

	return nil
}
