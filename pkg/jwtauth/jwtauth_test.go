package jwtauth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/axe-cute/axe/pkg/jwtauth"
)

func newTestService() *jwtauth.Service {
	return jwtauth.New("super-secret-key-min-32-bytes-long!!", 15*time.Minute, 7*24*time.Hour)
}

func TestGenerateTokenPair_HappyPath(t *testing.T) {
	svc := newTestService()
	id := uuid.New()

	pair, err := svc.GenerateTokenPair(id, "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken == "" {
		t.Error("access token should not be empty")
	}
	if pair.RefreshToken == "" {
		t.Error("refresh token should not be empty")
	}
	if pair.ExpiresIn != 900 {
		t.Errorf("expires_in = %d, want 900", pair.ExpiresIn)
	}
}

func TestValidate_ValidToken(t *testing.T) {
	svc := newTestService()
	id := uuid.New()

	pair, err := svc.GenerateTokenPair(id, "admin")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	claims, err := svc.Validate(pair.AccessToken)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.UserID != id.String() {
		t.Errorf("UserID = %q, want %q", claims.UserID, id.String())
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want admin", claims.Role)
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	// TTL of -1ns → immediately expired
	svc := jwtauth.New("super-secret-key-min-32-bytes-long!!", -1*time.Nanosecond, time.Hour)
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")

	_, err := svc.Validate(pair.AccessToken)
	if err != jwtauth.ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestValidate_TamperedToken(t *testing.T) {
	svc := newTestService()
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")

	tampered := pair.AccessToken + "tampered"
	_, err := svc.Validate(tampered)
	if err != jwtauth.ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got: %v", err)
	}
}

func TestValidate_WrongAlgorithmToken(t *testing.T) {
	_, err := newTestService().Validate("not.a.jwt")
	if err != jwtauth.ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got: %v", err)
	}
}

func TestGenerateTokenPair_AccessAndRefreshDiffer(t *testing.T) {
	svc := newTestService()
	pair, _ := svc.GenerateTokenPair(uuid.New(), "user")
	if pair.AccessToken == pair.RefreshToken {
		t.Error("access and refresh tokens should differ (different expiry)")
	}
}
