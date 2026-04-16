package storage

import (
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// handler provides HTTP endpoints for file operations.
type handler struct {
	store Store
	cfg   Config
	log   *slog.Logger
}

// handleUpload handles POST /upload — multipart file upload.
//
// Request: multipart/form-data with field "file".
// Response: 201 Created with JSON body:
//
//	{"key": "2026/04/16/abc123.png", "url": "/upload/2026/04/16/abc123.png", "size": 12345, "content_type": "image/png"}
func (h *handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Limit request body to max file size + overhead for multipart headers
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxFileSize+1024*1024)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		metricsUploadErrors.WithLabelValues("parse_error").Inc()
		writeError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		metricsUploadErrors.WithLabelValues("missing_file").Inc()
		writeError(w, http.StatusBadRequest, "missing 'file' field in form data")
		return
	}
	defer file.Close()

	// Detect content type
	contentType := header.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		// Try to detect from file extension
		ext := filepath.Ext(header.Filename)
		if ct := mime.TypeByExtension(ext); ct != "" {
			contentType = ct
		} else {
			// Sniff from content
			buf := make([]byte, 512)
			n, _ := file.Read(buf)
			contentType = http.DetectContentType(buf[:n])
			// Seek back to start
			if seeker, ok := file.(io.ReadSeeker); ok {
				_, _ = seeker.Seek(0, io.SeekStart)
			}
		}
	}

	// Generate key: YYYY/MM/DD/uuid.ext
	ext := filepath.Ext(header.Filename)
	key := KeyForFile(uuid.New().String() + ext)

	result, err := h.store.Upload(r.Context(), key, file, header.Size, contentType)
	if err != nil {
		h.log.Warn("upload failed", "error", err, "filename", header.Filename)
		metricsUploadErrors.WithLabelValues("store_error").Inc()

		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not allowed") {
			status = http.StatusUnsupportedMediaType
		} else if strings.Contains(err.Error(), "exceeds max") {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, err.Error())
		return
	}

	metricsUploadBytes.Add(float64(result.Size))
	metricsOps.WithLabelValues("upload", "ok").Inc()

	h.log.Info("file uploaded",
		"key", result.Key,
		"size", result.Size,
		"content_type", result.ContentType,
	)

	writeJSON(w, http.StatusCreated, result)
}

// handleServe handles GET /upload/{key} — serve file content.
func (h *handler) handleServe(w http.ResponseWriter, r *http.Request) {
	key := h.extractKey(r)
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing file key")
		return
	}

	reader, err := h.store.Open(r.Context(), key)
	if err != nil {
		metricsOps.WithLabelValues("serve", "error").Inc()
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "file not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to open file")
		}
		return
	}
	defer reader.Close()

	// Set content type from file extension
	ext := filepath.Ext(key)
	if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	metricsOps.WithLabelValues("serve", "ok").Inc()

	if _, err := io.Copy(w, reader); err != nil {
		h.log.Warn("serve file copy error", "key", key, "error", err)
	}
}

// handleDelete handles DELETE /upload/{key} — delete a file.
func (h *handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := h.extractKey(r)
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing file key")
		return
	}

	if err := h.store.Delete(r.Context(), key); err != nil {
		metricsOps.WithLabelValues("delete", "error").Inc()
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "file not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to delete file")
		}
		return
	}

	metricsOps.WithLabelValues("delete", "ok").Inc()
	h.log.Info("file deleted", "key", key)

	w.WriteHeader(http.StatusNoContent)
}

// ── Response helpers ──────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Use encoding/json for simplicity
	enc := make([]byte, 0, 256)
	enc = appendJSON(enc, v)
	_, _ = w.Write(enc)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":%q}`, msg)
}

// appendJSON is a minimal JSON serializer for Result structs.
func appendJSON(dst []byte, v any) []byte {
	switch val := v.(type) {
	case *Result:
		return fmt.Appendf(dst, `{"key":%q,"url":%q,"size":%d,"content_type":%q}`,
			val.Key, val.URL, val.Size, val.ContentType)
	default:
		return fmt.Appendf(dst, `{"error":"unknown type"}`)
	}
}

// extractKey derives the storage key from the request URL by stripping the
// configured URL prefix. This works with chi's /* catch-all pattern, which
// is necessary because keys contain slashes (e.g. "2026/04/16/uuid.ext").
func (h *handler) extractKey(r *http.Request) string {
	prefix := h.cfg.URLPrefix + "/"
	key := strings.TrimPrefix(r.URL.Path, prefix)
	if key == r.URL.Path {
		// Prefix didn't match — shouldn't happen if routing is correct
		return ""
	}
	return key
}
