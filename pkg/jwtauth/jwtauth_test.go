package jwtauth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/jwtauth"
)

const testSecret = "super-secret-key-min-32-bytes-long!!"

func newTestService() *jwtauth.Service {
	svc, err := jwtauth.New(testSecret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		panic(err)
	}
	return svc
}

// ─────────────────────────────────────────────────────────────────────────────
// GenerateTokenPair
// ─────────────────────────────────────────────────────────────────────────────

func TestGenerateTokenPair_HappyPath(t *testing.T) {
	svc := newTestService()
	id := uuid.New()

	pair, err := svc.GenerateTokenPair(id, "user")
	require.NoError(t, err)
	assert.NotEmpty(t, pair.AccessToken)
	assert.NotEmpty(t, pair.RefreshToken)
	assert.Equal(t, int64(900), pair.ExpiresIn) // 15m = 900s
}

func TestGenerateTokenPair_AccessAndRefreshDiffer(t *testing.T) {
	svc := newTestService()
	pair, err := svc.GenerateTokenPair(uuid.New(), "user")
	require.NoError(t, err)
	assert.NotEqual(t, pair.AccessToken, pair.RefreshToken,
		"access and refresh tokens must differ (different JTI + expiry)")
}

func TestGenerateTokenPair_DifferentRoles(t *testing.T) {
	svc := newTestService()
	id := uuid.New()

	roles := []string{"user", "admin", "moderator", ""}
	for _, role := range roles {
		t.Run("role="+role, func(t *testing.T) {
			pair, err := svc.GenerateTokenPair(id, role)
			require.NoError(t, err)

			claims, err := svc.Validate(pair.AccessToken)
			require.NoError(t, err)
			assert.Equal(t, role, claims.Role)
		})
	}
}

func TestGenerateTokenPair_UniqueJTI(t *testing.T) {
	svc := newTestService()
	id := uuid.New()

	pair1, _ := svc.GenerateTokenPair(id, "user")
	pair2, _ := svc.GenerateTokenPair(id, "user")

	c1, _ := svc.Validate(pair1.AccessToken)
	c2, _ := svc.Validate(pair2.AccessToken)

	assert.NotEqual(t, c1.JTI(), c2.JTI(),
		"each token pair should have a unique JTI for blocklist")
}

func TestGenerateTokenPair_TokenContainsUserID(t *testing.T) {
	svc := newTestService()
	id := uuid.New()

	pair, err := svc.GenerateTokenPair(id, "admin")
	require.NoError(t, err)

	claims, err := svc.Validate(pair.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, id.String(), claims.UserID)
	assert.Equal(t, id.String(), claims.Subject)
}

// ─────────────────────────────────────────────────────────────────────────────
// Validate — happy paths
// ─────────────────────────────────────────────────────────────────────────────

func TestValidate_ValidToken(t *testing.T) {
	svc := newTestService()
	id := uuid.New()

	pair, err := svc.GenerateTokenPair(id, "admin")
	require.NoError(t, err)

	claims, err := svc.Validate(pair.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, id.String(), claims.UserID)
	assert.Equal(t, "admin", claims.Role)
	assert.Equal(t, "axe", claims.Issuer)
	assert.NotEmpty(t, claims.JTI())
}

func TestValidate_RefreshToken(t *testing.T) {
	svc := newTestService()
	pair, err := svc.GenerateTokenPair(uuid.New(), "user")
	require.NoError(t, err)

	// Refresh token should also be valid (same secret, different expiry).
	claims, err := svc.Validate(pair.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "user", claims.Role)
}

// ─────────────────────────────────────────────────────────────────────────────
// Validate — error paths
// ─────────────────────────────────────────────────────────────────────────────

func TestValidate_ExpiredToken(t *testing.T) {
	// TTL of -1ns → immediately expired
	svc, _ := jwtauth.New(testSecret, -1*time.Nanosecond, time.Hour)
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")

	_, err := svc.Validate(pair.AccessToken)
	assert.ErrorIs(t, err, jwtauth.ErrTokenExpired)
}

func TestValidate_TamperedToken(t *testing.T) {
	svc := newTestService()
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")

	tampered := pair.AccessToken + "tampered"
	_, err := svc.Validate(tampered)
	assert.ErrorIs(t, err, jwtauth.ErrTokenInvalid)
}

