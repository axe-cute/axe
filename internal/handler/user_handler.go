package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/axe-cute/axe/internal/domain"
	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/pkg/apperror"
	"github.com/axe-cute/axe/pkg/logger"
)

// UserHandler handles HTTP requests for the User domain.
// It depends on domain.UserService (interface), never concrete type.
type UserHandler struct {
	svc domain.UserService
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(svc domain.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// Routes returns a Chi router with all user endpoints mounted.
// Register in main.go: r.Mount("/api/v1/users", userHandler.Routes())
func (h *UserHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.CreateUser)
	r.Get("/", h.ListUsers)
	r.Get("/{id}", h.GetUser)
	r.Put("/{id}", h.UpdateUser)
	r.Delete("/{id}", h.DeleteUser)
	return r
}

// ── Request / Response DTOs ───────────────────────────────────────────────────

type createUserRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

type updateUserRequest struct {
	Name *string `json:"name,omitempty"`
	Role *string `json:"role,omitempty"`
}

type userResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type listUsersResponse struct {
	Data   []*userResponse `json:"data"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

func toUserResponse(u *domain.User) *userResponse {
	return &userResponse{
		ID:        u.ID.String(),
		Email:     u.Email,
		Name:      u.Name,
		Role:      string(u.Role),
		Active:    u.Active,
		CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: u.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// CreateUser godoc
// POST /api/v1/users
// Body: createUserRequest
// Response: 201 userResponse
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromCtx(ctx)

	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}

	role := domain.RoleUser
	if req.Role != "" {
		role = domain.UserRole(req.Role)
	}

	user, err := h.svc.CreateUser(ctx, domain.CreateUserInput{
		Email:    req.Email,
		Name:     req.Name,
		Password: req.Password,
		Role:     role,
	})
	if err != nil {
		log.Warn("create user failed", "error", err)
		middleware.WriteError(w, err)
		return
	}

	middleware.WriteJSON(w, http.StatusCreated, toUserResponse(user))
}

// GetUser godoc
// GET /api/v1/users/{id}
// Response: 200 userResponse
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	user, err := h.svc.GetUser(r.Context(), id)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	middleware.WriteJSON(w, http.StatusOK, toUserResponse(user))
}

// UpdateUser godoc
// PUT /api/v1/users/{id}
// Body: updateUserRequest
// Response: 200 userResponse
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := parseUUID(r, "id")
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}

	input := domain.UpdateUserInput{Name: req.Name}
	if req.Role != nil {
		role := domain.UserRole(*req.Role)
		input.Role = &role
	}

	user, err := h.svc.UpdateUser(ctx, id, input)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	middleware.WriteJSON(w, http.StatusOK, toUserResponse(user))
}

// DeleteUser godoc
// DELETE /api/v1/users/{id}
// Response: 204 No Content
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	if err := h.svc.DeleteUser(r.Context(), id); err != nil {
		middleware.WriteError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListUsers godoc
// GET /api/v1/users?limit=20&offset=0
// Response: 200 listUsersResponse
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 20)
	offset := parseIntQuery(r, "offset", 0)

	users, total, err := h.svc.ListUsers(r.Context(), domain.Pagination{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	resp := &listUsersResponse{
		Data:   make([]*userResponse, len(users)),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
	for i, u := range users {
		resp.Data[i] = toUserResponse(u)
	}

	middleware.WriteJSON(w, http.StatusOK, resp)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func parseUUID(r *http.Request, param string) (uuid.UUID, error) {
	raw := chi.URLParam(r, param)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.ErrInvalidInput.WithMessage("invalid UUID: " + param)
	}
	return id, nil
}

func parseIntQuery(r *http.Request, key string, defaultVal int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}
