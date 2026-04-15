package config_test

import (
	"os"
	"testing"

	"github.com/axe-go/axe/config"
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

func TestLoad_Defaults(t *testing.T) {
	requiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.ServerPort != 8080 {
		t.Errorf("ServerPort default = %d, want 8080", cfg.ServerPort)
	}
	if cfg.Environment != "development" {
		t.Errorf("Environment default = %q, want \"development\"", cfg.Environment)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want \"info\"", cfg.LogLevel)
	}
	if cfg.DatabaseMaxOpenConns != 25 {
		t.Errorf("DatabaseMaxOpenConns default = %d, want 25", cfg.DatabaseMaxOpenConns)
	}
	if cfg.JWTAccessTokenExpiryMinutes != 15 {
		t.Errorf("JWTAccessTokenExpiryMinutes default = %d, want 15", cfg.JWTAccessTokenExpiryMinutes)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	requiredEnv(t)
	setEnv(t,
		"SERVER_PORT", "9090",
		"ENVIRONMENT", "production",
		"LOG_LEVEL", "warn",
	)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.ServerPort != 9090 {
		t.Errorf("ServerPort = %d, want 9090", cfg.ServerPort)
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want \"production\"", cfg.Environment)
	}
}

func TestLoad_MissingRequired_DatabaseURL(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("JWT_SECRET")

	_, err := config.Load()
	if err == nil {
		t.Error("Load() should fail when DATABASE_URL is missing")
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	requiredEnv(t)
	t.Setenv("SERVER_PORT", "99999")

	_, err := config.Load()
	if err == nil {
		t.Error("Load() should fail validation for port 99999")
	}
}

func TestLoad_InvalidEnvironment(t *testing.T) {
	requiredEnv(t)
	t.Setenv("ENVIRONMENT", "unknown")

	_, err := config.Load()
	if err == nil {
		t.Error("Load() should fail validation for unknown environment")
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	requiredEnv(t)
	t.Setenv("LOG_LEVEL", "verbose")

	_, err := config.Load()
	if err == nil {
		t.Error("Load() should fail validation for invalid log level")
	}
}

func TestConfig_IsProduction(t *testing.T) {
	requiredEnv(t)

	t.Run("production", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "production")
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.IsProduction() {
			t.Error("IsProduction() should be true")
		}
		if cfg.IsDevelopment() {
			t.Error("IsDevelopment() should be false in production")
		}
	})

	t.Run("development", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "development")
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.IsProduction() {
			t.Error("IsProduction() should be false in development")
		}
		if !cfg.IsDevelopment() {
			t.Error("IsDevelopment() should be true")
		}
	})
}
