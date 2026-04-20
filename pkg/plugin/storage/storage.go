// Package storage provides a file storage plugin for axe.
//
// It supports both local filesystem directories (development) and JuiceFS
// mount points (production). Since JuiceFS is POSIX-compliant, both backends
// use the same [FSStore] adapter — standard Go os package operations.
//
// Usage:
//
//	app.Use(storage.New(storage.Config{
//	    Backend:   "local",        // or "juicefs"
//	    MountPath: "./uploads",    // or "/mnt/jfs/uploads"
//	}))
//
// Other plugins can resolve the Store via the typed service locator:
//
//	store := plugin.MustResolve[storage.Store](app, storage.ServiceKey)
package storage

import (
	"context"
	"io"
	"time"
)

// ServiceKey is the typed service locator key for [Store].
const ServiceKey = "storage"

// Store abstracts file storage operations.
// Both local dev directories and JuiceFS mount points satisfy this interface.
type Store interface {
	// Upload stores a file and returns metadata about the stored object.
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) (*Result, error)

	// Delete removes a file by key.
	Delete(ctx context.Context, key string) error

	// Open returns a reader for the file content.
	Open(ctx context.Context, key string) (io.ReadCloser, error)

	// Exists checks whether a file exists.
	Exists(ctx context.Context, key string) (bool, error)

	// URL returns the serving URL/path for a given key.
	URL(key string) string

	// HealthCheck performs a write→read→delete probe to verify the mount is
	// fully operational. Used by [plugin.HealthChecker] aggregation (/ready).
	HealthCheck(ctx context.Context) error
}

// Result holds metadata about a stored file.
type Result struct {
	Key         string `json:"key"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// Config configures the storage plugin.
type Config struct {
	// Backend selects the storage adapter: "local" or "juicefs".
	// Both use the same FSStore internally — the distinction is for logging/metrics.
	Backend string

	// MountPath is the base directory for file storage.
	// For local dev: "./uploads"
	// For JuiceFS:   "/mnt/jfs/axe-uploads"
	MountPath string

	// MaxFileSize is the maximum allowed upload size in bytes.
	// Default: 10MB (10_485_760).
	MaxFileSize int64

	// AllowedTypes restricts uploads to specific MIME types.
	// Empty means all types are allowed.
	// Example: ["image/png", "image/jpeg", "application/pdf"]
	AllowedTypes []string

	// URLPrefix is the path prefix for serving files via HTTP.
	// Default: "/upload"
	URLPrefix string

	// RequireAuth controls JWT authentication on file serving (GET) routes.
	// Write operations (POST upload, DELETE) ALWAYS require JWT — this is not optional.
	// When false (default): GET serves files publicly, writes need JWT.
	// When true: ALL routes (read + write) require JWT (for private/internal files).
	RequireAuth bool
}

// defaults fills in zero-value config fields with sensible defaults.
func (c *Config) defaults() {
	if c.MountPath == "" {
		c.MountPath = "./uploads"
	}
	if c.MaxFileSize <= 0 {
		c.MaxFileSize = 10 * 1024 * 1024 // 10MB
	}
	if c.URLPrefix == "" {
		c.URLPrefix = "/upload"
	}
	if c.Backend == "" {
		c.Backend = "local"
	}
}

// KeyForFile generates a storage key in the format: YYYY/MM/DD/{name}
func KeyForFile(name string) string {
	now := time.Now().UTC()
	return now.Format("2006/01/02") + "/" + name
}
