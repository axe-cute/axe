package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/config"
)

// setEnv sets multiple env vars and returns a cleanup function.
func setEnv(t *testing.T, pairs ...string) {
	t.Helper()
	for i := 0; i < len(pairs); i += 2 {
		t.Setenv(pairs[i], pairs[i+1])
	}
}

// requiredEnv sets the minimum required env vars for Load() to succeed.
func requiredEnv(t *testing.T) {
	t.Helper()
	setEnv(t,
		"DATABASE_URL", "postgres://axe:axe@localhost:5432/axe_test",
		"JWT_SECRET", "test-secret-at-least-32-chars-long",
	)
}

// ── Load defaults ─────────────────────────────────────────────────────────────

func TestLoad_Defaults(t *testing.T) {
	requiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.ServerPort)
	assert.Equal(t, "development", cfg.Environment)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "postgres", cfg.DBDriver)
	assert.Equal(t, 25, cfg.DatabaseMaxOpenConns)
	assert.Equal(t, 5, cfg.DatabaseMaxIdleConns)
	assert.Equal(t, 30, cfg.DatabaseConnMaxLifetimeMins)
	assert.Equal(t, 15, cfg.JWTAccessTokenExpiryMinutes)
	assert.Equal(t, 7, cfg.JWTRefreshTokenExpiryDays)
	assert.Equal(t, 10, cfg.AsynqConcurrency)
	assert.Equal(t, "default", cfg.AsynqQueueDefault)
	assert.Equal(t, "critical", cfg.AsynqQueueCritical)
	assert.Equal(t, "memory", cfg.HubAdapter)
	assert.Equal(t, "local", cfg.StorageBackend)
	assert.Equal(t, "./uploads", cfg.StorageMountPath)
	assert.Equal(t, "*", cfg.CORSAllowedOrigins)
	assert.Equal(t, "axe", cfg.OTELServiceName)
}

// ── Load custom values ────────────────────────────────────────────────────────

func TestLoad_CustomValues(t *testing.T) {
	requiredEnv(t)
	setEnv(t,
		"SERVER_PORT", "9090",
		"ENVIRONMENT", "production",
		"LOG_LEVEL", "warn",
		"DB_DRIVER", "mysql",
		"HUB_ADAPTER", "redis",
		"STORAGE_BACKEND", "juicefs",
		"STORAGE_MOUNT_PATH", "/mnt/jfs",
		"CORS_ALLOWED_ORIGINS", "https://example.com", // P0-05: wildcard rejected in production
	)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 9090, cfg.ServerPort)
	assert.Equal(t, "production", cfg.Environment)
	assert.Equal(t, "warn", cfg.LogLevel)
	assert.Equal(t, "mysql", cfg.DBDriver)
	assert.Equal(t, "redis", cfg.HubAdapter)
	assert.Equal(t, "juicefs", cfg.StorageBackend)
	assert.Equal(t, "/mnt/jfs", cfg.StorageMountPath)
}

// ── Required fields ──────────────────────────────────────────────────────────

func TestLoad_MissingRequired_DatabaseURL(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("JWT_SECRET")

	_, err := config.Load()
	assert.Error(t, err)
}

func TestLoad_MissingJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Unsetenv("JWT_SECRET")

	_, err := config.Load()
	assert.Error(t, err)
}

// ── Validation ───────────────────────────────────────────────────────────────

func TestLoad_InvalidPort_TooHigh(t *testing.T) {
	requiredEnv(t)
	t.Setenv("SERVER_PORT", "99999")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SERVER_PORT")
}

func TestLoad_InvalidPort_Zero(t *testing.T) {
	requiredEnv(t)
	t.Setenv("SERVER_PORT", "0")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SERVER_PORT")
}

func TestLoad_InvalidEnvironment(t *testing.T) {
	requiredEnv(t)
	t.Setenv("ENVIRONMENT", "unknown")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ENVIRONMENT")
}

func TestLoad_StagingEnvironment(t *testing.T) {
	requiredEnv(t)
	t.Setenv("ENVIRONMENT", "staging")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "staging", cfg.Environment)
	assert.False(t, cfg.IsProduction())
	assert.False(t, cfg.IsDevelopment())
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	requiredEnv(t)
	t.Setenv("LOG_LEVEL", "verbose")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
}

func TestLoad_AllLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			requiredEnv(t)
			t.Setenv("LOG_LEVEL", level)
			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, level, cfg.LogLevel)
		})
	}
}

func TestLoad_InvalidDBDriver(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DB_DRIVER", "oracle")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_DRIVER")
}

func TestLoad_SQLite3Driver(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DB_DRIVER", "sqlite3")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "sqlite3", cfg.DBDriver)
}

func TestLoad_InvalidHubAdapter(t *testing.T) {
	requiredEnv(t)
	t.Setenv("HUB_ADAPTER", "kafka")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HUB_ADAPTER")
}

func TestLoad_InvalidStorageBackend(t *testing.T) {
	requiredEnv(t)
	t.Setenv("STORAGE_BACKEND", "s3")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "STORAGE_BACKEND")
}

func TestLoad_InvalidJWTExpiry(t *testing.T) {
	requiredEnv(t)
	t.Setenv("JWT_ACCESS_TOKEN_EXPIRY_MINUTES", "0")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_ACCESS_TOKEN_EXPIRY_MINUTES")
}

// ── Helper methods ───────────────────────────────────────────────────────────

func TestConfig_IsProduction(t *testing.T) {
	requiredEnv(t)
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://example.com") // P0-05: wildcard rejected in production
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.IsProduction())
	assert.False(t, cfg.IsDevelopment())
}

func TestConfig_IsDevelopment(t *testing.T) {
	requiredEnv(t)
	t.Setenv("ENVIRONMENT", "development")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.IsProduction())
	assert.True(t, cfg.IsDevelopment())
}

func TestConfig_AccessTokenTTL(t *testing.T) {
	requiredEnv(t)
	t.Setenv("JWT_ACCESS_TOKEN_EXPIRY_MINUTES", "30")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, cfg.AccessTokenTTL())
}

func TestConfig_RefreshTokenTTL(t *testing.T) {
	requiredEnv(t)
	t.Setenv("JWT_REFRESH_TOKEN_EXPIRY_DAYS", "14")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 14*24*time.Hour, cfg.RefreshTokenTTL())
}

func TestConfig_RedisAddr(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"standard", "redis://localhost:6379/0", "localhost:6379"},
		{"no db", "redis://localhost:6379", "localhost:6379"},
		{"tls", "rediss://redis.example.com:6380/2", "redis.example.com:6380"},
		{"with password", "redis://user:pass@myredis:6379/0", "myredis:6379"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{RedisURL: tc.url}
			assert.Equal(t, tc.want, cfg.RedisAddr())
		})
	}
}

// ── Stripe optional fields ───────────────────────────────────────────────────

func TestLoad_StripeOptional(t *testing.T) {
	requiredEnv(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.StripeSecretKey, "Stripe fields are optional")
}

func TestLoad_StripeSet(t *testing.T) {
	requiredEnv(t)
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_123")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "sk_test_123", cfg.StripeSecretKey)
	assert.Equal(t, "whsec_123", cfg.StripeWebhookSecret)
}
