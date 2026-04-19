package storage

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── handler tests ────────────────────────────────────────────────────────────

var (
	sharedMetrics     *storageMetrics
	sharedMetricsOnce sync.Once
)

func getTestMetrics() *storageMetrics {
	sharedMetricsOnce.Do(func() {
		sharedMetrics = newMetrics("test")
	})
	return sharedMetrics
}

func newTestHandler(t *testing.T) (*handler, Store) {
	t.Helper()
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)
	return &handler{
		store:   store,
		cfg:     cfg,
		log:     testLogger(),
		metrics: getTestMetrics(),
	}, store
}

// makeMultipart builds a multipart request body with a single "file" field.
func makeMultipart(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body, writer.FormDataContentType()
}

func TestHandleUpload_Success(t *testing.T) {
	h, _ := newTestHandler(t)

	body, contentType := makeMultipart(t, "test.txt", []byte("hello world"))
	req := httptest.NewRequest("POST", "/upload/", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	h.handleUpload(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"key"`)
	assert.Contains(t, w.Body.String(), `"url"`)
	assert.Contains(t, w.Body.String(), `"size":11`)
}

func TestHandleUpload_MissingFileField(t *testing.T) {
	h, _ := newTestHandler(t)

	// Multipart but no "file" field
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("other", "value")
	_ = writer.Close()

	req := httptest.NewRequest("POST", "/upload/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	h.handleUpload(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing")
}

func TestHandleUpload_InvalidMultipart(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("POST", "/upload/", strings.NewReader("not a multipart"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	h.handleUpload(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpload_ContentTypeDetection(t *testing.T) {
	h, _ := newTestHandler(t)

	// PNG magic bytes
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := makeMultipart(t, "image.png", pngHeader)
	req := httptest.NewRequest("POST", "/upload/", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	h.handleUpload(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"content_type":"image/png"`)
}

func TestHandleUpload_AllowedTypesReject(t *testing.T) {
	cfg := testConfig(t)
	cfg.AllowedTypes = []string{"image/png"}
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	h := &handler{
		store:   store,
		cfg:     cfg,
		log:     testLogger(),
		metrics: getTestMetrics(),
	}

	body, contentType := makeMultipart(t, "doc.txt", []byte("hello"))
	req := httptest.NewRequest("POST", "/upload/", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	h.handleUpload(w, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
	assert.Contains(t, w.Body.String(), "not allowed")
}

func TestHandleServe_Success(t *testing.T) {
	h, store := newTestHandler(t)

	// Upload a file first.
	content := []byte("serve me")
	res, err := store.Upload(context.Background(), "serve.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", h.cfg.URLPrefix+"/"+res.Key, nil)
	w := httptest.NewRecorder()

	h.handleServe(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "serve me", w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}

func TestHandleServe_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", h.cfg.URLPrefix+"/nonexistent.txt", nil)
	w := httptest.NewRecorder()

	h.handleServe(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not found")
}

func TestHandleServe_MissingKey(t *testing.T) {
	h, _ := newTestHandler(t)

	// URL doesn't match prefix → extractKey returns ""
	req := httptest.NewRequest("GET", "/wrong-prefix/file.txt", nil)
	w := httptest.NewRecorder()

	h.handleServe(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing")
}

func TestHandleDelete_Success(t *testing.T) {
	h, store := newTestHandler(t)

	content := []byte("delete me")
	res, err := store.Upload(context.Background(), "to-delete.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	req := httptest.NewRequest("DELETE", h.cfg.URLPrefix+"/"+res.Key, nil)
	w := httptest.NewRecorder()

	h.handleDelete(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify file is gone.
	exists, err := store.Exists(context.Background(), res.Key)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestHandleDelete_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("DELETE", h.cfg.URLPrefix+"/nope.txt", nil)
	w := httptest.NewRecorder()

	h.handleDelete(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleDelete_MissingKey(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("DELETE", "/wrong/key", nil)
	w := httptest.NewRecorder()

	h.handleDelete(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── Helper function tests ────────────────────────────────────────────────────

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := &Result{Key: "a/b.txt", URL: "/a/b.txt", Size: 42, ContentType: "text/plain"}
	writeJSON(w, http.StatusCreated, r)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), `"key":"a/b.txt"`)
}

func TestWriteJSON_UnknownType(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, "not a Result")

	assert.Contains(t, w.Body.String(), "unknown type")
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "bad input")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bad input")
}

func TestExtractKey(t *testing.T) {
	h := &handler{cfg: Config{URLPrefix: "/upload"}}

	tests := []struct {
		path string
		want string
	}{
		{"/upload/2026/04/16/file.txt", "2026/04/16/file.txt"},
		{"/upload/a.png", "a.png"},
		{"/wrong/path", ""},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		got := h.extractKey(req)
		assert.Equal(t, tt.want, got, "path: %s", tt.path)
	}
}

func TestAppendJSON_Result(t *testing.T) {
	r := &Result{Key: "k", URL: "/k", Size: 5, ContentType: "text/plain"}
	out := appendJSON(nil, r)
	assert.Contains(t, string(out), `"key":"k"`)
}

func TestAppendJSON_DefaultCase(t *testing.T) {
	out := appendJSON(nil, 42)
	assert.Contains(t, string(out), "unknown type")
}

// ── Config defaults tests ────────────────────────────────────────────────────

func TestConfig_Defaults(t *testing.T) {
	c := Config{}
	c.defaults()
	assert.Equal(t, "./uploads", c.MountPath)
	assert.Equal(t, int64(10*1024*1024), c.MaxFileSize)
	assert.Equal(t, "/upload", c.URLPrefix)
	assert.Equal(t, "local", c.Backend)
}

func TestConfig_DefaultsPartial(t *testing.T) {
	c := Config{MountPath: "/custom"}
	c.defaults()
	assert.Equal(t, "/custom", c.MountPath)
	assert.Equal(t, "local", c.Backend)
}

// ── Plugin lifecycle tests ───────────────────────────────────────────────────

func TestPlugin_HealthCheck_NotInitialized(t *testing.T) {
	p := &Plugin{cfg: Config{Backend: "local"}}
	status := p.HealthCheck(context.Background())
	assert.False(t, status.OK)
	assert.Contains(t, status.Message, "not initialized")
}

func TestPlugin_HealthCheck_WithStore(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	p := &Plugin{cfg: cfg, store: store}
	status := p.HealthCheck(context.Background())
	assert.True(t, status.OK)
	assert.Equal(t, "mount accessible", status.Message)
}

func TestPlugin_Shutdown(t *testing.T) {
	p := &Plugin{log: testLogger()}
	err := p.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestPlugin_Shutdown_NilLog(t *testing.T) {
	p := &Plugin{}
	err := p.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestPlugin_Name(t *testing.T) {
	p := &Plugin{}
	assert.Equal(t, "storage", p.Name())
}

// ── KeyForFile test ──────────────────────────────────────────────────────────

func TestKeyForFile(t *testing.T) {
	key := KeyForFile("test.png")
	// Should be in format: YYYY/MM/DD/test.png
	parts := strings.Split(key, "/")
	assert.Equal(t, 4, len(parts), "expected YYYY/MM/DD/name, got: %s", key)
	assert.Equal(t, "test.png", parts[3])
}

// ── Store URL test ───────────────────────────────────────────────────────────

func TestFSStore_URL(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	assert.Equal(t, "/upload/path/to/file.txt", store.URL("path/to/file.txt"))
}

// testLogger returns a discard logger for tests.
func testLogger() *slog.Logger {
	return slog.Default()
}

// ── Store Read/Close test ────────────────────────────────────────────────────

func TestFSStore_OpenReadClose(t *testing.T) {
	cfg := testConfig(t)
	store, err := NewFSStore(cfg)
	require.NoError(t, err)

	content := []byte("read me back")
	_, err = store.Upload(context.Background(), "readable.txt", bytes.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	rc, err := store.Open(context.Background(), "readable.txt")
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}
