//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"
)

// ── User CRUD Integration Tests ───────────────────────────────────────────────

// TestCreateUser_Success creates a user and verifies the response.
func TestCreateUser_Success(t *testing.T) {
	resp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "create_test@example.com",
		"name":     "Create Test",
		"password": "secure_password",
		"role":     "user",
	}, adminToken(t))
	mustBeStatus(t, resp, http.StatusCreated)

	var body map[string]any
	decodeJSON(t, resp, &body)

	if body["id"] == "" || body["id"] == nil {
		t.Error("expected id in response")
	}
	if body["email"] != "create_test@example.com" {
		t.Errorf("email = %v, want create_test@example.com", body["email"])
	}
	if body["name"] != "Create Test" {
		t.Errorf("name = %v, want Create Test", body["name"])
	}
	if _, hasPassword := body["password"]; hasPassword {
		t.Error("password must not appear in response")
	}
	if _, hasHash := body["password_hash"]; hasHash {
		t.Error("password_hash must not appear in response")
	}
}

// TestCreateUser_DuplicateEmail returns 409 Conflict.
func TestCreateUser_DuplicateEmail(t *testing.T) {
	tok := adminToken(t)
	payload := map[string]any{
		"email": "duplicate@example.com", "name": "First",
		"password": "password1", "role": "user",
	}
	resp1 := do(t, http.MethodPost, "/api/v1/users", payload, tok)
	mustBeStatus(t, resp1, http.StatusCreated)
	resp1.Body.Close()

	resp2 := do(t, http.MethodPost, "/api/v1/users", payload, tok)
	mustBeStatus(t, resp2, http.StatusConflict)
	resp2.Body.Close()
}

// TestGetUser_RequiresJWT returns 401 without a token.
func TestGetUser_RequiresJWT(t *testing.T) {
	resp := do(t, http.MethodGet, "/api/v1/users/some-id", nil, "")
	mustBeStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
}

// TestGetUser_Success creates a user then retrieves it by ID.
func TestGetUser_Success(t *testing.T) {
	tok := adminToken(t)

	// Create
	createResp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email": "get_user@example.com", "name": "Get User", "password": "password1", "role": "user",
	}, tok)
	mustBeStatus(t, createResp, http.StatusCreated)
	var createBody map[string]any
	decodeJSON(t, createResp, &createBody)
	userID := fmt.Sprintf("%v", createBody["id"])

	// Get by ID using admin token
	getResp := do(t, http.MethodGet, "/api/v1/users/"+userID, nil, tok)
	mustBeStatus(t, getResp, http.StatusOK)

	var getBody map[string]any
	decodeJSON(t, getResp, &getBody)
	if getBody["id"] != userID {
		t.Errorf("id = %v, want %v", getBody["id"], userID)
	}
	if getBody["email"] != "get_user@example.com" {
		t.Errorf("email = %v", getBody["email"])
	}
}

// TestListUsers_Pagination verifies total count and limit.
func TestListUsers_Pagination(t *testing.T) {
	tok := adminToken(t)

	// Create 3 users
	for i := range 3 {
		resp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
			"email":    fmt.Sprintf("pag_%d@example.com", i),
			"name":     fmt.Sprintf("Pag User %d", i),
			"password": "password1", "role": "user",
		}, tok)
		resp.Body.Close()
	}

	// List with limit=2
	listResp := do(t, http.MethodGet, "/api/v1/users?limit=2&offset=0", nil, tok)
	mustBeStatus(t, listResp, http.StatusOK)

	var listBody map[string]any
	decodeJSON(t, listResp, &listBody)

	users, ok := listBody["data"].([]any) // response: { data: [...], total: N, limit: 2, offset: 0 }
	if !ok {
		t.Fatalf("expected data array in response, got: %T %v", listBody["data"], listBody["data"])
	}
	if len(users) != 2 {
		t.Errorf("len(users) = %d, want 2 (limit=2)", len(users))
	}
	total, ok := listBody["total"].(float64)
	if !ok {
		t.Fatalf("expected total in response, got: %T %v", listBody["total"], listBody["total"])
	}
	if int(total) < 3 {
		t.Errorf("total = %v, want >= 3", total)
	}
}

// TestUpdateUser_Success updates a user's name via real login token.
func TestUpdateUser_Success(t *testing.T) {
	tok := adminToken(t)

	// Create user
	createResp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email": "update_user@example.com", "name": "Old Name", "password": "password1", "role": "user",
	}, tok)
	mustBeStatus(t, createResp, http.StatusCreated)
	var createBody map[string]any
	decodeJSON(t, createResp, &createBody)
	userID := fmt.Sprintf("%v", createBody["id"])

	// Login to get that user's own token
	loginResp := do(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": "update_user@example.com", "password": "password1",
	}, "")
	mustBeStatus(t, loginResp, http.StatusOK)
	var loginBody map[string]any
	decodeJSON(t, loginResp, &loginBody)
	userToken := fmt.Sprintf("%v", loginBody["access_token"])

	// Update name with user's own token
	updateResp := do(t, http.MethodPut, "/api/v1/users/"+userID, map[string]any{
		"name": "New Name",
	}, userToken)
	mustBeStatus(t, updateResp, http.StatusOK)

	var body map[string]any
	decodeJSON(t, updateResp, &body)
	if body["name"] != "New Name" {
		t.Errorf("name = %v, want New Name", body["name"])
	}
}

// TestDeleteUser_Success soft-deletes a user.
func TestDeleteUser_Success(t *testing.T) {
	tok := adminToken(t)

	// Create user
	createResp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email": "delete_user@example.com", "name": "Delete Me", "password": "password1", "role": "user",
	}, tok)
	mustBeStatus(t, createResp, http.StatusCreated)
	var createBody map[string]any
	decodeJSON(t, createResp, &createBody)
	userID := fmt.Sprintf("%v", createBody["id"])

	// Delete using admin token
	delResp := do(t, http.MethodDelete, "/api/v1/users/"+userID, nil, tok)
	mustBeStatus(t, delResp, http.StatusNoContent)
	delResp.Body.Close()
}
