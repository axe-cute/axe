package apperror_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/axe-go/axe/pkg/apperror"
)

func TestAppError_Error(t *testing.T) {
	t.Run("without cause", func(t *testing.T) {
		err := apperror.ErrNotFound.WithMessage("user not found")
		want := "[NOT_FOUND] user not found"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("sql: no rows")
		err := apperror.ErrNotFound.WithMessage("user not found").WithCause(cause)
		if err.Error() == "" {
			t.Error("Error() should not be empty")
		}
		if !errors.Is(err, apperror.ErrNotFound) {
			t.Error("errors.Is should match ErrNotFound")
		}
	})
}

func TestAppError_Is(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{"same sentinel", apperror.ErrNotFound, apperror.ErrNotFound, true},
		{"different sentinel", apperror.ErrNotFound, apperror.ErrConflict, false},
		{"wrapped sentinel", apperror.ErrNotFound.WithMessage("custom msg"), apperror.ErrNotFound, true},
		{"non-AppError target", apperror.ErrNotFound, errors.New("other"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, got, tt.want)
			}
		})
	}
}

func TestAppError_WithMessage(t *testing.T) {
	original := apperror.ErrNotFound
	modified := original.WithMessage("order not found")

	if modified.Message != "order not found" {
		t.Errorf("Message = %q, want %q", modified.Message, "order not found")
	}
	// Sentinel unchanged
	if original.Message != "resource not found" {
		t.Errorf("Original message mutated: %q", original.Message)
	}
	if modified.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want %d", modified.HTTPStatus, http.StatusNotFound)
	}
	if modified.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want NOT_FOUND", modified.Code)
	}
}

func TestAppError_WithCause(t *testing.T) {
	cause := errors.New("underlying db error")
	err := apperror.ErrInternal.WithCause(cause)

	if !errors.Is(err.Unwrap(), cause) {
		t.Error("Unwrap should return the original cause")
	}
}

func TestSentinels_HTTPStatus(t *testing.T) {
	cases := []struct {
		err    *apperror.AppError
		status int
	}{
		{apperror.ErrNotFound, http.StatusNotFound},
		{apperror.ErrInvalidInput, http.StatusBadRequest},
		{apperror.ErrUnauthorized, http.StatusUnauthorized},
		{apperror.ErrForbidden, http.StatusForbidden},
		{apperror.ErrConflict, http.StatusConflict},
		{apperror.ErrInternal, http.StatusInternalServerError},
		{apperror.ErrUnprocessable, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		if c.err.HTTPStatus != c.status {
			t.Errorf("%s: HTTPStatus = %d, want %d", c.err.Code, c.err.HTTPStatus, c.status)
		}
	}
}

func TestAsAppError(t *testing.T) {
	t.Run("AppError", func(t *testing.T) {
		err := apperror.ErrNotFound.WithMessage("not found")
		appErr, ok := apperror.AsAppError(err)
		if !ok {
			t.Fatal("AsAppError should return true for *AppError")
		}
		if appErr.Code != "NOT_FOUND" {
			t.Errorf("Code = %q, want NOT_FOUND", appErr.Code)
		}
	})

	t.Run("non-AppError", func(t *testing.T) {
		err := errors.New("raw error")
		_, ok := apperror.AsAppError(err)
		if ok {
			t.Fatal("AsAppError should return false for non-AppError")
		}
	})
}

func TestHelpers(t *testing.T) {
	if !apperror.IsNotFound(apperror.ErrNotFound) {
		t.Error("IsNotFound should be true for ErrNotFound")
	}
	if apperror.IsNotFound(apperror.ErrConflict) {
		t.Error("IsNotFound should be false for ErrConflict")
	}
	if !apperror.IsConflict(apperror.ErrConflict) {
		t.Error("IsConflict should be true for ErrConflict")
	}
	if !apperror.IsUnauthorized(apperror.ErrUnauthorized) {
		t.Error("IsUnauthorized should be true for ErrUnauthorized")
	}
}
