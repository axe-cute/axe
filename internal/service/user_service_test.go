package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/axe-go/axe/internal/domain"
	"github.com/axe-go/axe/internal/service"
	"github.com/axe-go/axe/pkg/apperror"
)

// ── Mock Repository ────────────────────────────────────────────────────────────

type mockUserRepo struct {
	createFn     func(ctx context.Context, input domain.CreateUserInput) (*domain.User, error)
	getByIDFn    func(ctx context.Context, id uuid.UUID) (*domain.User, error)
	getByEmailFn func(ctx context.Context, email string) (*domain.User, error)
	updateFn     func(ctx context.Context, id uuid.UUID, input domain.UpdateUserInput) (*domain.User, error)
	deleteFn     func(ctx context.Context, id uuid.UUID) error
	listFn       func(ctx context.Context, p domain.Pagination) ([]*domain.User, int, error)
}

func (m *mockUserRepo) Create(ctx context.Context, input domain.CreateUserInput) (*domain.User, error) {
	if m.createFn != nil {
		return m.createFn(ctx, input)
	}
	return &domain.User{ID: uuid.New(), Email: input.Email, Name: input.Name, Active: true}, nil
}

func (m *mockUserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return &domain.User{ID: id, Email: "test@example.com", Name: "Test", Active: true}, nil
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if m.getByEmailFn != nil {
		return m.getByEmailFn(ctx, email)
	}
	return nil, apperror.ErrNotFound
}

func (m *mockUserRepo) Update(ctx context.Context, id uuid.UUID, input domain.UpdateUserInput) (*domain.User, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, input)
	}
	return &domain.User{ID: id, Active: true}, nil
}

func (m *mockUserRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockUserRepo) List(ctx context.Context, p domain.Pagination) ([]*domain.User, int, error) {
	if m.listFn != nil {
		return m.listFn(ctx, p)
	}
	return []*domain.User{}, 0, nil
}

// ── Test helpers ───────────────────────────────────────────────────────────────

func newSvc(repo domain.UserRepository) domain.UserService {
	return service.NewUserService(repo)
}

func validCreateInput() domain.CreateUserInput {
	return domain.CreateUserInput{
		Email:    "alice@example.com",
		Name:     "Alice",
		Password: "supersecret123",
	}
}

// ── CreateUser ────────────────────────────────────────────────────────────────

func TestCreateUser_HappyPath(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	user, err := svc.CreateUser(context.Background(), validCreateInput())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user == nil {
		t.Fatal("user should not be nil")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("email = %q, want alice@example.com", user.Email)
	}
}

func TestCreateUser_InvalidEmail(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	input := validCreateInput()
	input.Email = "not-an-email"

	_, err := svc.CreateUser(context.Background(), input)
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestCreateUser_ShortPassword(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	input := validCreateInput()
	input.Password = "short"

	_, err := svc.CreateUser(context.Background(), input)
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	existing := &domain.User{ID: uuid.New(), Email: "alice@example.com"}
	repo := &mockUserRepo{
		getByEmailFn: func(_ context.Context, _ string) (*domain.User, error) {
			return existing, nil
		},
	}
	svc := newSvc(repo)
	_, err := svc.CreateUser(context.Background(), validCreateInput())

	if !errors.Is(err, apperror.ErrConflict) {
		t.Errorf("expected ErrConflict, got: %v", err)
	}
}

func TestCreateUser_EmptyName(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	input := validCreateInput()
	input.Name = ""

	_, err := svc.CreateUser(context.Background(), input)
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

// ── GetUser ───────────────────────────────────────────────────────────────────

func TestGetUser_Found(t *testing.T) {
	id := uuid.New()
	svc := newSvc(&mockUserRepo{})

	user, err := svc.GetUser(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != id {
		t.Errorf("ID = %v, want %v", user.ID, id)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*domain.User, error) {
			return nil, apperror.ErrNotFound
		},
	}
	svc := newSvc(repo)

	_, err := svc.GetUser(context.Background(), uuid.New())
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// ── DeleteUser ────────────────────────────────────────────────────────────────

func TestDeleteUser_HappyPath(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	if err := svc.DeleteUser(context.Background(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteUser_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*domain.User, error) {
			return nil, apperror.ErrNotFound
		},
	}
	svc := newSvc(repo)

	err := svc.DeleteUser(context.Background(), uuid.New())
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// ── ListUsers ─────────────────────────────────────────────────────────────────

func TestListUsers_InvalidPagination(t *testing.T) {
	svc := newSvc(&mockUserRepo{})

	_, _, err := svc.ListUsers(context.Background(), domain.Pagination{Limit: 0})
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestListUsers_HappyPath(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	users, total, err := svc.ListUsers(context.Background(), domain.DefaultPagination())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = users
	_ = total
}

// ── Authenticate ──────────────────────────────────────────────────────────────

func TestAuthenticate_WrongEmail(t *testing.T) {
	repo := &mockUserRepo{
		getByEmailFn: func(_ context.Context, _ string) (*domain.User, error) {
			return nil, apperror.ErrNotFound
		},
	}
	svc := newSvc(repo)

	_, err := svc.Authenticate(context.Background(), "nobody@example.com", "pass")
	if !errors.Is(err, apperror.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got: %v", err)
	}
}
