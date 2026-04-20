package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/internal/domain"
	"github.com/axe-cute/axe/internal/handler"
	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/pkg/apperror"
	"github.com/axe-cute/axe/pkg/jwtauth"
)

// ── Auth-specific mocks ─────────────────────────────────────────────────────
// mockUserService is defined in user_handler_test.go — reused here.
// We only need a Blocklist mock.

type mockBlocklist struct {
	blocked map[string]bool
}

func newMockBlocklist() *mockBlocklist {
	return &mockBlocklist{blocked: make(map[string]bool)}
}

func (b *mockBlocklist) BlockToken(_ context.Context, jti string, _ time.Duration) error {
	b.blocked[jti] = true
	return nil
}

func (b *mockBlocklist) IsTokenBlocked(_ context.Context, jti string) (bool, error) {
	return b.blocked[jti], nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

const authTestSecret = "test-secret-key-at-least-32-bytes!!"

func newAuthJWTService() *jwtauth.Service {
	svc, err := jwtauth.New(authTestSecret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		panic(err)
	}
	return svc
}

func authTestUser() *domain.User {
	return &domain.User{
		ID:     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Email:  "alice@example.com",
		Name:   "Alice",
		Role:   domain.RoleUser,
		Active: true,
	}
}

func authJsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewBuffer(buf)
}

