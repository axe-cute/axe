// Package apperror defines domain-level error types and sentinel errors.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError is a structured application error with an HTTP status code.
type AppError struct {
	HTTPStatus int
	Code       string
	Message    string
	Cause      error
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Cause }

// WithMessage returns a copy of the error with a new message.
func (e *AppError) WithMessage(msg string) *AppError {
	clone := *e
	clone.Message = msg
	return &clone
}

// WithCause returns a copy of the error with a wrapped cause.
func (e *AppError) WithCause(err error) *AppError {
	clone := *e
	clone.Cause = err
	return &clone
}

// Sentinel errors — use these in handlers and services.
var (
	ErrNotFound     = &AppError{HTTPStatus: http.StatusNotFound, Code: "NOT_FOUND", Message: "resource not found"}
	ErrInvalidInput = &AppError{HTTPStatus: http.StatusBadRequest, Code: "INVALID_INPUT", Message: "invalid input"}
	ErrUnauthorized = &AppError{HTTPStatus: http.StatusUnauthorized, Code: "UNAUTHORIZED", Message: "unauthorized"}
	ErrForbidden    = &AppError{HTTPStatus: http.StatusForbidden, Code: "FORBIDDEN", Message: "forbidden"}
	ErrConflict     = &AppError{HTTPStatus: http.StatusConflict, Code: "CONFLICT", Message: "conflict"}
	ErrInternal     = &AppError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "internal server error"}
)

// IsNotFound reports whether err is a 404 AppError.
func IsNotFound(err error) bool {
	var ae *AppError
	return errors.As(err, &ae) && ae.HTTPStatus == http.StatusNotFound
}
