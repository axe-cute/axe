// Example 01: Using pkg/apperror in any Go project.
//
// This demonstrates how axe's error taxonomy provides consistent, typed
// HTTP error responses without the full framework.
//
// Run: go run ./examples/01-apperror
// Test: curl -i http://localhost:8080/users/42
//
//	curl -i http://localhost:8080/users/99
//	curl -i -X POST http://localhost:8080/users -d '{"email":"bad"}'
//	curl -i http://localhost:8080/admin/secrets
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/axe-cute/axe/pkg/apperror"
)

// ── Fake service layer ───────────────────────────────────────────────────────

type UserService struct{}

func (s *UserService) FindByID(id string) (map[string]string, error) {
	// Simulate: user 42 exists, anything else → not found.
	if id == "42" {
		return map[string]string{
			"id":    "42",
			"name":  "Nguyen Van A",
			"email": "a@example.com",
		}, nil
	}
	// Return a typed error — the handler doesn't need to know about HTTP status codes.
	return nil, apperror.ErrNotFound.WithMessage(fmt.Sprintf("user %s not found", id))
}

func (s *UserService) Create(email string) (map[string]string, error) {
	if email == "" || len(email) < 5 {
		return nil, apperror.ErrInvalidInput.WithMessage("email must be at least 5 characters")
	}
	if email == "taken@example.com" {
		return nil, apperror.ErrConflict.WithMessage("email already registered")
	}
	return map[string]string{"id": "99", "email": email}, nil
}

func (s *UserService) AdminAction() error {
	// Simulate: always forbidden for this demo.
	return apperror.ErrForbidden.WithMessage("admin role required")
}

// ── HTTP handlers ────────────────────────────────────────────────────────────

func main() {
	svc := &UserService{}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		user, err := svc.FindByID(id)
		if err != nil {
			writeError(w, err) // ← one function handles all error types
			return
		}
		writeJSON(w, http.StatusOK, user)
	})

	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		user, err := svc.Create(body.Email)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, user)
	})

	mux.HandleFunc("GET /admin/secrets", func(w http.ResponseWriter, r *http.Request) {
		if err := svc.AdminAction(); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"secret": "42"})
	})

	fmt.Println("🪓 Example 01: apperror")
	fmt.Println("   GET  http://localhost:8080/users/42     → 200 OK")
	fmt.Println("   GET  http://localhost:8080/users/99     → 404 Not Found")
	fmt.Println("   POST http://localhost:8080/users        → 400 / 409")
	fmt.Println("   GET  http://localhost:8080/admin/secrets → 403 Forbidden")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// ── Error response helper ────────────────────────────────────────────────────

func writeError(w http.ResponseWriter, err error) {
	// axe's apperror.AsAppError extracts the typed error from the chain.
	appErr, ok := apperror.AsAppError(err)
	if !ok {
		// Unknown error → 500 Internal Server Error (never expose raw errors).
		appErr = apperror.ErrInternal
	}

	writeJSON(w, appErr.HTTPStatus, map[string]interface{}{
		"error": map[string]interface{}{
			"code":    appErr.Code,
			"message": appErr.Message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
