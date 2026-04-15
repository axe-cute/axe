package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/axe-go/axe/internal/domain"
	"github.com/axe-go/axe/internal/handler"
	"github.com/axe-go/axe/pkg/apperror"
)

// ── Mock Service ─────────────────────────────────────────────────────────────

type mockUserService struct {
	createFn       func(context.Context, domain.CreateUserInput) (*domain.User, error)
	getFn          func(context.Context, uuid.UUID) (*domain.User, error)
	updateFn       func(context.Context, uuid.UUID, domain.UpdateUserInput) (*domain.User, error)
	deleteFn       func(context.Context, uuid.UUID) error
	listFn         func(context.Context, domain.Pagination) ([]*domain.User, int, error)
	authenticateFn func(context.Context, string, string) (*domain.User, error)
}

func (m *mockUserService) CreateUser(ctx context.Context, input domain.CreateUserInput) (*domain.User, error) {
	if m.createFn != nil {
		return m.createFn(ctx, input)
	}
	return &domain.User{ID: uuid.New(), Email: input.Email, Name: input.Name, Role: domain.RoleUser, Active: true}, nil
}
func (m *mockUserService) GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return &domain.User{ID: id, Email: "test@example.com", Name: "Test", Role: domain.RoleUser, Active: true}, nil
}
func (m *mockUserService) UpdateUser(ctx context.Context, id uuid.UUID, input domain.UpdateUserInput) (*domain.User, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, input)
	}
	return &domain.User{ID: id, Active: true}, nil
}
func (m *mockUserService) DeleteUser(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}
func (m *mockUserService) ListUsers(ctx context.Context, p domain.Pagination) ([]*domain.User, int, error) {
	if m.listFn != nil {
		return m.listFn(ctx, p)
	}
	return []*domain.User{}, 0, nil
}
func (m *mockUserService) Authenticate(ctx context.Context, email, pw string) (*domain.User, error) {
	if m.authenticateFn != nil {
		return m.authenticateFn(ctx, email, pw)
	}
	return nil, apperror.ErrUnauthorized
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func setupRouter(svc domain.UserService) *chi.Mux {
	h := handler.NewUserHandler(svc)
	r := chi.NewRouter()
	r.Mount("/api/v1/users", h.Routes())
	return r
}

func mustEncodeJSON(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to encode JSON: %v", err)
	}
	return bytes.NewBuffer(b)
}

// ── POST /api/v1/users ────────────────────────────────────────────────────────

func TestCreateUser_201(t *testing.T) {
	r := setupRouter(&mockUserService{})
	body := mustEncodeJSON(t, map[string]string{
		"email":    "alice@example.com",
		"name":     "Alice",
		"password": "secret123",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp["email"] != "alice@example.com" {
		t.Errorf("email = %v, want alice@example.com", resp["email"])
	}
	// password_hash must NOT be in response
	if _, ok := resp["password_hash"]; ok {
		t.Error("password_hash must not be exposed in response")
	}
}

func TestCreateUser_400_InvalidJSON(t *testing.T) {
	r := setupRouter(&mockUserService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestCreateUser_409_Conflict(t *testing.T) {
	svc := &mockUserService{
		createFn: func(_ context.Context, _ domain.CreateUserInput) (*domain.User, error) {
			return nil, apperror.ErrConflict.WithMessage("email already in use")
		},
	}
	r := setupRouter(svc)
	body := mustEncodeJSON(t, map[string]string{"email": "x@x.com", "name": "X", "password": "secret123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

// ── GET /api/v1/users/{id} ────────────────────────────────────────────────────

func TestGetUser_200(t *testing.T) {
	id := uuid.New()
	r := setupRouter(&mockUserService{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestGetUser_400_InvalidUUID(t *testing.T) {
	r := setupRouter(&mockUserService{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGetUser_404(t *testing.T) {
	svc := &mockUserService{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.User, error) {
			return nil, apperror.ErrNotFound
		},
	}
	r := setupRouter(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ── DELETE /api/v1/users/{id} ─────────────────────────────────────────────────

func TestDeleteUser_204(t *testing.T) {
	r := setupRouter(&mockUserService{})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

func TestDeleteUser_404(t *testing.T) {
	svc := &mockUserService{
		deleteFn: func(_ context.Context, _ uuid.UUID) error {
			return apperror.ErrNotFound
		},
	}
	r := setupRouter(svc)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ── GET /api/v1/users ─────────────────────────────────────────────────────────

func TestListUsers_200(t *testing.T) {
	r := setupRouter(&mockUserService{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?limit=10&offset=0", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := resp["data"]; !ok {
		t.Error("response should have 'data' field")
	}
	if _, ok := resp["total"]; !ok {
		t.Error("response should have 'total' field")
	}
}
