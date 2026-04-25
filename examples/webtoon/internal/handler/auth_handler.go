// Package handler — auth handler.
// This example does NOT include a full User domain; it exposes a simple
// dev-login endpoint that mints a JWT for any email/password. Suitable for
// showcasing the framework end-to-end. Do NOT use in production.
package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/axe-cute/examples-webtoon/internal/handler/middleware"
	"github.com/axe-cute/examples-webtoon/pkg/apperror"
	"github.com/axe-cute/examples-webtoon/pkg/jwtauth"
)

// AuthHandler issues JWT tokens for the webtoon example.
type AuthHandler struct {
	jwt *jwtauth.Service
}

func NewAuthHandler(jwt *jwtauth.Service) *AuthHandler {
	return &AuthHandler{jwt: jwt}
}

func (h *AuthHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/login", h.Login)
	r.Post("/register", h.Login) // alias — example has no separate user store
	return r
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
}

// Login (dev-mode): accepts any email + non-empty password. The returned
// UserID is a deterministic UUIDv5 derived from the email, so the same email
// always yields the same user identity (stable bookmarks across sessions).
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("valid email required"))
		return
	}
	if len(req.Password) < 4 {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("password must be at least 4 characters"))
		return
	}

	userID := deterministicUserID(email)
	role := "user"
	if email == "admin@axe.dev" {
		role = "admin"
	}

	pair, err := h.jwt.GenerateTokenPair(userID, role)
	if err != nil {
		middleware.WriteError(w, apperror.ErrInternal.WithCause(err))
		return
	}

	middleware.WriteJSON(w, http.StatusOK, loginResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		UserID:       userID.String(),
		Email:        email,
		Role:         role,
	})
}

// deterministicUserID generates a stable UUIDv5 from the email so that the
// same email always maps to the same user identity (within this demo).
func deterministicUserID(email string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("webtoon-demo:"+email))
}
