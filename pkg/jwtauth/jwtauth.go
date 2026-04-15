// Package jwtauth provides JWT token generation and validation for axe.
// Uses HS256 signing with a secret key from config.
// Claims follow the standard + add role and user_id.
package jwtauth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

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
	UserID string `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service handles token generation and validation.
type Service struct {
	secret        []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	issuer        string
}

// New creates a new JWT Service.
//
//	secret    — HMAC-SHA256 signing secret (min 32 bytes recommended)
//	accessTTL — access token lifetime (e.g. 15m)
//	refreshTTL — refresh token lifetime (e.g. 7d)
func New(secret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "axe",
	}
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
		UserID: userID.String(),
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(), // JTI — unique token identifier for revocation
			Issuer:    s.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("jwtauth: sign access token: %w", err)
	}

	refreshClaims := Claims{
		UserID: userID.String(),
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(), // JTI — separate ID for each refresh token
			Issuer:    s.issuer,
			Subject:   userID.String(),
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
// Returns ErrTokenExpired or ErrTokenInvalid on failure.
func (s *Service) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})

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

// ── Sentinel errors ───────────────────────────────────────────────────────────

var (
	ErrTokenExpired  = errors.New("token expired")
	ErrTokenInvalid  = errors.New("token invalid")
	ErrTokenRevoked  = errors.New("token revoked") // JTI found in blocklist
)
