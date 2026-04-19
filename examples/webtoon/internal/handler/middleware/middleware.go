// Package middleware provides HTTP middleware (WriteError, WriteJSON, Logger).
package middleware

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/axe-cute/examples-webtoon/pkg/apperror"
	"github.com/axe-cute/examples-webtoon/pkg/logger"
	"github.com/google/uuid"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(logger.WithRequestID(r.Context(), id)))
	})
}

type rw struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrap(w http.ResponseWriter) *rw { return &rw{ResponseWriter: w} }
func (r *rw) Status() int {
	if r.status == 0 {
		return 200
	}
	return r.status
}
func (r *rw) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := wrap(w)
		defer func() {
			logger.FromCtx(r.Context()).Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.Status()),
				slog.Duration("latency", time.Since(start)),
			)
		}()
		next.ServeHTTP(wrapped, r)
	})
}

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.FromCtx(r.Context()).Error("panic",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				writeError(w, apperror.ErrInternal)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteError(w http.ResponseWriter, err error) {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		writeError(w, appErr)
		return
	}
	writeError(w, apperror.ErrInternal)
}

func writeError(w http.ResponseWriter, appErr *apperror.AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.HTTPStatus)
	_ = json.NewEncoder(w).Encode(errorResponse{Code: appErr.Code, Message: appErr.Message})
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
