package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	fullPath := filepath.Join(s.basePath, filepath.FromSlash(key))

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

	return &Result{
		Key:         key,
		URL:         s.URL(key),
		Size:        written,
		ContentType: contentType,
	}, nil
}

// Delete removes a file from the filesystem.
func (s *FSStore) Delete(_ context.Context, key string) error {
	fullPath := filepath.Join(s.basePath, filepath.FromSlash(key))

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("storage: file %q not found", key)
		}
		return fmt.Errorf("storage: delete: %w", err)
	}

	return nil
}

// Open returns a reader for the file content.
func (s *FSStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(s.basePath, filepath.FromSlash(key))

	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("storage: file %q not found", key)
		}
		return nil, fmt.Errorf("storage: open: %w", err)
	}

	return f, nil
}

// Exists checks whether a file exists on the filesystem.
func (s *FSStore) Exists(_ context.Context, key string) (bool, error) {
	fullPath := filepath.Join(s.basePath, filepath.FromSlash(key))

	_, err := os.Stat(fullPath)
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
