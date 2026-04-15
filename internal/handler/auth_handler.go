package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/axe-go/axe/internal/domain"
	"github.com/axe-go/axe/internal/handler/middleware"
	"github.com/axe-go/axe/pkg/apperror"
	"github.com/axe-go/axe/pkg/jwtauth"
)

// AuthHandler handles login, token refresh, and logout.
type AuthHandler struct {
	userSvc domain.UserService
	jwt     *jwtauth.Service
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(userSvc domain.UserService, jwt *jwtauth.Service) *AuthHandler {
	return &AuthHandler{userSvc: userSvc, jwt: jwt}
}

// Routes returns the auth sub-router.
// Mount in main.go: r.Mount("/api/v1/auth", authHandler.Routes())
func (h *AuthHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/login", h.Login)
	r.Post("/refresh", h.Refresh)
	r.Post("/logout", h.Logout)
	r.With(middleware.JWTAuth(h.jwt)).Get("/me", h.Me)
	return r
}

// ── Request/Response DTOs ─────────────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	UserID       string `json:"user_id"`
	Role         string `json:"role"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// Login godoc
// POST /api/v1/auth/login
// Body: loginRequest
// Response: 200 tokenResponse
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}
	if req.Email == "" || req.Password == "" {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("email and password are required"))
		return
	}

	user, err := h.userSvc.Authenticate(r.Context(), req.Email, req.Password)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	pair, err := h.jwt.GenerateTokenPair(user.ID, string(user.Role))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInternal.WithCause(err))
		return
	}

	middleware.WriteJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		UserID:       user.ID.String(),
		Role:         string(user.Role),
	})
}

// Refresh godoc
// POST /api/v1/auth/refresh
// Body: refreshRequest
// Response: 200 tokenResponse
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}

	claims, err := h.jwt.Validate(req.RefreshToken)
	if err != nil {
		middleware.WriteError(w, apperror.ErrUnauthorized.WithMessage("invalid or expired refresh token"))
		return
	}

	uid, err := uuid.Parse(claims.UserID)
	if err != nil {
		middleware.WriteError(w, apperror.ErrUnauthorized.WithMessage("invalid token claims"))
		return
	}

	// Verify user still exists and is active
	user, err := h.userSvc.GetUser(r.Context(), uid)
	if err != nil {
		middleware.WriteError(w, apperror.ErrUnauthorized.WithMessage("user not found"))
		return
	}

	pair, err := h.jwt.GenerateTokenPair(user.ID, string(user.Role))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInternal.WithCause(err))
		return
	}

	middleware.WriteJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		UserID:       user.ID.String(),
		Role:         string(user.Role),
	})
}

// Logout godoc
// POST /api/v1/auth/logout
// Header: Authorization: Bearer <token>
// Response: 204 No Content
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// TODO (Story 3.4 extended): add token to Redis blocklist using cache.BlockToken()
	// For now: stateless logout (client discards token)
	w.WriteHeader(http.StatusNoContent)
}

// Me godoc
// GET /api/v1/auth/me  [requires JWT]
// Response: 200 userResponse (current authenticated user)
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFromCtx(r.Context())
	if claims == nil {
		middleware.WriteError(w, apperror.ErrUnauthorized)
		return
	}

	uid, err := uuid.Parse(claims.UserID)
	if err != nil {
		middleware.WriteError(w, apperror.ErrUnauthorized.WithMessage("invalid token"))
		return
	}

	user, err := h.userSvc.GetUser(r.Context(), uid)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	middleware.WriteJSON(w, http.StatusOK, toUserResponse(user))
}
