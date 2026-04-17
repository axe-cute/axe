package storage

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/axe-cute/axe/config"
	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	return Config{
		Backend:     "local",
		MountPath:   dir,
		MaxFileSize: 1024, // 1KB for tests
		URLPrefix:   "/upload",
	}
}

func TestFSStoreUpload(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	content := []byte("hello world")
	result, err := store.Upload(context.Background(), "test/file.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	assert.Equal(t, "test/file.txt", result.Key)
	assert.Equal(t, "/upload/test/file.txt", result.URL)
	assert.Equal(t, int64(len(content)), result.Size)
	assert.Equal(t, "text/plain", result.ContentType)

	// Verify file exists on disk
	fullPath := filepath.Join(cfg.MountPath, "test", "file.txt")
	data, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

func TestFSStoreDelete(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	content := []byte("to be deleted")
	_, err = store.Upload(context.Background(), "del.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	err = store.Delete(context.Background(), "del.txt")
	require.NoError(t, err)

	// Verify file is gone
	exists, err := store.Exists(context.Background(), "del.txt")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestFSStoreDeleteNotFound(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	err = store.Delete(context.Background(), "nonexistent.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFSStoreExists(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	// File does not exist yet
	exists, err := store.Exists(context.Background(), "check.txt")
	require.NoError(t, err)
	assert.False(t, exists)

	// Upload the file
	content := []byte("exists check")
	_, err = store.Upload(context.Background(), "check.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	// Now it should exist
	exists, err = store.Exists(context.Background(), "check.txt")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestFSStoreOpen(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	content := []byte("read me back")
	_, err = store.Upload(context.Background(), "read.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	reader, err := store.Open(context.Background(), "read.txt")
	require.NoError(t, err)
	defer reader.Close()

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestFSStoreOpenNotFound(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	_, err = store.Open(context.Background(), "no-such-file.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUploadMaxSize(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxFileSize = 10 // 10 bytes max
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	// 20 bytes exceeds the 10-byte limit
	content := []byte("this is way too big!")
	_, err = store.Upload(context.Background(), "big.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max")
}

func TestUploadAllowedTypes(t *testing.T) {
	cfg := testConfig(t)
	cfg.AllowedTypes = []string{"image/png", "image/jpeg"}
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	// Allowed type
	content := []byte("fake png")
	_, err = store.Upload(context.Background(), "img.png", bytes.NewReader(content), int64(len(content)), "image/png")
	require.NoError(t, err)

	// Disallowed type
	_, err = store.Upload(context.Background(), "doc.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

func TestUploadKeyFormat(t *testing.T) {
	key := KeyForFile("abc123.png")

	// Should match YYYY/MM/DD/abc123.png
	parts := strings.Split(key, "/")
	require.Len(t, parts, 4, "key should have 4 path segments")
	assert.Len(t, parts[0], 4, "year should be 4 digits")
	assert.Len(t, parts[1], 2, "month should be 2 digits")
	assert.Len(t, parts[2], 2, "day should be 2 digits")
	assert.Equal(t, "abc123.png", parts[3])
}

func TestUploadCreatesSubdirectories(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	content := []byte("nested file")
	_, err = store.Upload(context.Background(), "a/b/c/nested.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	exists, err := store.Exists(context.Background(), "a/b/c/nested.txt")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestFSStoreURL(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	url := store.URL("2026/04/16/abc.png")
	assert.Equal(t, "/upload/2026/04/16/abc.png", url)
}

// ── Path Traversal Tests ──────────────────────────────────────────────────────

func TestSafePath_TraversalBlocked(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	traversalKeys := []string{
		"../../etc/passwd",
		"../../../etc/shadow",
		"foo/../../..",
		"../secret.txt",
		"a/b/../../../../etc/hosts",
	}

	for _, key := range traversalKeys {
		t.Run(key, func(t *testing.T) {
			content := []byte("exploit")
			_, err := store.Upload(context.Background(), key,
				bytes.NewReader(content), int64(len(content)), "text/plain")
			require.Error(t, err, "key %q should be blocked", key)
			assert.Contains(t, err.Error(), "path traversal")
		})
	}
}

func TestSafePath_NormalKeysOK(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	validKeys := []string{
		"test.txt",
		"2026/04/17/photo.png",
		"a/b/c/deep.pdf",
		"user-uploads/avatar.jpg",
	}

	for _, key := range validKeys {
		t.Run(key, func(t *testing.T) {
			content := []byte("ok")
			_, err := store.Upload(context.Background(), key,
				bytes.NewReader(content), int64(len(content)), "text/plain")
			require.NoError(t, err, "key %q should be allowed", key)
		})
	}
}

func TestSafePath_DeleteTraversalBlocked(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	err = store.Delete(context.Background(), "../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestSafePath_OpenTraversalBlocked(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	_, err = store.Open(context.Background(), "../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestSafePath_ExistsTraversalBlocked(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	_, err = store.Exists(context.Background(), "../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

// ── Plugin Tests ──────────────────────────────────────────────────────────────

func TestPluginRegister(t *testing.T) {
	cfg := testConfig(t)

	p := New(cfg)
	assert.Equal(t, "storage", p.Name())

	r := chi.NewRouter()
	app := plugin.NewApp(plugin.AppConfig{
		Router: r,
		Logger: slog.Default(),
		Config: &config.Config{
			JWTSecret:                   "test-secret-256-bits-long-enough",
			JWTAccessTokenExpiryMinutes: 15,
			JWTRefreshTokenExpiryDays:   7,
		},
	})

	err := p.Register(context.Background(), app)
	require.NoError(t, err)

	// Verify Store is provided to service locator
	store, ok := plugin.Resolve[Store](app, ServiceKey)
	assert.True(t, ok)
	assert.NotNil(t, store)
}

func TestPluginShutdown(t *testing.T) {
	cfg := testConfig(t)
	p := New(cfg)

	// Shutdown before register should not panic
	err := p.Shutdown(context.Background())
	require.NoError(t, err)
}

// ── HealthCheck Tests ─────────────────────────────────────────────────────────

func TestHealthCheck_WritableMount(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	err = store.HealthCheck()
	assert.NoError(t, err, "HealthCheck should succeed on a writable temp directory")
}

func TestHealthCheck_SentinelFileCleanedUp(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	err = store.HealthCheck()
	require.NoError(t, err)

	// Sentinel file must be removed after successful check
	sentinelPath := cfg.MountPath + "/.axe-health-check"
	_, statErr := os.Stat(sentinelPath)
	assert.True(t, os.IsNotExist(statErr), "sentinel file should be deleted after HealthCheck")
}

func TestHealthCheck_ReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test read-only dir as root")
	}

	dir := t.TempDir()
	// Make the directory read-only
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	store := &FSStore{basePath: dir, maxSize: 10 * 1024 * 1024, urlPrefix: "/upload"}

	err := store.HealthCheck()
	assert.Error(t, err, "HealthCheck should fail on read-only directory")
	assert.Contains(t, err.Error(), "storage:")
}

// ── wrapFSError Tests ─────────────────────────────────────────────────────────

func TestWrapFSError_Nil(t *testing.T) {
	err := wrapFSError("op", nil)
	assert.NoError(t, err)
}

func TestWrapFSError_ENOTCONN(t *testing.T) {
	err := wrapFSError("open", syscall.ENOTCONN)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mount unavailable")
	assert.Contains(t, err.Error(), "JuiceFS")
	// Must NOT expose raw syscall message to callers
	assert.NotContains(t, err.Error(), "transport endpoint")
}

func TestWrapFSError_EIO(t *testing.T) {
	err := wrapFSError("delete", syscall.EIO)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mount unavailable")
}

func TestWrapFSError_EROFS(t *testing.T) {
	err := wrapFSError("write", syscall.EROFS)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only")
}

func TestWrapFSError_ENOSPC(t *testing.T) {
	err := wrapFSError("write", syscall.ENOSPC)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no space left")
}

func TestWrapFSError_EACCES(t *testing.T) {
	err := wrapFSError("open", syscall.EACCES)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestWrapFSError_Generic(t *testing.T) {
	err := wrapFSError("stat", syscall.ENOENT)
	require.Error(t, err)
	// Generic errors are wrapped with the op name
	assert.Contains(t, err.Error(), "storage: stat:")
}

// ── fsync on Upload ───────────────────────────────────────────────────────────

func TestUpload_DataReadableAfterClose(t *testing.T) {
	// This tests that data written by Upload is immediately readable
	// (fsync ensures the FUSE/OS buffers are flushed before we return)
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	content := []byte("critical data that must survive a crash")
	result, err := store.Upload(context.Background(), "critical.txt",
		bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), result.Size)

	// Read back immediately — data must be durable
	reader, err := store.Open(context.Background(), "critical.txt")
	require.NoError(t, err)
	defer reader.Close()

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, got, "data must be fully durable after Upload returns")
}
