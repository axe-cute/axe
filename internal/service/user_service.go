package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/axe-cute/axe/internal/domain"
	"github.com/axe-cute/axe/pkg/apperror"
	"github.com/axe-cute/axe/pkg/logger"
)

// UserService implements domain.UserService.
// It holds the repository interface and the transaction manager.
type UserService struct {
	repo domain.UserRepository
}

// NewUserService creates a new UserService.
// Dependencies are injected via interfaces — no concrete types.
func NewUserService(repo domain.UserRepository) domain.UserService {
	return &UserService{repo: repo}
}

// CreateUser validates input, hashes the password, and delegates to repository.
func (s *UserService) CreateUser(ctx context.Context, input domain.CreateUserInput) (*domain.User, error) {
	log := logger.FromCtx(ctx).With("email", input.Email)

	// Validate
	if err := validateCreateUserInput(input); err != nil {
		return nil, apperror.ErrInvalidInput.WithMessage(err.Error()).WithCause(err)
	}

	// Check duplicate email
	existing, err := s.repo.GetByEmail(ctx, input.Email)
	if err != nil && !apperror.IsNotFound(err) {
		return nil, fmt.Errorf("UserService.CreateUser: check existing: %w", err)
	}
	if existing != nil {
		return nil, apperror.ErrConflict.WithMessage("email already in use")
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperror.ErrInternal.WithMessage("failed to hash password").WithCause(err)
	}
	input.Password = string(hash)

	// Default role
	if input.Role == "" {
		input.Role = domain.RoleUser
	}

	user, err := s.repo.Create(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("UserService.CreateUser: %w", err)
	}

	log.Info("user created", "user_id", user.ID)
	return user, nil
}

// GetUser retrieves a single user by ID.
func (s *UserService) GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err // apperror already wrapped by repository
	}
	return user, nil
}

// UpdateUser applies partial updates to a user.
func (s *UserService) UpdateUser(ctx context.Context, id uuid.UUID, input domain.UpdateUserInput) (*domain.User, error) {
	// Ensure user exists
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}

	user, err := s.repo.Update(ctx, id, input)
	if err != nil {
		return nil, fmt.Errorf("UserService.UpdateUser: %w", err)
	}

	logger.FromCtx(ctx).Info("user updated", "user_id", id)
	return user, nil
}

// DeleteUser soft-deletes a user (sets active=false).
func (s *UserService) DeleteUser(ctx context.Context, id uuid.UUID) error {
	// Ensure user exists
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("UserService.DeleteUser: %w", err)
	}

	logger.FromCtx(ctx).Info("user deleted", "user_id", id)
	return nil
}

// ListUsers returns a paginated list of active users.
func (s *UserService) ListUsers(ctx context.Context, pagination domain.Pagination) ([]*domain.User, int, error) {
	if err := pagination.Validate(); err != nil {
		return nil, 0, apperror.ErrInvalidInput.WithMessage(err.Error())
	}
	return s.repo.List(ctx, pagination)
}

// Authenticate verifies email+password and returns the user on success.
func (s *UserService) Authenticate(ctx context.Context, email, password string) (*domain.User, error) {
	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		if apperror.IsNotFound(err) {
			// Don't leak whether the email exists
			return nil, apperror.ErrUnauthorized.WithMessage("invalid credentials")
		}
		return nil, fmt.Errorf("UserService.Authenticate: %w", err)
	}

	if !user.IsActive() {
		return nil, apperror.ErrForbidden.WithMessage("account deactivated")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, apperror.ErrUnauthorized.WithMessage("invalid credentials")
	}

	return user, nil
}

// ── Validation ────────────────────────────────────────────────────────────────

func validateCreateUserInput(input domain.CreateUserInput) error {
	input.Email = strings.TrimSpace(input.Email)
	input.Name = strings.TrimSpace(input.Name)

	if input.Email == "" {
		return fmt.Errorf("email is required")
	}
	if !strings.Contains(input.Email, "@") {
		return fmt.Errorf("email is invalid")
	}
	if len(input.Email) > 255 {
		return fmt.Errorf("email must be 255 characters or fewer")
	}
	if input.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(input.Name) > 255 {
		return fmt.Errorf("name must be 255 characters or fewer")
	}
	if len(input.Password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if input.Role != "" && input.Role != domain.RoleUser && input.Role != domain.RoleAdmin {
		return fmt.Errorf("role must be 'user' or 'admin'")
	}
	return nil
}
