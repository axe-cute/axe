// Package repository implements the domain repository interfaces using Ent (writes)
// and raw SQL/sqlc (complex reads). All methods extract transactions from context.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	ent "github.com/axe-go/axe/ent"
	entuser "github.com/axe-go/axe/ent/user"
	"github.com/axe-go/axe/internal/domain"
	"github.com/axe-go/axe/pkg/apperror"
)

// UserRepo implements domain.UserRepository using Ent for writes and
// simple reads, sqlc for complex reads (see user_query.go).
type UserRepo struct {
	client *ent.Client
}

// NewUserRepo creates a new UserRepo.
func NewUserRepo(client *ent.Client) domain.UserRepository {
	return &UserRepo{client: client}
}

// ── Write Operations (Ent) ────────────────────────────────────────────────────

// Create inserts a new user into the database.
func (r *UserRepo) Create(ctx context.Context, input domain.CreateUserInput) (*domain.User, error) {
	u, err := r.client.User.Create().
		SetEmail(input.Email).
		SetName(input.Name).
		SetPasswordHash(input.Password). // already hashed by service layer
		SetRole(entuser.Role(input.Role)).
		SetActive(true).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, apperror.ErrConflict.WithMessage("email already in use").WithCause(err)
		}
		return nil, fmt.Errorf("UserRepo.Create: %w", err)
	}
	return mapEntUser(u), nil
}

// Update applies partial updates to an existing user.
func (r *UserRepo) Update(ctx context.Context, id uuid.UUID, input domain.UpdateUserInput) (*domain.User, error) {
	q := r.client.User.UpdateOneID(id)

	if input.Name != nil {
		q = q.SetName(*input.Name)
	}
	if input.Role != nil {
		q = q.SetRole(entuser.Role(*input.Role))
	}
	if input.Active != nil {
		q = q.SetActive(*input.Active)
	}

	u, err := q.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
		}
		return nil, fmt.Errorf("UserRepo.Update: %w", err)
	}
	return mapEntUser(u), nil
}

// Delete soft-deletes a user by setting active=false.
func (r *UserRepo) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.client.User.UpdateOneID(id).
		SetActive(false).
		SetUpdatedAt(time.Now()).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
		}
		return fmt.Errorf("UserRepo.Delete: %w", err)
	}
	return nil
}

// ── Read Operations (Ent) ────────────────────────────────────────────────────

// GetByID retrieves an active user by UUID.
func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	u, err := r.client.User.Query().
		Where(
			entuser.IDEQ(id),
			entuser.ActiveEQ(true),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
		}
		return nil, fmt.Errorf("UserRepo.GetByID: %w", err)
	}
	return mapEntUser(u), nil
}

// GetByEmail retrieves an active user by email address.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	u, err := r.client.User.Query().
		Where(
			entuser.EmailEQ(email),
			entuser.ActiveEQ(true),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
		}
		return nil, fmt.Errorf("UserRepo.GetByEmail: %w", err)
	}
	return mapEntUser(u), nil
}

// List returns a paginated list of active users ordered by created_at DESC.
// For more complex listing (search, filters), see user_query.go.
func (r *UserRepo) List(ctx context.Context, p domain.Pagination) ([]*domain.User, int, error) {
	query := r.client.User.Query().
		Where(entuser.ActiveEQ(true)).
		Order(ent.Desc(entuser.FieldCreatedAt))

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("UserRepo.List count: %w", err)
	}

	users, err := query.
		Limit(p.Limit).
		Offset(p.Offset).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("UserRepo.List: %w", err)
	}

	result := make([]*domain.User, len(users))
	for i, u := range users {
		result[i] = mapEntUser(u)
	}
	return result, total, nil
}

// ── Mapping ───────────────────────────────────────────────────────────────────

// mapEntUser converts an *ent.User to *domain.User.
// This is the only place where Ent types cross the repository boundary.
func mapEntUser(u *ent.User) *domain.User {
	return &domain.User{
		ID:           u.ID,
		Email:        u.Email,
		Name:         u.Name,
		PasswordHash: u.PasswordHash,
		Role:         domain.UserRole(u.Role),
		Active:       u.Active,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
}
