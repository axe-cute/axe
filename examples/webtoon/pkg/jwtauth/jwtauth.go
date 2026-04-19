// Package jwtauth provides JWT token generation and validation.
package jwtauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims extends jwt.RegisteredClaims with application-specific fields.
type Claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// JTI returns the JWT ID claim (used as blocklist key for revocation).
func (c *Claims) JTI() string { return c.RegisteredClaims.ID }

// RemainingTTL returns how long until the token expires.
func (c *Claims) RemainingTTL() time.Duration {
	if c.ExpiresAt == nil { return 0 }
	if ttl := time.Until(c.ExpiresAt.Time); ttl > 0 { return ttl }
	return 0
}

// Service handles token generation and validation.
type Service struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

// New creates a new JWT Service.
func New(secret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "axe",
	}
}

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// GenerateTokenPair mints a fresh access + refresh token pair.
func (s *Service) GenerateTokenPair(userID uuid.UUID, role string) (*TokenPair, error) {
	now := time.Now()
	accessClaims := Claims{
		UserID: userID.String(), Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID: uuid.New().String(), Issuer: s.issuer, Subject: userID.String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.secret)
	if err != nil { return nil, fmt.Errorf("jwtauth: sign access: %w", err) }

	refreshClaims := Claims{
		UserID: userID.String(), Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID: uuid.New().String(), Issuer: s.issuer, Subject: userID.String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
		},
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.secret)
	if err != nil { return nil, fmt.Errorf("jwtauth: sign refresh: %w", err) }

	return &TokenPair{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresIn: int64(s.accessTTL.Seconds())}, nil
}

// Validate parses and validates a token string, returning its Claims.
func (s *Service) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) { return nil, ErrTokenExpired }
		return nil, ErrTokenInvalid
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid { return nil, ErrTokenInvalid }
	return claims, nil
}

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
	ErrTokenRevoked = errors.New("token revoked")
)

// ── Chi Middleware ────────────────────────────────────────────────────────────

type contextKey string

const claimsKey contextKey = "jwt_claims"

// ChiMiddleware returns a chi-compatible middleware that validates JWT tokens.
// It extracts the token from the Authorization header (Bearer <token>),
// validates it, and injects the claims into the request context.
// Requests without a valid token receive 401 Unauthorized.
func ChiMiddleware(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, `{"error":"missing or invalid Authorization header"}`, http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(auth, "Bearer ")
			claims, err := svc.Validate(tokenStr)
			if err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext extracts JWT claims from the request context.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*Claims)
	return claims, ok
}
