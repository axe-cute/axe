package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/axe-cute/axe/pkg/apperror"
	"github.com/axe-cute/axe/pkg/jwtauth"
	"github.com/axe-cute/axe/pkg/logger"
)

// Blocklist defines the contract for token revocation storage.
// Implemented by *cache.Client. Pass nil to disable blocklist checks.
// Both BlockToken (for logout) and IsTokenBlocked (for middleware check) are here
// so a single *cache.Client satisfies all auth needs.
type Blocklist interface {
	BlockToken(ctx context.Context, jti string, ttl time.Duration) error
	IsTokenBlocked(ctx context.Context, jti string) (bool, error)
}

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

// JWTAuth validates the Bearer token from the Authorization header.
// On success, injects *jwtauth.Claims into the request context.
// blocklist may be nil — when set, revoked tokens (JTI in Redis) are rejected.
//
// Usage: r.Use(middleware.JWTAuth(jwtSvc, cacheClient))
func JWTAuth(svc *jwtauth.Service, blocklist Blocklist) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.FromCtx(r.Context())
			token := extractBearerToken(r)
			if token == "" {
				WriteError(w, apperror.ErrUnauthorized.WithMessage("missing authorization header"))
				return
			}

			claims, err := svc.ValidateAccess(token)
			if err != nil {
				switch err {
				case jwtauth.ErrTokenExpired:
					log.Info("token expired", "ip", r.RemoteAddr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("token expired"))
				default:
					log.Warn("invalid token", "ip", r.RemoteAddr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("invalid token"))
				}
				return
			}

			// Blocklist check (token revocation via Redis JTI)
			if blocklist != nil && claims.JTI() != "" {
				blocked, blErr := blocklist.IsTokenBlocked(r.Context(), claims.JTI())
				if blErr != nil {
					// Fail-closed: reject request when blocklist is unavailable
					// to prevent revoked tokens from being accepted.
					log.Warn("blocklist check failed — rejecting request (fail-closed)", "error", blErr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("authentication service unavailable"))
					return
				} else if blocked {
					log.Info("token revoked", "jti", claims.JTI(), "user_id", claims.UserID)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("token revoked"))
					return
				}
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

// roleLevel maps known roles to their privilege level.
// Unknown roles are denied by default (fail-closed).
var roleLevel = map[string]int{
	"user":  1,
	"admin": 2,
}

// hasRole checks whether userRole satisfies the required role.
// Unknown roles are denied — fail-closed by design (P1-03).
func hasRole(userRole, required string) bool {
	userLvl, userOK := roleLevel[userRole]
	reqLvl, reqOK := roleLevel[required]
	if !userOK || !reqOK {
		return false // unknown roles are denied
	}
	return userLvl >= reqLvl
}
