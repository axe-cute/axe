package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/axe-cute/axe/internal/domain"
	"github.com/axe-cute/axe/internal/service"
	"github.com/axe-cute/axe/pkg/apperror"
)

// ── PostService: GetPost, UpdatePost, ListPosts ──────────────────────────────

func TestGetPost_HappyPath(t *testing.T) {
	svc := service.NewPostService(&mockPostRepo{})
	id := uuid.New()
	result, err := svc.GetPost(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != id {
		t.Errorf("ID = %v, want %v", result.ID, id)
	}
}

func TestUpdatePost_HappyPath(t *testing.T) {
	svc := service.NewPostService(&mockPostRepo{})
	id := uuid.New()
	result, err := svc.UpdatePost(context.Background(), id, domain.UpdatePostInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != id {
		t.Errorf("ID = %v, want %v", result.ID, id)
	}
}

func TestUpdatePost_NotFound(t *testing.T) {
	repo := &mockPostRepo{}
	// Override GetByID to return not found — need internal test or wrap.
	// Since mockPostRepo is simple, we test via the service layer.
	// The default mockPostRepo returns a post, so this path is already tested.
	// Let's test with a repo that fails.
	svc := service.NewPostService(&failGetPostRepo{})
	_, err := svc.UpdatePost(context.Background(), uuid.New(), domain.UpdatePostInput{})
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
	_ = repo
}

func TestListPosts_HappyPath(t *testing.T) {
	svc := service.NewPostService(&mockPostRepo{})
	results, total, err := svc.ListPosts(context.Background(), domain.DefaultPagination())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = results
	_ = total
}

func TestListPosts_InvalidPagination(t *testing.T) {
	svc := service.NewPostService(&mockPostRepo{})
	_, _, err := svc.ListPosts(context.Background(), domain.Pagination{Limit: 0})
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestDeletePost_NotFound(t *testing.T) {
	svc := service.NewPostService(&failGetPostRepo{})
	err := svc.DeletePost(context.Background(), uuid.New())
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestCreatePost_RepoError(t *testing.T) {
	svc := service.NewPostService(&failCreatePostRepo{})
	_, err := svc.CreatePost(context.Background(), domain.CreatePostInput{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// failGetPostRepo returns NotFound on GetByID.
type failGetPostRepo struct{ mockPostRepo }

func (m *failGetPostRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Post, error) {
	return nil, apperror.ErrNotFound
}

// failCreatePostRepo returns error on Create.
type failCreatePostRepo struct{ mockPostRepo }

func (m *failCreatePostRepo) Create(_ context.Context, _ domain.CreatePostInput) (*domain.Post, error) {
	return nil, errors.New("db error")
}

// ── UserService: UpdateUser, Authenticate ────────────────────────────────────

func TestUpdateUser_HappyPath(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	id := uuid.New()
	name := "Updated"
	user, err := svc.UpdateUser(context.Background(), id, domain.UpdateUserInput{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != id {
		t.Errorf("ID = %v, want %v", user.ID, id)
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*domain.User, error) {
			return nil, apperror.ErrNotFound
		},
	}
	svc := newSvc(repo)
	_, err := svc.UpdateUser(context.Background(), uuid.New(), domain.UpdateUserInput{})
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestUpdateUser_RepoError(t *testing.T) {
	repo := &mockUserRepo{
		updateFn: func(_ context.Context, _ uuid.UUID, _ domain.UpdateUserInput) (*domain.User, error) {
			return nil, errors.New("db error")
		},
	}
	svc := newSvc(repo)
	_, err := svc.UpdateUser(context.Background(), uuid.New(), domain.UpdateUserInput{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAuthenticate_Success(t *testing.T) {
	password := "supersecret123"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)

	repo := &mockUserRepo{
		getByEmailFn: func(_ context.Context, _ string) (*domain.User, error) {
			return &domain.User{
				ID:           uuid.New(),
				Email:        "alice@example.com",
				PasswordHash: string(hash),
				Active:       true,
			}, nil
		},
	}
	svc := newSvc(repo)
	user, err := svc.Authenticate(context.Background(), "alice@example.com", password)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("email = %q, want alice@example.com", user.Email)
	}
}

func TestAuthenticate_WrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)

	repo := &mockUserRepo{
		getByEmailFn: func(_ context.Context, _ string) (*domain.User, error) {
			return &domain.User{ID: uuid.New(), PasswordHash: string(hash), Active: true}, nil
		},
	}
	svc := newSvc(repo)
	_, err := svc.Authenticate(context.Background(), "a@b.com", "wrong")
	if !errors.Is(err, apperror.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got: %v", err)
	}
}

func TestAuthenticate_InactiveAccount(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)

	repo := &mockUserRepo{
		getByEmailFn: func(_ context.Context, _ string) (*domain.User, error) {
			return &domain.User{ID: uuid.New(), PasswordHash: string(hash), Active: false}, nil
		},
	}
	svc := newSvc(repo)
	_, err := svc.Authenticate(context.Background(), "a@b.com", "password")
	if !errors.Is(err, apperror.ErrForbidden) {
		t.Errorf("expected ErrForbidden, got: %v", err)
	}
}

func TestAuthenticate_RepoError(t *testing.T) {
	repo := &mockUserRepo{
		getByEmailFn: func(_ context.Context, _ string) (*domain.User, error) {
			return nil, errors.New("db connection error")
		},
	}
	svc := newSvc(repo)
	_, err := svc.Authenticate(context.Background(), "a@b.com", "pass")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── Validation edge cases ────────────────────────────────────────────────────

func TestCreateUser_EmptyEmail(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	input := validCreateInput()
	input.Email = ""
	_, err := svc.CreateUser(context.Background(), input)
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestCreateUser_InvalidRole(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	input := validCreateInput()
	input.Role = "superadmin"
	_, err := svc.CreateUser(context.Background(), input)
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestCreateUser_LongEmail(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	input := validCreateInput()
	longPart := ""
	for i := 0; i < 260; i++ {
		longPart += "a"
	}
	input.Email = longPart + "@test.com"
	_, err := svc.CreateUser(context.Background(), input)
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestCreateUser_LongName(t *testing.T) {
	svc := newSvc(&mockUserRepo{})
	input := validCreateInput()
	longName := ""
	for i := 0; i < 260; i++ {
		longName += "n"
	}
	input.Name = longName
	_, err := svc.CreateUser(context.Background(), input)
	if !errors.Is(err, apperror.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestCreateUser_RepoError(t *testing.T) {
	repo := &mockUserRepo{
		createFn: func(_ context.Context, _ domain.CreateUserInput) (*domain.User, error) {
			return nil, errors.New("db error")
		},
	}
	svc := newSvc(repo)
	_, err := svc.CreateUser(context.Background(), validCreateInput())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDeleteUser_RepoDeleteError(t *testing.T) {
	repo := &mockUserRepo{
		deleteFn: func(_ context.Context, _ uuid.UUID) error {
			return errors.New("db error")
		},
	}
	svc := newSvc(repo)
	err := svc.DeleteUser(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
