package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/pkg/apperror"
)

// ── RequestID ─────────────────────────────────────────────────────────────────

func TestRequestID_GeneratesID(t *testing.T) {
	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Error("X-Request-ID header should be set")
	}
}

func TestRequestID_ReusesClientID(t *testing.T) {
	clientID := "my-client-request-id"
	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", clientID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != clientID {
		t.Errorf("X-Request-ID = %q, want %q", got, clientID)
	}
}

// ── Logger middleware ─────────────────────────────────────────────────────────

func TestLoggerMiddleware_Runs(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Inject a logger into ctx so Logger middleware doesn't use the global one.
	handler := middleware.RequestID(middleware.Logger(inner))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("inner handler should have been called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ── Recoverer ─────────────────────────────────────────────────────────────────

func TestRecoverer_CatchesPanic(t *testing.T) {
	// Setup: inject logger so Recoverer can log
	handler := middleware.RequestID(middleware.Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Should NOT panic — Recoverer catches it
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// ── WriteError ────────────────────────────────────────────────────────────────

func TestWriteError_AppError(t *testing.T) {
	rec := httptest.NewRecorder()
	middleware.WriteError(rec, apperror.ErrNotFound.WithMessage("user not found"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["code"] != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", body["code"])
	}
	if body["message"] != "user not found" {
		t.Errorf("message = %q, want 'user not found'", body["message"])
	}
}

func TestWriteError_UnknownError_Returns500(t *testing.T) {
	rec := httptest.NewRecorder()
	middleware.WriteError(rec, errors.New("totally unexpected error"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["code"] != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", body["code"])
	}
}

// ── WriteJSON ─────────────────────────────────────────────────────────────────

func TestWriteJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	rec := httptest.NewRecorder()
	middleware.WriteJSON(rec, http.StatusCreated, payload{Name: "alice"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body payload
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if body.Name != "alice" {
		t.Errorf("name = %q, want alice", body.Name)
	}
}

// ── MaxBodySize ──────────────────────────────────────────────────────────────

func TestMaxBodySize_Default(t *testing.T) {
	called := false
	handler := middleware.MaxBodySize(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should have been called for small body")
	}
}

func TestMaxBodySize_CustomSize(t *testing.T) {
	handler := middleware.MaxBodySize(5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read body — should fail if over 5 bytes
		buf := make([]byte, 100)
		_, err := r.Body.Read(buf)
		if err != nil {
			middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("body too large"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// 10-byte body should exceed 5-byte limit
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for oversized body", rec.Code)
	}
}

func TestDefaultMaxBodySize_Constant(t *testing.T) {
	if middleware.DefaultMaxBodySize != 1<<20 {
		t.Errorf("DefaultMaxBodySize = %d, want %d", middleware.DefaultMaxBodySize, 1<<20)
	}
}

// ── Recoverer — X-Request-Id preservation ────────────────────────────────────

func TestRecoverer_PreservesRequestID(t *testing.T) {
	handler := middleware.Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "req-12345")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "req-12345" {
		t.Errorf("X-Request-Id = %q, want req-12345", got)
	}
}

func TestRecoverer_NoPanic_PassesThrough(t *testing.T) {
	called := false
	handler := middleware.Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("inner handler should have been called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ── Logger with non-200 status ───────────────────────────────────────────────

func TestLoggerMiddleware_CapturesNon200(t *testing.T) {
	handler := middleware.RequestID(middleware.Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})))

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestLoggerMiddleware_NoExplicitStatus_Defaults200(t *testing.T) {
	handler := middleware.RequestID(middleware.Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't call WriteHeader — should default to 200
		_, _ = w.Write([]byte("ok"))
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ── E11 — server errors ALWAYS log, dev ?debug=1 surfaces cause ────────────
//
// Regression target: a 500 must never reach the client without a server
// log line. We lost ~30 min per occurrence in the webtoon build because
// a raw error returned from a service was silently dropped to ErrInternal
// with no logging. See _internal/roadmap-evidence.md row E11.

// captureSlog swaps slog.Default for a buffer-backed JSON handler and
// returns the buffer plus a restore func.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return &buf, func() { slog.SetDefault(prev) }
}

func TestWriteError_5xx_AlwaysLogsServerSide(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	rec := httptest.NewRecorder()
	middleware.WriteError(rec, errors.New("db connection refused"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	logs := buf.String()
	if !strings.Contains(logs, "server error response") {
		t.Errorf("expected 'server error response' log, got: %s", logs)
	}
	if !strings.Contains(logs, "db connection refused") {
		t.Errorf("expected cause to be logged, got: %s", logs)
	}
}

func TestWriteError_4xx_DoesNotLog(t *testing.T) {
	// 4xx is client error — flooding logs with these creates noise.
	buf, restore := captureSlog(t)
	defer restore()

	rec := httptest.NewRecorder()
	middleware.WriteError(rec, apperror.ErrNotFound.WithMessage("user 42 not found"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if buf.Len() != 0 {
		t.Errorf("4xx must not log, got: %s", buf.String())
	}
}

func TestWriteErrorCtx_DebugQuery_GatedByAppEnv(t *testing.T) {
	cause := errors.New("the underlying cause")

	cases := []struct {
		name      string
		appEnv    string
		debugQS   bool
		wantDebug bool
	}{
		{"prod_no_debug_qs", "prod", false, false},
		{"prod_with_debug_qs_still_blocked", "prod", true, false},
		{"dev_no_debug_qs", "dev", false, false},
		{"dev_with_debug_qs_exposes_cause", "dev", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.appEnv)

			url := "/x"
			if tc.debugQS {
				url += "?debug=1"
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			middleware.WriteErrorCtx(rec, req, apperror.ErrInternal.WithCause(cause))

			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			gotDebug, ok := body["debug"]
			if tc.wantDebug {
				if !ok || gotDebug != cause.Error() {
					t.Errorf("debug = %q (present=%v); want %q", gotDebug, ok, cause.Error())
				}
			} else if ok {
				t.Errorf("debug must NOT leak in %s, got %q", tc.name, gotDebug)
			}
		})
	}
}

func TestWriteErrorCtx_RequestTaggedLog(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	req := httptest.NewRequest(http.MethodPost, "/v1/orders/123", nil)
	rec := httptest.NewRecorder()
	middleware.WriteErrorCtx(rec, req, errors.New("planet exploded"))

	logs := buf.String()
	if !strings.Contains(logs, `"method":"POST"`) {
		t.Errorf("expected method in log, got: %s", logs)
	}
	if !strings.Contains(logs, `"path":"/v1/orders/123"`) {
		t.Errorf("expected path in log, got: %s", logs)
	}
	if !strings.Contains(logs, "planet exploded") {
		t.Errorf("expected cause in log, got: %s", logs)
	}
}
