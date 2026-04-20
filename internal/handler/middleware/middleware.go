// Package middleware provides HTTP middleware for the axe framework.
// All middleware follows Chi's pattern: func(http.Handler) http.Handler.
package middleware

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/axe-cute/axe/pkg/apperror"
	"github.com/axe-cute/axe/pkg/logger"
)

// ── RequestID ──────────────────────────────────────────────────────────────────

// RequestID generates a unique request ID (UUID v4) for each request,
// sets it on the response header, and injects it into the context logger.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := logger.WithRequestID(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ── Logger ─────────────────────────────────────────────────────────────────────

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (rw *responseWriter) Status() int {
	if rw.status == 0 {
		return http.StatusOK
	}
	return rw.status
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

// Logger logs each incoming request with method, path, status, and latency.
// Uses the context-injected logger so logs carry request_id automatically.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := wrapResponseWriter(w)

		defer func() {
			log := logger.FromCtx(r.Context())
			log.Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.Status()),
				slog.Duration("latency", time.Since(start)),
				slog.String("ip", r.RemoteAddr),
			)
		}()

		next.ServeHTTP(wrapped, r)
	})
}

// ── Recoverer ─────────────────────────────────────────────────────────────────

// Recoverer catches panics and returns a 500 Internal Server Error.
// It logs the stack trace using the context logger.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log := logger.FromCtx(r.Context())
				log.Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				writeError(w, apperror.ErrInternal)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ── MaxBodySize ───────────────────────────────────────────────────────────────

// DefaultMaxBodySize is the default request body limit (1 MB).
const DefaultMaxBodySize int64 = 1 << 20

// MaxBodySize limits the size of incoming request bodies.
// Returns 413 Request Entity Too Large if exceeded.
// Use 0 for the default (1 MB), or pass a custom size in bytes.
//
//	r.Use(middleware.MaxBodySize(0))        // 1 MB default
//	r.Use(middleware.MaxBodySize(5<<20))    // 5 MB
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodySize
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// ── Error Response ────────────────────────────────────────────────────────────

// errorResponse is the canonical JSON error envelope.
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes an *apperror.AppError as a JSON response.
// Handlers should call this instead of writing raw error strings.
//
//	func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
//	    user, err := h.svc.GetUser(ctx, id)
//	    if err != nil {
//	        middleware.WriteError(w, err)
//	        return
//	    }
//	    WriteJSON(w, http.StatusOK, user)
//	}
func WriteError(w http.ResponseWriter, err error) {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		writeError(w, appErr)
		return
	}
	// Unknown error — log and return 500
	writeError(w, apperror.ErrInternal)
}

func writeError(w http.ResponseWriter, appErr *apperror.AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.HTTPStatus)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Code:    appErr.Code,
		Message: appErr.Message,
	})
}

// WriteJSON writes a value as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
