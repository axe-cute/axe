package s3store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/events"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
	storagePlugin "github.com/axe-cute/axe/pkg/plugin/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_MissingBucket(t *testing.T) {
	_, err := New(Config{AccessKey: "ak", SecretKey: "sk"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bucket")
}

func TestNew_MissingAccessKey(t *testing.T) {
	_, err := New(Config{Bucket: "my-bucket", SecretKey: "sk"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AccessKey")
}

func TestNew_MissingSecretKey(t *testing.T) {
	_, err := New(Config{Bucket: "my-bucket", AccessKey: "ak"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SecretKey")
}

func TestNew_ValidConfig(t *testing.T) {
	p, err := New(Config{Bucket: "my-bucket", AccessKey: "ak", SecretKey: "sk"})
	require.NoError(t, err)
	assert.Equal(t, "storage:s3", p.Name())
}

func TestNew_DefaultRegion(t *testing.T) {
	p, err := New(Config{Bucket: "b", AccessKey: "a", SecretKey: "s"})
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", p.cfg.Region)
}

func TestNew_DefaultMaxFileSize(t *testing.T) {
	p, err := New(Config{Bucket: "b", AccessKey: "a", SecretKey: "s"})
	require.NoError(t, err)
	assert.Equal(t, int64(100*1024*1024), p.cfg.MaxFileSize)
}

// ── Plugin lifecycle ──────────────────────────────────────────────────────────

func TestRegister_ProvidesStoreUnder_StorageServiceKey(t *testing.T) {
	p := mustNewPlugin(t, "http://mock.local")
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	// Must be resolvable via storage.ServiceKey — same as FSStore.
	store, ok := plugin.Resolve[storagePlugin.Store](app, storagePlugin.ServiceKey)
	require.True(t, ok, "S3 plugin must register under storage.ServiceKey")
	assert.NotNil(t, store)
}

func TestShutdown_NoError(t *testing.T) {
	p := mustNewPlugin(t, "http://mock.local")
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Upload ────────────────────────────────────────────────────────────────────

func TestUpload_Success(t *testing.T) {
	srv := newMockS3Server(t, http.StatusOK, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	result, err := p.Upload(t.Context(), "2024/01/photo.jpg",
		strings.NewReader("img-data"), 8, "image/jpeg")
	require.NoError(t, err)
	assert.Equal(t, "2024/01/photo.jpg", result.Key)
	assert.Equal(t, int64(8), result.Size)
	assert.Equal(t, "image/jpeg", result.ContentType)
	assert.NotEmpty(t, result.URL)
}

func TestUpload_FileTooLarge(t *testing.T) {
	srv := newMockS3Server(t, http.StatusOK, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	p.cfg.MaxFileSize = 4 // 4 bytes max
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	_, err := p.Upload(t.Context(), "big.bin",
		strings.NewReader("12345"), 5, "application/octet-stream")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max")
}

func TestUpload_PublishesStorageEvent(t *testing.T) {
	srv := newMockS3Server(t, http.StatusOK, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	bus := events.NewInProcessBus(nil)
	p.events = bus

	uploaded := make(chan events.Event, 1)
	bus.Subscribe(events.TopicStorageUploaded, func(_ context.Context, e events.Event) error {
		uploaded <- e
		return nil
	})

	_, err := p.Upload(t.Context(), "photo.jpg", strings.NewReader("x"), 1, "image/jpeg")
	require.NoError(t, err)

	select {
	case e := <-uploaded:
		assert.Equal(t, events.TopicStorageUploaded, e.Topic)
		assert.Equal(t, "storage:s3", e.Meta.PluginSource)
		assert.Equal(t, "photo.jpg", e.Payload["key"])
	default:
		t.Fatal("storage.uploaded event not published")
	}
}

func TestUpload_S3Error(t *testing.T) {
	srv := newMockS3Server(t, http.StatusForbidden,
		`<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	_, err := p.Upload(t.Context(), "key.jpg", strings.NewReader("data"), 4, "image/jpeg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "s3")
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_Success(t *testing.T) {
	srv := newMockS3Server(t, http.StatusNoContent, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	err := p.Delete(t.Context(), "photo.jpg")
	require.NoError(t, err)
}

func TestDelete_NotFound_IsIgnored(t *testing.T) {
	srv := newMockS3Server(t, http.StatusNotFound, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	// 404 on delete must not return an error (object already gone = success).
	err := p.Delete(t.Context(), "ghost.jpg")
	require.NoError(t, err)
}

// ── Open ──────────────────────────────────────────────────────────────────────

func TestOpen_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("file-content")) //nolint:errcheck
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	rc, err := p.Open(t.Context(), "photo.jpg")
	require.NoError(t, err)
	defer rc.Close()

	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "file-content", string(body))
}

func TestOpen_NotFound(t *testing.T) {
	srv := newMockS3Server(t, http.StatusNotFound, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	_, err := p.Open(t.Context(), "missing.jpg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ── Exists ────────────────────────────────────────────────────────────────────

func TestExists_True(t *testing.T) {
	srv := newMockS3Server(t, http.StatusOK, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	exists, err := p.Exists(t.Context(), "photo.jpg")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestExists_False(t *testing.T) {
	srv := newMockS3Server(t, http.StatusNotFound, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	exists, err := p.Exists(t.Context(), "missing.jpg")
	require.NoError(t, err)
	assert.False(t, exists)
}

// ── URL ───────────────────────────────────────────────────────────────────────

func TestURL_WithURLPrefix(t *testing.T) {
	p, _ := New(Config{
		Bucket: "b", AccessKey: "a", SecretKey: "s",
		URLPrefix: "https://cdn.example.com",
	})
	assert.Equal(t, "https://cdn.example.com/photo.jpg", p.URL("photo.jpg"))
}

func TestURL_WithoutURLPrefix(t *testing.T) {
	p, _ := New(Config{
		Bucket: "b", Region: "us-east-1", AccessKey: "a", SecretKey: "s",
	})
	p.cfg.URLPrefix = "" // clear forced default
	url := p.URL("photo.jpg")
	assert.Contains(t, url, "b")
	assert.Contains(t, url, "photo.jpg")
}

// ── ServiceKey ────────────────────────────────────────────────────────────────

func TestServiceKey_MatchesStorageServiceKey(t *testing.T) {
	// Critical: S3 must register under the same key as FSStore.
	assert.Equal(t, storagePlugin.ServiceKey, ServiceKey)
}

// ── HealthCheck ───────────────────────────────────────────────────────────────

func TestHealthCheck_ReachableBucket(t *testing.T) {
	// 404 on .axe-health sentinel = bucket reachable, object just doesn't exist.
	srv := newMockS3Server(t, http.StatusNotFound, "")
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	err := p.HealthCheck()
	require.NoError(t, err, "404 on sentinel means bucket is reachable → healthy")
}

func TestHealthCheck_UnreachableBucket(t *testing.T) {
	p := mustNewPlugin(t, "http://127.0.0.1:1") // refuses connection
	err := p.HealthCheck()
	require.Error(t, err)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func mustNewPlugin(t *testing.T, endpoint string) *Plugin {
	t.Helper()
	p, err := New(Config{
		Bucket:    "test-bucket",
		Region:    "us-east-1",
		AccessKey: "ak",
		SecretKey: "sk",
		Endpoint:  endpoint,
		URLPrefix: endpoint + "/test-bucket",
	})
	require.NoError(t, err)
	return p
}

func newMockS3Server(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		if body != "" {
			w.Write([]byte(body)) //nolint:errcheck
		}
	}))
}
