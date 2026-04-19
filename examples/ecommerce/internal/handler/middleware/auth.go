package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/axe-cute/examples-ecommerce/pkg/apperror"
	"github.com/axe-cute/examples-ecommerce/pkg/jwtauth"
	"github.com/axe-cute/examples-ecommerce/pkg/logger"
)

type Blocklist interface {
	BlockToken(ctx context.Context, jti string, ttl time.Duration) error
	IsTokenBlocked(ctx context.Context, jti string) (bool, error)
}

type contextKey string

const claimsKey contextKey = "jwt_claims"

func ClaimsFromCtx(ctx context.Context) *jwtauth.Claims {
	v, _ := ctx.Value(claimsKey).(*jwtauth.Claims)
	return v
}

func JWTAuth(svc *jwtauth.Service, blocklist Blocklist) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.FromCtx(r.Context())
			token := extractBearerToken(r)
			if token == "" {
				WriteError(w, apperror.ErrUnauthorized.WithMessage("missing authorization header"))
				return
			}
			claims, err := svc.Validate(token)
			if err != nil {
				if err == jwtauth.ErrTokenExpired {
					log.Info("token expired", "ip", r.RemoteAddr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("token expired"))
				} else {
					log.Warn("invalid token", "ip", r.RemoteAddr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("invalid token"))
				}
				return
			}
			if blocklist != nil && claims.JTI() != "" {
				blocked, blErr := blocklist.IsTokenBlocked(r.Context(), claims.JTI())
				if blErr != nil {
					log.Warn("blocklist check failed — failing open", "error", blErr)
					// Fail-open: Redis down should not block all authenticated requests.
				} else if blocked {
					log.Info("token revoked", "jti", claims.JTI())
					WriteError(w, apperror.ErrUnauthorized.WithMessage("token revoked"))
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
		})
	}
}

func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromCtx(r.Context())
			if claims == nil {
				WriteError(w, apperror.ErrUnauthorized.WithMessage("authentication required"))
				return
			}
			if !hasRole(claims.Role, role) {
				WriteError(w, apperror.ErrForbidden.WithMessage("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type LoginResponse struct {
	*jwtauth.TokenPair
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func hasRole(userRole, required string) bool {
	if required == "admin" {
		return userRole == "admin"
	}
	return userRole == "user" || userRole == "admin"
}
