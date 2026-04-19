package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/axe-cute/axe/internal/handler"
)

// ── Post handler: Create + Update ────────────────────────────────────────────

func TestPost_Create_201(t *testing.T) {
	body := `{"title":"Hello","body":"World","published":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	setupPostRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPost_Create_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	setupPostRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPost_Update_200(t *testing.T) {
	body := `{"title":"Updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/posts/"+uuid.New().String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	setupPostRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPost_Update_InvalidUUID(t *testing.T) {
	body := `{"title":"Updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/posts/not-a-uuid", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	setupPostRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPost_Update_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/posts/"+uuid.New().String(), strings.NewReader("{broken"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	setupPostRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPost_Get_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/not-uuid", nil)
	rec := httptest.NewRecorder()
	setupPostRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPost_Delete_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/bad-id", nil)
	rec := httptest.NewRecorder()
	setupPostRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ── OpenAPI handler ──────────────────────────────────────────────────────────

func TestOpenAPI_Spec(t *testing.T) {
	h := handler.NewOpenAPIHandler()
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	h.Spec(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "yaml") {
		t.Errorf("Content-Type = %q, want yaml", ct)
	}
}

func TestOpenAPI_SwaggerUI(t *testing.T) {
	h := handler.NewOpenAPIHandler()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	h.SwaggerUI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "swagger-ui") {
		t.Error("expected swagger-ui in response body")
	}
}

func TestOpenAPI_Redoc(t *testing.T) {
	h := handler.NewOpenAPIHandler()
	req := httptest.NewRequest(http.MethodGet, "/docs/redoc", nil)
	rec := httptest.NewRecorder()
	h.Redoc(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "redoc") {
		t.Error("expected redoc in response body")
	}
}

// ── User handler: UpdateUser + ListUsers pagination ──────────────────────────

func TestUser_Update_200(t *testing.T) {
	svc := &mockUserService{}
	r := setupRouter(svc)

	body := `{"name":"Updated Name"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+uuid.New().String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestUser_Update_WithRole(t *testing.T) {
	svc := &mockUserService{}
	r := setupRouter(svc)

	body := `{"name":"Admin User","role":"admin"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+uuid.New().String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestUser_Update_InvalidUUID(t *testing.T) {
	svc := &mockUserService{}
	r := setupRouter(svc)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/not-uuid", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestUser_Update_InvalidJSON(t *testing.T) {
	svc := &mockUserService{}
	r := setupRouter(svc)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+uuid.New().String(), strings.NewReader("{broken"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestUser_List_WithPagination(t *testing.T) {
	svc := &mockUserService{}
	r := setupRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?limit=10&offset=5", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestUser_List_InvalidQueryParams(t *testing.T) {
	svc := &mockUserService{}
	r := setupRouter(svc)

	// Invalid limit and offset should use defaults.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?limit=abc&offset=-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
