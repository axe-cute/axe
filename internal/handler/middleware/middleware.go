// Package middleware provides HTTP middleware for the axe framework.
// All middleware follows Chi's pattern: func(http.Handler) http.Handler.
package middleware

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
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
// It logs the stack trace using the context logger and routes the
// response through WriteErrorCtx so that `?debug=1` in dev mode also
// surfaces the panic value inline.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log := logger.FromCtx(r.Context())
				log.Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				// P2-05: Preserve X-Request-ID so the client can correlate the 500.
				if rid := r.Header.Get("X-Request-Id"); rid != "" {
					w.Header().Set("X-Request-Id", rid)
				}
				WriteErrorCtx(w, r, apperror.ErrInternal.WithCause(
					errors.New(formatPanic(rec)),
				))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// formatPanic converts a recovered value to a string suitable for
// inclusion in an error chain. Avoids importing fmt purely for %v.
func formatPanic(rec any) string {
	switch v := rec.(type) {
	case error:
		return v.Error()
	case string:
		return v
	default:
		return "panic: see server logs for details"
	}
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
//
// Debug is populated only when the request runs in dev mode AND the
// caller passes `?debug=1`. It surfaces the wrapped Cause chain so a
// developer can localise a 500 without tailing logs. Production builds
// never emit this field — both because `APP_ENV != "dev"` and because
// only WriteErrorCtx (which has access to *http.Request) can set it.
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Debug   string `json:"debug,omitempty"`
}

// WriteError writes an error as a JSON response.
//
// Behaviour (E11 — see _internal/roadmap-evidence.md):
//   - 5xx responses ALWAYS produce a server-side log line, even if the
//     caller forgot to log. A 500 must never reach the client silently.
//   - For request-tagged logs (request_id, route) and `?debug=1` inline
//     details, prefer WriteErrorCtx — but the bare WriteError remains
//     safe to call from contexts that have no *http.Request handy.
func WriteError(w http.ResponseWriter, err error) {
	writeErrorImpl(w, nil, err)
}

// WriteErrorCtx is like WriteError but uses the request's context-bound
// logger so log lines carry request_id/method/path. When the process is
// running in dev mode (APP_ENV=dev) and the request URL has ?debug=1,
// the wrapped Cause chain is returned inline as `debug` in the JSON
// envelope to shorten the localise-a-500 loop.
func WriteErrorCtx(w http.ResponseWriter, r *http.Request, err error) {
	writeErrorImpl(w, r, err)
}

func writeErrorImpl(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		// Wrap so the cause survives for logging and `?debug=1`.
		appErr = apperror.ErrInternal.WithCause(err)
	}

	if appErr.HTTPStatus >= http.StatusInternalServerError {
		logServerError(r, appErr)
	}

	debug := isDebugRequest(r)
	writeError(w, appErr, debug)
}

// logServerError emits a structured log for any 5xx response. If a
// request is in scope its context-bound logger is used (so request_id
// shows up); otherwise we fall back to slog.Default so the line is
// never lost.
func logServerError(r *http.Request, appErr *apperror.AppError) {
	var log *slog.Logger
	if r != nil {
		log = logger.FromCtx(r.Context())
	} else {
		log = slog.Default()
	}
	attrs := []any{
		slog.String("code", appErr.Code),
		slog.Int("status", appErr.HTTPStatus),
		slog.String("message", appErr.Message),
	}
	if appErr.Cause != nil {
		attrs = append(attrs, slog.String("cause", appErr.Cause.Error()))
	}
	if r != nil {
		attrs = append(attrs,
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
	}
	log.Error("server error response", attrs...)
}

// isDebugRequest reports whether the response should embed Cause details
// in the JSON envelope. Two gates, both required:
//
//   - APP_ENV must equal "dev" (process-level — operator opts in once).
//   - The request must carry `?debug=1` (per-request — caller opts in).
//
// Either gate alone is insufficient; production cannot accidentally
// expose stack traces just because a curl carries `?debug=1`.
func isDebugRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if os.Getenv("APP_ENV") != "dev" {
		return false
	}
	return r.URL.Query().Get("debug") == "1"
}

func writeError(w http.ResponseWriter, appErr *apperror.AppError, debug bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.HTTPStatus)
	resp := errorResponse{Code: appErr.Code, Message: appErr.Message}
	if debug && appErr.Cause != nil {
		resp.Debug = appErr.Cause.Error()
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// WriteJSON writes a value as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
