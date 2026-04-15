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

	"github.com/axe-go/axe/pkg/apperror"
	"github.com/axe-go/axe/pkg/logger"
	"github.com/google/uuid"
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

// ── ErrorHandler ──────────────────────────────────────────────────────────────

// errorResponse is the canonical JSON error envelope.
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorHandler is a Chi-compatible middleware that handles errors written
// to the response. For handler-level errors, prefer using WriteError directly.
//
// NOTE: Chi does not have built-in error propagation; handlers call
// WriteError(w, err) directly rather than returning errors.
func ErrorHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
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
