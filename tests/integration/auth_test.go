//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"
)

// ── Auth Integration Tests ────────────────────────────────────────────────────

// TestLogin_Success verifies that a registered user can log in and receive tokens.
func TestLogin_Success(t *testing.T) {
	tok := adminToken(t)

	// Register a user via authenticated endpoint
	createResp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "login_test@example.com",
		"name":     "Login Test",
		"password": "password123",
		"role":     "user",
	}, tok)
	mustBeStatus(t, createResp, http.StatusCreated)
	createResp.Body.Close()

	// Login (public endpoint — no token needed)
	resp := do(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email":    "login_test@example.com",
		"password": "password123",
	}, "")
	mustBeStatus(t, resp, http.StatusOK)

	var body map[string]any
	decodeJSON(t, resp, &body)

	if body["access_token"] == "" || body["access_token"] == nil {
		t.Error("expected access_token in response")
	}
	if body["refresh_token"] == "" || body["refresh_token"] == nil {
		t.Error("expected refresh_token in response")
	}
	if body["user_id"] == "" || body["user_id"] == nil {
		t.Error("expected user_id in response")
	}
	if body["role"] != "user" {
		t.Errorf("role = %v, want user", body["role"])
	}
}

// TestLogin_WrongPassword returns 401 on bad credentials.
func TestLogin_WrongPassword(t *testing.T) {
	tok := adminToken(t)
	do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email": "wrong_pw@example.com", "name": "WrongPW",
		"password": "correct_password", "role": "user",
	}, tok).Body.Close()

	resp := do(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": "wrong_pw@example.com", "password": "wrong_password",
	}, "")
	mustBeStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
}

// TestLogin_UnknownEmail returns 401 on unknown email.
func TestLogin_UnknownEmail(t *testing.T) {
	resp := do(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": "nobody@example.com", "password": "irrelevant",
	}, "")
	mustBeStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
}

// TestGetMe_WithValidToken returns current user info.
func TestGetMe_WithValidToken(t *testing.T) {
	email := "me_test@example.com"
	tok := adminToken(t)

	// Register user
	do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email": email, "name": "Me Test User", "password": "password123", "role": "user",
	}, tok).Body.Close()

	// Login to get a real user token
	loginResp := do(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, "")
	mustBeStatus(t, loginResp, http.StatusOK)
	var loginBody map[string]any
	decodeJSON(t, loginResp, &loginBody)
	userToken := fmt.Sprintf("%v", loginBody["access_token"])

	// GET /auth/me with real user token
	meResp := do(t, http.MethodGet, "/api/v1/auth/me", nil, userToken)
	mustBeStatus(t, meResp, http.StatusOK)

	var meBody map[string]any
	decodeJSON(t, meResp, &meBody)
	if meBody["email"] != email {
		t.Errorf("email = %v, want %v", meBody["email"], email)
	}
}

// TestGetMe_WithoutToken returns 401.
func TestGetMe_WithoutToken(t *testing.T) {
	resp := do(t, http.MethodGet, "/api/v1/auth/me", nil, "")
	mustBeStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
}

// TestRefresh_Success rotates token pair.
func TestRefresh_Success(t *testing.T) {
	tok := adminToken(t)
	do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email": "refresh_test@example.com", "name": "Refresh", "password": "password123", "role": "user",
	}, tok).Body.Close()

	loginResp := do(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": "refresh_test@example.com", "password": "password123",
	}, "")
	mustBeStatus(t, loginResp, http.StatusOK)
	var loginBody map[string]any
	decodeJSON(t, loginResp, &loginBody)
	refreshToken := fmt.Sprintf("%v", loginBody["refresh_token"])
	oldAccess := fmt.Sprintf("%v", loginBody["access_token"])

	// Refresh tokens
	refreshResp := do(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": refreshToken,
	}, "")
	mustBeStatus(t, refreshResp, http.StatusOK)

	var refreshBody map[string]any
	decodeJSON(t, refreshResp, &refreshBody)

	newAccess := fmt.Sprintf("%v", refreshBody["access_token"])
	if newAccess == "" || newAccess == "<nil>" {
		t.Error("expected new access_token")
	}
	// New token should differ (different JTI + iat)
	if newAccess == oldAccess {
		t.Error("refreshed access token should differ from original (different JTI)")
	}
}
