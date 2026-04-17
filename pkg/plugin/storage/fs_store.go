package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// FSStore implements [Store] using standard filesystem operations.
// It works identically whether the base path is a local directory or a
// JuiceFS POSIX mount point — no special SDK required.
type FSStore struct {
	basePath  string
	maxSize   int64
	allowed   map[string]bool
	urlPrefix string
}

// NewFSStore creates a new filesystem-backed store.
// It creates the base directory if it does not exist.
func NewFSStore(cfg Config) (*FSStore, error) {
	cfg.defaults()

	if err := os.MkdirAll(cfg.MountPath, 0o755); err != nil {
		return nil, fmt.Errorf("storage: create base dir %q: %w", cfg.MountPath, err)
	}

	allowed := make(map[string]bool, len(cfg.AllowedTypes))
	for _, t := range cfg.AllowedTypes {
		allowed[strings.ToLower(t)] = true
	}

	return &FSStore{
		basePath:  cfg.MountPath,
		maxSize:   cfg.MaxFileSize,
		allowed:   allowed,
		urlPrefix: cfg.URLPrefix,
	}, nil
}

// safePath ensures the resolved path stays within basePath.
// Prevents path traversal attacks (e.g. key = "../../etc/passwd").
func (s *FSStore) safePath(key string) (string, error) {
	fullPath := filepath.Join(s.basePath, filepath.FromSlash(key))
	absBase, err := filepath.Abs(s.basePath)
	if err != nil {
		return "", fmt.Errorf("storage: resolve base: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("storage: resolve path: %w", err)
	}
	// absPath must be inside absBase (or equal to it)
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return "", fmt.Errorf("storage: invalid key %q (path traversal)", key)
	}
	return fullPath, nil
}

// Upload stores a file on the filesystem.
// It enforces max file size and allowed content types.
func (s *FSStore) Upload(_ context.Context, key string, r io.Reader, size int64, contentType string) (*Result, error) {
	// Validate content type
	if len(s.allowed) > 0 && !s.allowed[strings.ToLower(contentType)] {
		return nil, fmt.Errorf("storage: content type %q not allowed", contentType)
	}

	// Validate size
	if size > s.maxSize {
		return nil, fmt.Errorf("storage: file size %d exceeds max %d bytes", size, s.maxSize)
	}

	fullPath, err := s.safePath(key)
	if err != nil {
		return nil, err
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return nil, fmt.Errorf("storage: mkdir: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("storage: create file: %w", err)
	}
	defer f.Close()

	// Copy with size limit enforcement
	limited := io.LimitReader(r, s.maxSize+1)
	written, err := io.Copy(f, limited)
	if err != nil {
		// Clean up partial file on error
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("storage: write: %w", err)
	}

	if written > s.maxSize {
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("storage: file size exceeds max %d bytes", s.maxSize)
	}

	// Flush FUSE/OS buffers before close to prevent silent data loss on crash.
	// Critical for JuiceFS — the FUSE client may buffer writes asynchronously.
	if err := f.Sync(); err != nil {
		_ = os.Remove(fullPath)
		return nil, wrapFSError("fsync", err)
	}

	return &Result{
		Key:         key,
		URL:         s.URL(key),
		Size:        written,
		ContentType: contentType,
	}, nil
}

// Delete removes a file from the filesystem.
func (s *FSStore) Delete(_ context.Context, key string) error {
	fullPath, err := s.safePath(key)
	if err != nil {
		return err
	}

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("storage: file %q not found", key)
		}
		return wrapFSError("delete", err)
	}

	return nil
}

// Open returns a reader for the file content.
func (s *FSStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath, err := s.safePath(key)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("storage: file %q not found", key)
		}
		return nil, wrapFSError("open", err)
	}

	return f, nil
}

// Exists checks whether a file exists on the filesystem.
func (s *FSStore) Exists(_ context.Context, key string) (bool, error) {
	fullPath, err := s.safePath(key)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("storage: stat: %w", err)
	}

	return true, nil
}

// URL returns the serving URL path for a given key.
func (s *FSStore) URL(key string) string {
	return s.urlPrefix + "/" + key
}

// HealthCheck performs a write→read→delete cycle on the storage mount.
// This verifies the path is writable — os.Stat alone is insufficient because
// a stale or read-only FUSE mount (e.g. JuiceFS with exceeded quota) passes
// Stat but fails on writes.
func (s *FSStore) HealthCheck() error {
	sentinel := filepath.Join(s.basePath, ".axe-health-check")

	if err := os.WriteFile(sentinel, []byte("ok"), 0o644); err != nil {
		return wrapFSError("health-check write", err)
	}
	defer os.Remove(sentinel)

	data, err := os.ReadFile(sentinel)
	if err != nil {
		return wrapFSError("health-check read", err)
	}
	if string(data) != "ok" {
		return fmt.Errorf("storage: health-check: read-back value mismatch")
	}
	return nil
}

// wrapFSError translates low-level OS/FUSE errors into storage-layer errors.
// It prevents raw syscall details (e.g. "transport endpoint is not connected")
// from leaking into HTTP responses. A returned error includes [storage: op: ...]
// prefix that callers can match on.
func wrapFSError(op string, err error) error {
	if err == nil {
		return nil
	}
	// FUSE mount is disconnected or transport broken (JuiceFS crash/restart)
	if errors.Is(err, syscall.ENOTCONN) || errors.Is(err, syscall.EIO) {
		return fmt.Errorf("storage: %s: mount unavailable (check JuiceFS connection)", op)
	}
	// Read-only mount (quota exceeded or intentional RO)
	if errors.Is(err, syscall.EROFS) {
		return fmt.Errorf("storage: %s: mount is read-only", op)
	}
	// No space left (disk full or JuiceFS quota)
	if errors.Is(err, syscall.ENOSPC) {
		return fmt.Errorf("storage: %s: no space left on mount", op)
	}
	// Permission denied
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("storage: %s: permission denied", op)
	}
	return fmt.Errorf("storage: %s: %w", op, err)
}
