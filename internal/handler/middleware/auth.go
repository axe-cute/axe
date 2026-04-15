package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/axe-go/axe/pkg/apperror"
	"github.com/axe-go/axe/pkg/jwtauth"
	"github.com/axe-go/axe/pkg/logger"
)

// ── Context keys ──────────────────────────────────────────────────────────────

type contextKey string

const (
	claimsKey contextKey = "jwt_claims"
)

// ClaimsFromCtx retrieves the JWT Claims from context.
// Returns nil if no claims are present (unauthenticated route).
func ClaimsFromCtx(ctx context.Context) *jwtauth.Claims {
	v, _ := ctx.Value(claimsKey).(*jwtauth.Claims)
	return v
}

// ── JWTAuth middleware ────────────────────────────────────────────────────────

// JWTAuth validates the Bearer token from the Authorization header.
// On success, injects *jwtauth.Claims into the request context.
// Usage: r.Use(middleware.JWTAuth(jwtSvc))
func JWTAuth(svc *jwtauth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				WriteError(w, apperror.ErrUnauthorized.WithMessage("missing authorization header"))
				return
			}

			claims, err := svc.Validate(token)
			if err != nil {
				log := logger.FromCtx(r.Context())
				if err == jwtauth.ErrTokenExpired {
					log.Info("token expired", "ip", r.RemoteAddr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("token expired"))
				} else {
					log.Warn("invalid token", "ip", r.RemoteAddr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("invalid token"))
				}
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns a middleware that enforces a minimum role.
// Roles: "user" < "admin"
// Usage: r.With(middleware.RequireRole("admin")).Get("/admin", handler)
func RequireRole(requiredRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromCtx(r.Context())
			if claims == nil {
				WriteError(w, apperror.ErrUnauthorized.WithMessage("authentication required"))
				return
			}
			if !hasRole(claims.Role, requiredRole) {
				WriteError(w, apperror.ErrForbidden.WithMessage("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── Auth handler helpers (login / refresh / logout) ───────────────────────────

// These are lightweight handler functions — mount in main.go under /api/v1/auth.

// LoginResponse is returned on successful authentication.
type LoginResponse struct {
	*jwtauth.TokenPair
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// extractBearerToken extracts the token from "Authorization: Bearer <token>".
func extractBearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// hasRole checks whether userRole satisfies the required role.
// Role hierarchy: admin > user
func hasRole(userRole, required string) bool {
	if required == "admin" {
		return userRole == "admin"
	}
	// "user" level: both user and admin have access
	return userRole == "user" || userRole == "admin"
}
