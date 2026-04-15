// Package domain defines core business entities and repository/service interfaces.
// IMPORT RULE: Only stdlib packages allowed here (context, errors, fmt, strings, time, uuid).
// DO NOT import: database drivers, HTTP frameworks, ORM packages, or any infra.
package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ── Entity ────────────────────────────────────────────────────────────────────

// UserRole represents the possible roles a user can have.
type UserRole string

const (
	RoleUser  UserRole = "user"
	RoleAdmin UserRole = "admin"
)

// User is the core user entity.
// It must remain free of any infrastructure concerns.
type User struct {
	ID           uuid.UUID
	Email        string
	Name         string
	PasswordHash string // bcrypt hash — never expose in API responses
	Role         UserRole
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IsAdmin reports whether the user has admin role.
func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

// IsActive reports whether the user account is active.
func (u *User) IsActive() bool {
	return u.Active
}

// ── Value Objects ─────────────────────────────────────────────────────────────

// Pagination holds cursor-based pagination parameters.
type Pagination struct {
	Limit  int
	Offset int
}

// DefaultPagination returns sensible defaults.
func DefaultPagination() Pagination {
	return Pagination{Limit: 20, Offset: 0}
}

// Validate checks pagination bounds.
func (p Pagination) Validate() error {
	if p.Limit < 1 || p.Limit > 100 {
		return errors.New("limit must be between 1 and 100")
	}
	if p.Offset < 0 {
		return errors.New("offset must be non-negative")
	}
	return nil
}

// ── Repository Interface ───────────────────────────────────────────────────────

// UserRepository defines all data access operations for the User entity.
// Implemented in internal/repository/user_repo.go and user_query.go.
// RULE: implementations must extract transaction from context.
type UserRepository interface {
	// Write operations — implemented with Ent
	Create(ctx context.Context, user CreateUserInput) (*User, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateUserInput) (*User, error)
	Delete(ctx context.Context, id uuid.UUID) error // soft-delete (sets active=false)

	// Read operations — implemented with Ent (simple) or sqlc (complex)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context, pagination Pagination) ([]*User, int, error)
}

// ── Service Interface ──────────────────────────────────────────────────────────

// UserService defines the business operations for the User domain.
// Implemented in internal/service/user_service.go.
type UserService interface {
	CreateUser(ctx context.Context, input CreateUserInput) (*User, error)
	GetUser(ctx context.Context, id uuid.UUID) (*User, error)
	UpdateUser(ctx context.Context, id uuid.UUID, input UpdateUserInput) (*User, error)
	DeleteUser(ctx context.Context, id uuid.UUID) error
	ListUsers(ctx context.Context, pagination Pagination) ([]*User, int, error)
	Authenticate(ctx context.Context, email, password string) (*User, error)
}

// ── Input Types ───────────────────────────────────────────────────────────────

// CreateUserInput represents the data needed to create a new user.
type CreateUserInput struct {
	Email    string
	Name     string
	Password string // plain text — service layer hashes it
	Role     UserRole
}

// UpdateUserInput contains fields that can be updated.
// Pointer fields: nil means "no change".
type UpdateUserInput struct {
	Name   *string
	Role   *UserRole
	Active *bool
}
