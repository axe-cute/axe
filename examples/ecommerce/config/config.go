// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config holds all application configuration.
type Config struct {
	// Server
	ServerPort  int    `env:"SERVER_PORT"  env-default:"8080"`
	Environment string `env:"ENVIRONMENT"  env-default:"development"`
	LogLevel    string `env:"LOG_LEVEL"    env-default:"info"`

	// Database
	DBDriver                    string `env:"DB_DRIVER"                          env-default:"postgres"`
	DatabaseURL                 string `env:"DATABASE_URL"                       env-required:"true"`
	DatabaseMaxOpenConns        int    `env:"DATABASE_MAX_OPEN_CONNS"            env-default:"25"`
	DatabaseMaxIdleConns        int    `env:"DATABASE_MAX_IDLE_CONNS"            env-default:"5"`
	DatabaseConnMaxLifetimeMins int    `env:"DATABASE_CONN_MAX_LIFETIME_MINUTES" env-default:"30"`

	// Redis
	RedisURL        string `env:"REDIS_URL"         env-default:"redis://localhost:6379/0"`
	RedisMaxRetries int    `env:"REDIS_MAX_RETRIES" env-default:"3"`

	// Auth
	JWTSecret                   string `env:"JWT_SECRET"                      env-required:"true"`
	JWTAccessTokenExpiryMinutes int    `env:"JWT_ACCESS_TOKEN_EXPIRY_MINUTES" env-default:"15"`
	JWTRefreshTokenExpiryDays   int    `env:"JWT_REFRESH_TOKEN_EXPIRY_DAYS"   env-default:"7"`

	// Asynq
	AsynqConcurrency   int    `env:"ASYNQ_CONCURRENCY"    env-default:"10"`
	AsynqQueueDefault  string `env:"ASYNQ_QUEUE_DEFAULT"  env-default:"default"`
	AsynqQueueCritical string `env:"ASYNQ_QUEUE_CRITICAL" env-default:"critical"`

	// Observability
	OTELEndpoint    string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" env-default:""`
	OTELServiceName string `env:"OTEL_SERVICE_NAME"           env-default:"app"`

	// Storage
	StorageBackend     string `env:"STORAGE_BACKEND"       env-default:"local"`
	StorageMountPath   string `env:"STORAGE_MOUNT_PATH"    env-default:"./uploads"`
	StorageMaxFileSize int64  `env:"STORAGE_MAX_FILE_SIZE" env-default:"10485760"`
	StorageURLPrefix   string `env:"STORAGE_URL_PREFIX"    env-default:"/upload"`

	// axe:plugin:config
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := cleanenv.ReadEnv(cfg); err != nil {
		return nil, fmt.Errorf("config: load from env: %w", err)
	}
	return cfg, nil
}

// LoadFromFile reads configuration from a .env file and environment variables.
func LoadFromFile(path string) (*Config, error) {
	cfg := &Config{}
	if err := cleanenv.ReadConfig(path, cfg); err != nil {
		return nil, fmt.Errorf("config: load from file %q: %w", path, err)
	}
	return cfg, nil
}

// IsProduction reports whether the current environment is production.
func (c *Config) IsProduction() bool  { return c.Environment == "production" }

// IsDevelopment reports whether the current environment is development.
func (c *Config) IsDevelopment() bool { return c.Environment == "development" }

// AccessTokenTTL returns the access token expiry as a duration.
func (c *Config) AccessTokenTTL() time.Duration {
	return time.Duration(c.JWTAccessTokenExpiryMinutes) * time.Minute
}

// RefreshTokenTTL returns the refresh token expiry as a duration.
func (c *Config) RefreshTokenTTL() time.Duration {
	return time.Duration(c.JWTRefreshTokenExpiryDays) * 24 * time.Hour
}

// RedisAddr extracts host:port from REDIS_URL.
func (c *Config) RedisAddr() string {
	u := c.RedisURL
	u = strings.TrimPrefix(u, "redis://")
	u = strings.TrimPrefix(u, "rediss://")
	if idx := strings.LastIndex(u, "/"); idx != -1 {
		u = u[:idx]
	}
	return u
}
