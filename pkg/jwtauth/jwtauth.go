// Package jwtauth provides JWT token generation and validation for axe.
// Uses HS256 signing with a secret key from config.
// Claims follow the standard + add role, user_id, and token_type.
package jwtauth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Token type constants distinguish access from refresh tokens.
// Prevents token confusion attacks (P0-03).
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// MinSecretLength is the minimum required secret length in bytes.
// NIST SP 800-107 requires HMAC-SHA256 keys to be at least 256 bits (32 bytes).
const MinSecretLength = 32

// JTI returns the JWT ID (jti) claim from a Claims struct.
// Used as blocklist key for token revocation.
func (c *Claims) JTI() string {
	return c.RegisteredClaims.ID
}

// RemainingTTL returns how long until the token expires.
// Used to set the correct Redis TTL for the blocklist entry.
func (c *Claims) RemainingTTL() time.Duration {
	if c.ExpiresAt == nil {
		return 0
	}
	ttl := time.Until(c.ExpiresAt.Time)
	if ttl < 0 {
		return 0
	}
	return ttl
}

// ── Claims ────────────────────────────────────────────────────────────────────

// Claims extends jwt.RegisteredClaims with axe-specific fields.
type Claims struct {
	UserID    string `json:"uid"`
	Role      string `json:"role"`
	TokenType string `json:"typ"` // "access" or "refresh" — prevents token confusion attacks
	jwt.RegisteredClaims
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service handles token generation and validation.
type Service struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

// New creates a new JWT Service.
// Returns an error if the secret is shorter than MinSecretLength (32 bytes).
//
//	secret     — HMAC-SHA256 signing secret (min 32 bytes required)
//	accessTTL  — access token lifetime (e.g. 15m)
//	refreshTTL — refresh token lifetime (e.g. 7d)
func New(secret string, accessTTL, refreshTTL time.Duration) (*Service, error) {
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("jwtauth: secret must be at least %d bytes, got %d", MinSecretLength, len(secret))
	}
	return &Service{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "axe",
	}, nil
}

// TokenPair holds both the access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds until access token expires
}

// GenerateTokenPair mints a fresh access + refresh token pair for a user.
func (s *Service) GenerateTokenPair(userID uuid.UUID, role string) (*TokenPair, error) {
	now := time.Now()

	accessClaims := Claims{
		UserID:    userID.String(),
		Role:      role,
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(), // JTI — unique token identifier for revocation
			Issuer:    s.issuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{s.issuer},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("jwtauth: sign access token: %w", err)
	}

	refreshClaims := Claims{
		UserID:    userID.String(),
		Role:      role,
		TokenType: TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(), // JTI — separate ID for each refresh token
			Issuer:    s.issuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{s.issuer},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
		},
	}

	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("jwtauth: sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.accessTTL.Seconds()),
	}, nil
}

// Validate parses and validates a token string, returning its Claims.
// Accepts both access and refresh tokens. Use ValidateAccess or ValidateRefresh
// for type-checked validation.
// Returns ErrTokenExpired or ErrTokenInvalid on failure.
func (s *Service) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithAudience(s.issuer))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}

	return claims, nil
}

// ValidateAccess validates a token and ensures it is an access token.
// Rejects refresh tokens — use this in API middleware.
func (s *Service) ValidateAccess(tokenStr string) (*Claims, error) {
	claims, err := s.Validate(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != TokenTypeAccess {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// ValidateRefresh validates a token and ensures it is a refresh token.
// Rejects access tokens — use this in the /auth/refresh endpoint.
func (s *Service) ValidateRefresh(tokenStr string) (*Claims, error) {
	claims, err := s.Validate(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != TokenTypeRefresh {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// ── Sentinel errors ───────────────────────────────────────────────────────────

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
	ErrTokenRevoked = errors.New("token revoked") // JTI found in blocklist
)