// injectClaims creates a context with JWT claims, simulating what JWTAuth middleware does.
func injectClaims(ctx context.Context, userID uuid.UUID, role string) context.Context {
	claims := &jwtauth.Claims{
		UserID: userID.String(),
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	return middleware.InjectClaimsForTest(ctx, claims)
}

// ─────────────────────────────────────────────────────────────────────────────
// Login
// ─────────────────────────────────────────────────────────────────────────────

func TestLogin_HappyPath(t *testing.T) {
	user := authTestUser()
	svc := &mockUserService{
		authenticateFn: func(_ context.Context, email, password string) (*domain.User, error) {
			if email == "alice@example.com" && password == "correct-password" {
				return user, nil
			}
			return nil, apperror.ErrUnauthorized
		},
	}

	h := handler.NewAuthHandler(svc, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Post("/login", h.Login)

	body := authJsonBody(t, map[string]string{
		"email":    "alice@example.com",
		"password": "correct-password",
	})
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["access_token"])
	assert.NotEmpty(t, resp["refresh_token"])
	assert.Equal(t, user.ID.String(), resp["user_id"])
	assert.Equal(t, "user", resp["role"])
}

func TestLogin_WrongCredentials(t *testing.T) {
	svc := &mockUserService{
		authenticateFn: func(context.Context, string, string) (*domain.User, error) {
			return nil, apperror.ErrUnauthorized.WithMessage("invalid credentials")
		},
	}

	h := handler.NewAuthHandler(svc, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Post("/login", h.Login)

	body := authJsonBody(t, map[string]string{"email": "bad@test.com", "password": "wrong"})
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLogin_MissingFields(t *testing.T) {
	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Post("/login", h.Login)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"empty email", map[string]string{"email": "", "password": "pass"}},
		{"empty password", map[string]string{"email": "a@b.com", "password": ""}},
		{"both empty", map[string]string{"email": "", "password": ""}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/login", authJsonBody(t, tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestLogin_InvalidJSON(t *testing.T) {
	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Post("/login", h.Login)

	req := httptest.NewRequest("POST", "/login", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ─────────────────────────────────────────────────────────────────────────────
// Refresh
// ─────────────────────────────────────────────────────────────────────────────

func TestRefresh_HappyPath(t *testing.T) {
	user := authTestUser()
	jwtSvc := newAuthJWTService()

	svc := &mockUserService{
		getFn: func(_ context.Context, id uuid.UUID) (*domain.User, error) {
			if id == user.ID {
				return user, nil
			}
			return nil, apperror.ErrNotFound
		},
	}

	// Generate a valid refresh token.
	pair, err := jwtSvc.GenerateTokenPair(user.ID, string(user.Role))
	require.NoError(t, err)

	h := handler.NewAuthHandler(svc, jwtSvc, nil)
	r := chi.NewRouter()
	r.Post("/refresh", h.Refresh)

	body := authJsonBody(t, map[string]string{"refresh_token": pair.RefreshToken})
	req := httptest.NewRequest("POST", "/refresh", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["access_token"])
	assert.NotEmpty(t, resp["refresh_token"])
	assert.Equal(t, user.ID.String(), resp["user_id"])
}

func TestRefresh_InvalidToken(t *testing.T) {
	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Post("/refresh", h.Refresh)

	body := authJsonBody(t, map[string]string{"refresh_token": "invalid.token.here"})
	req := httptest.NewRequest("POST", "/refresh", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRefresh_UserNotFound(t *testing.T) {
	jwtSvc := newAuthJWTService()
	pair, _ := jwtSvc.GenerateTokenPair(uuid.New(), "user")

	svc := &mockUserService{
		getFn: func(context.Context, uuid.UUID) (*domain.User, error) {
			return nil, apperror.ErrNotFound
		},
	}

	h := handler.NewAuthHandler(svc, jwtSvc, nil)
	r := chi.NewRouter()
	r.Post("/refresh", h.Refresh)

	body := authJsonBody(t, map[string]string{"refresh_token": pair.RefreshToken})
	req := httptest.NewRequest("POST", "/refresh", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRefresh_InvalidJSON(t *testing.T) {
	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Post("/refresh", h.Refresh)

	req := httptest.NewRequest("POST", "/refresh", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ─────────────────────────────────────────────────────────────────────────────
// Logout
// ─────────────────────────────────────────────────────────────────────────────

func TestLogout_WithBlocklist(t *testing.T) {
	user := authTestUser()
	blocklist := newMockBlocklist()

	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), blocklist)
	r := chi.NewRouter()
	r.Post("/logout", h.Logout)

	req := httptest.NewRequest("POST", "/logout", nil)
	ctx := injectClaims(req.Context(), user.ID, "user")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.True(t, len(blocklist.blocked) > 0, "token JTI should be added to blocklist")
}

func TestLogout_WithoutBlocklist(t *testing.T) {
	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Post("/logout", h.Logout)

	req := httptest.NewRequest("POST", "/logout", nil)
	ctx := injectClaims(req.Context(), authTestUser().ID, "user")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestLogout_NoClaims(t *testing.T) {
	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Post("/logout", h.Logout)

	req := httptest.NewRequest("POST", "/logout", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, "defensive: 204 even without claims")
}

// ─────────────────────────────────────────────────────────────────────────────
// Me
// ─────────────────────────────────────────────────────────────────────────────

func TestMe_HappyPath(t *testing.T) {
	user := authTestUser()
	svc := &mockUserService{
		getFn: func(_ context.Context, id uuid.UUID) (*domain.User, error) {
			if id == user.ID {
				return user, nil
			}
			return nil, apperror.ErrNotFound
		},
	}

	h := handler.NewAuthHandler(svc, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Get("/me", h.Me)

	req := httptest.NewRequest("GET", "/me", nil)
	ctx := injectClaims(req.Context(), user.ID, "user")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, user.ID.String(), resp["id"])
	assert.Equal(t, "alice@example.com", resp["email"])
	assert.Equal(t, "Alice", resp["name"])
}

func TestMe_NoClaims(t *testing.T) {
	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Get("/me", h.Me)

	req := httptest.NewRequest("GET", "/me", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMe_UserNotFound(t *testing.T) {
	svc := &mockUserService{
		getFn: func(context.Context, uuid.UUID) (*domain.User, error) {
			return nil, apperror.ErrNotFound
		},
	}

	h := handler.NewAuthHandler(svc, newAuthJWTService(), nil)
	r := chi.NewRouter()
	r.Get("/me", h.Me)

	req := httptest.NewRequest("GET", "/me", nil)
	ctx := injectClaims(req.Context(), uuid.New(), "user")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ─────────────────────────────────────────────────────────────────────────────
// Routes structure
// ─────────────────────────────────────────────────────────────────────────────

func TestAuthHandler_Routes_MountsAllEndpoints(t *testing.T) {
	h := handler.NewAuthHandler(&mockUserService{}, newAuthJWTService(), nil)
	router := h.Routes()

	routes := []string{}
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})

	assert.Contains(t, routes, "POST /login")
	assert.Contains(t, routes, "POST /refresh")
	assert.Contains(t, routes, "POST /logout")
	assert.Contains(t, routes, "GET /me")
}
