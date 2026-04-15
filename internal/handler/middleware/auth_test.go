package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/pkg/jwtauth"
)

func newJWTSvc() *jwtauth.Service {
	return jwtauth.New("test-secret-key-min-32-bytes-long!!", 15*time.Minute, 7*24*time.Hour)
}

// ── JWTAuth middleware ────────────────────────────────────────────────────────

func TestJWTAuth_ValidToken_Passes(t *testing.T) {
	svc := newJWTSvc()
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")

	called := false
	handler := middleware.JWTAuth(svc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		claims := middleware.ClaimsFromCtx(r.Context())
		if claims == nil {
			t.Error("expected claims in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("inner handler should have been called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestJWTAuth_MissingHeader_Returns401(t *testing.T) {
	svc := newJWTSvc()
	handler := middleware.JWTAuth(svc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach inner handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil) // no auth header
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestJWTAuth_InvalidToken_Returns401(t *testing.T) {
	svc := newJWTSvc()
	handler := middleware.JWTAuth(svc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach inner handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestJWTAuth_ExpiredToken_Returns401(t *testing.T) {
	expiredSvc := jwtauth.New("test-secret-key-min-32-bytes-long!!", -time.Second, time.Hour)
	pair, _ := expiredSvc.GenerateTokenPair(uuid.New(), "user")

	// validate with normal svc (same secret, but expired)
	svc := newJWTSvc()
	handler := middleware.JWTAuth(svc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach inner handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// ── RequireRole middleware ────────────────────────────────────────────────────

func TestRequireRole_Admin_AllowsAdmin(t *testing.T) {
	svc := newJWTSvc()
	pair, _ := svc.GenerateTokenPair(uuid.New(), "admin")

	called := false
	handler := middleware.JWTAuth(svc, nil)(
		middleware.RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("admin should access admin route")
	}
}

func TestRequireRole_Admin_BlocksUser(t *testing.T) {
	svc := newJWTSvc()
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")

	handler := middleware.JWTAuth(svc, nil)(
		middleware.RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("user should not reach admin handler")
		})),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestRequireRole_User_AllowsBothRoles(t *testing.T) {
	svc := newJWTSvc()

	for _, role := range []string{"user", "admin"} {
		pair, _ := svc.GenerateTokenPair(uuid.New(), role)
		called := false

		handler := middleware.JWTAuth(svc, nil)(
			middleware.RequireRole("user")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})),
		)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !called {
			t.Errorf("role %q should pass user-level RequireRole", role)
		}
	}
}