func TestValidate_WrongSecret(t *testing.T) {
	svc1, _ := jwtauth.New("secret-one-at-least-32-bytes-long!!", 15*time.Minute, 7*24*time.Hour)
	svc2, _ := jwtauth.New("secret-two-at-least-32-bytes-long!!", 15*time.Minute, 7*24*time.Hour)

	pair, _ := svc1.GenerateTokenPair(uuid.New(), "user")
	_, err := svc2.Validate(pair.AccessToken)
	assert.ErrorIs(t, err, jwtauth.ErrTokenInvalid, "token signed with different secret should be invalid")
}

func TestValidate_MalformedToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty string", ""},
		{"random text", "not-a-jwt"},
		{"partial jwt", "header.payload"},
		{"three parts garbage", "aaa.bbb.ccc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTestService().Validate(tc.token)
			assert.ErrorIs(t, err, jwtauth.ErrTokenInvalid)
		})
	}
}

func TestValidate_WrongSigningMethod(t *testing.T) {
	// Create a token with "none" algorithm — must be rejected.
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"uid":  "user-123",
		"role": "admin",
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	tokenStr, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = newTestService().Validate(tokenStr)
	assert.ErrorIs(t, err, jwtauth.ErrTokenInvalid, "none algorithm must be rejected")
}

// ─────────────────────────────────────────────────────────────────────────────
// Claims helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestClaims_JTI(t *testing.T) {
	svc := newTestService()
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")

	claims, err := svc.Validate(pair.AccessToken)
	require.NoError(t, err)

	jti := claims.JTI()
	assert.NotEmpty(t, jti)
	// JTI should be a valid UUID.
	_, err = uuid.Parse(jti)
	assert.NoError(t, err, "JTI should be a valid UUID")
}

func TestClaims_RemainingTTL(t *testing.T) {
	svc := newTestService()
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")

	claims, err := svc.Validate(pair.AccessToken)
	require.NoError(t, err)

	ttl := claims.RemainingTTL()
	// Should be close to 15 minutes (minus test execution time).
	assert.True(t, ttl > 14*time.Minute, "TTL should be > 14m, got %v", ttl)
	assert.True(t, ttl <= 15*time.Minute, "TTL should be <= 15m, got %v", ttl)
}

func TestClaims_RemainingTTL_Expired(t *testing.T) {
	// Expired claims should return 0 TTL.
	claims := &jwtauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
	}
	assert.Equal(t, time.Duration(0), claims.RemainingTTL())
}

func TestClaims_RemainingTTL_NoExpiry(t *testing.T) {
	// Claims without ExpiresAt should return 0.
	claims := &jwtauth.Claims{}
	assert.Equal(t, time.Duration(0), claims.RemainingTTL())
}

// ─────────────────────────────────────────────────────────────────────────────
// Sentinel errors
// ─────────────────────────────────────────────────────────────────────────────

func TestSentinelErrors_AreDistinct(t *testing.T) {
	assert.NotEqual(t, jwtauth.ErrTokenExpired, jwtauth.ErrTokenInvalid)
	assert.NotEqual(t, jwtauth.ErrTokenExpired, jwtauth.ErrTokenRevoked)
	assert.NotEqual(t, jwtauth.ErrTokenInvalid, jwtauth.ErrTokenRevoked)
}

func TestSentinelErrors_HaveMessages(t *testing.T) {
	assert.Contains(t, jwtauth.ErrTokenExpired.Error(), "expired")
	assert.Contains(t, jwtauth.ErrTokenInvalid.Error(), "invalid")
	assert.Contains(t, jwtauth.ErrTokenRevoked.Error(), "revoked")
}

// ─────────────────────────────────────────────────────────────────────────────
// Full lifecycle: generate → validate → refresh → expire
// ─────────────────────────────────────────────────────────────────────────────

func TestFullLifecycle_IssueValidateExpire(t *testing.T) {
	svc := newTestService()
	userID := uuid.New()

	// 1. Generate tokens
	pair, err := svc.GenerateTokenPair(userID, "user")
	require.NoError(t, err)

	// 2. Validate access token
	accessClaims, err := svc.Validate(pair.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, userID.String(), accessClaims.UserID)

	// 3. Validate refresh token
	refreshClaims, err := svc.Validate(pair.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, userID.String(), refreshClaims.UserID)

	// 4. Access and refresh have different JTIs (for independent revocation)
	assert.NotEqual(t, accessClaims.JTI(), refreshClaims.JTI())

	// 5. Simulate refresh: generate new pair, old still valid (not revoked here)
	pair2, err := svc.GenerateTokenPair(userID, "user")
	require.NoError(t, err)
	assert.NotEqual(t, pair.AccessToken, pair2.AccessToken)
}
