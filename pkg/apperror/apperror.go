// Package apperror defines the application error taxonomy.
// All layers MUST use these types — never create raw errors at handler level.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError is the canonical error type for axe.
// It carries an error code, human-readable message, HTTP status, and an optional cause.
type AppError struct {
	Code       string
	Message    string
	HTTPStatus int
	Cause      error
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap allows errors.Is / errors.As to traverse the chain.
func (e *AppError) Unwrap() error {
	return e.Cause
}

// Is reports whether the target matches this error's code.
// Enables: errors.Is(err, apperror.ErrNotFound)
func (e *AppError) Is(target error) bool {
	var t *AppError
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// WithMessage returns a copy of this error with a new message.
// Usage: apperror.ErrNotFound.WithMessage("user not found")
func (e *AppError) WithMessage(msg string) *AppError {
	return &AppError{
		Code:       e.Code,
		Message:    msg,
		HTTPStatus: e.HTTPStatus,
		Cause:      e.Cause,
	}
}

// WithCause returns a copy of this error wrapping the given cause.
// Usage: apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
func (e *AppError) WithCause(cause error) *AppError {
	return &AppError{
		Code:       e.Code,
		Message:    e.Message,
		HTTPStatus: e.HTTPStatus,
		Cause:      cause,
	}
}

// ── Sentinel errors ────────────────────────────────────────────────────────────
// These are the ONLY error types allowed at service/handler boundaries.

var (
	// ErrNotFound is returned when a resource does not exist.
	ErrNotFound = &AppError{
		Code:       "NOT_FOUND",
		Message:    "resource not found",
		HTTPStatus: http.StatusNotFound,
	}

	// ErrInvalidInput is returned when the request payload fails validation.
	ErrInvalidInput = &AppError{
		Code:       "INVALID_INPUT",
		Message:    "invalid input",
		HTTPStatus: http.StatusBadRequest,
	}

	// ErrUnauthorized is returned when authentication is missing or invalid.
	ErrUnauthorized = &AppError{
		Code:       "UNAUTHORIZED",
		Message:    "authentication required",
		HTTPStatus: http.StatusUnauthorized,
	}

	// ErrForbidden is returned when the authenticated user lacks permission.
	ErrForbidden = &AppError{
		Code:       "FORBIDDEN",
		Message:    "insufficient permissions",
		HTTPStatus: http.StatusForbidden,
	}

	// ErrConflict is returned when a business rule is violated (e.g. duplicate email).
	ErrConflict = &AppError{
		Code:       "CONFLICT",
		Message:    "resource conflict",
		HTTPStatus: http.StatusConflict,
	}

	// ErrInternal is returned for unexpected infrastructure failures.
	// Never expose internal details to clients.
	ErrInternal = &AppError{
		Code:       "INTERNAL_ERROR",
		Message:    "an internal error occurred",
		HTTPStatus: http.StatusInternalServerError,
	}

	// ErrUnprocessable is returned when the request is well-formed but semantically invalid.
	ErrUnprocessable = &AppError{
		Code:       "UNPROCESSABLE",
		Message:    "unprocessable request",
		HTTPStatus: http.StatusUnprocessableEntity,
	}
)

// ── Helpers ────────────────────────────────────────────────────────────────────

// AsAppError extracts an *AppError from the error chain.
// Returns (nil, false) if the error is not an *AppError.
func AsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// IsNotFound reports whether the error is ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsConflict reports whether the error is ErrConflict.
func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}

// IsUnauthorized reports whether the error is ErrUnauthorized.
func IsUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}
