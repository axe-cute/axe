//go:build integration_sqlite

// Package sqliteintegration provides SQLite-specific integration tests for the axe HTTP API.
//
// Unlike the Postgres and MySQL suites, this suite uses an in-memory SQLite
// database — no Docker or external service is required.
//
// Run with: go test -tags=integration_sqlite ./tests/integration/sqlite/ -v -timeout 120s
// Or:       make test-integration-sqlite
package sqliteintegration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	moderncsqlite "modernc.org/sqlite" // pure-Go SQLite driver

	"github.com/axe-cute/axe/ent"
	"github.com/axe-cute/axe/internal/handler"
	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/internal/repository"
	"github.com/axe-cute/axe/internal/service"
	"github.com/axe-cute/axe/pkg/jwtauth"
)

func init() {
	// modernc.org/sqlite registers as "sqlite". Ent's sqlite dialect expects
	// the driver to be registered under "sqlite3". Re-register under that name.
	sql.Register("sqlite3", &moderncsqlite.Driver{})
}

// ── Suite ─────────────────────────────────────────────────────────────────────

var suite *testSuite

type testSuite struct {
	server *httptest.Server
	jwtSvc *jwtauth.Service
}

// ── TestMain ──────────────────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	ctx := context.Background()

	// 1. Open Ent client directly against a named in-memory SQLite database.
	//    We use:
	//    - mode=memory&cache=shared  → named in-memory DB visible to all conns
	//    - _pragma=foreign_keys(1)   → Ent validates this via "PRAGMA foreign_keys"
	//    - _pragma=journal_mode(WAL) → WAL is safer under concurrent test reads
	//    - _pragma=busy_timeout(5000) → 5 s before "database is locked"
	const dsn = "file:testdb?mode=memory&cache=shared" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)"

	entClient, err := ent.Open("sqlite3", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: open ent/sqlite: %v\n", err)
		os.Exit(1)
	}

	// 2. Auto-migrate schema (creates all tables if not present).
	if err := entClient.Schema.Create(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: ent auto-migrate: %v\n", err)
		os.Exit(1)
	}

	userRepo := repository.NewUserRepo(entClient)
	userSvc := service.NewUserService(userRepo)
	jwtSvc, _ := jwtauth.New("sqlite-integration-test-secret-32b!!", 15*time.Minute, 7*24*time.Hour)

	// 5. Build router (no Redis blocklist — nil is safe for tests)
	r := chi.NewRouter()
	r.Use(chimiddleware.Recoverer)

	authH := handler.NewAuthHandler(userSvc, jwtSvc, nil)
	userH := handler.NewUserHandler(userSvc)

	r.Mount("/api/v1/auth", authH.Routes())
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(jwtSvc, nil))
		r.Mount("/api/v1/users", userH.Routes())
	})

	// 6. Create test HTTP server
	srv := httptest.NewServer(r)

	suite = &testSuite{
		server: srv,
		jwtSvc: jwtSvc,
	}

	// 7. Run tests
	code := m.Run()

	// 8. Teardown
	srv.Close()
	entClient.Close()

	os.Exit(code)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestCreateUser verifies user creation against SQLite.
func TestCreateUser(t *testing.T) {
	token := adminToken(t)

	resp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "sqlite-user@example.com",
		"name":     "SQLite User",
		"password": "secret123",
	}, token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["email"] != "sqlite-user@example.com" {
		t.Errorf("email = %v, want sqlite-user@example.com", body["email"])
	}
}

// TestCreateUser_Duplicate verifies unique email constraint on SQLite.
func TestCreateUser_Duplicate(t *testing.T) {
	token := adminToken(t)

	// Create user first time
	do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "dup@sqlite.example.com",
		"name":     "Dup User",
		"password": "secret123",
	}, token)

	// Second attempt should return 409
	resp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "dup@sqlite.example.com",
		"name":     "Dup User Again",
		"password": "secret123",
	}, token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

// TestListUsers verifies pagination query works on SQLite.
func TestListUsers(t *testing.T) {
	token := adminToken(t)

	resp := do(t, http.MethodGet, "/api/v1/users?page=1&page_size=10", nil, token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestAuthLogin verifies JWT authentication flow against SQLite.
func TestAuthLogin(t *testing.T) {
	token := adminToken(t)

	// Create a user first
	do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "logintest@sqlite.example.com",
		"name":     "Login Test",
		"password": "loginpass123",
	}, token)

	// Attempt login
	resp := do(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email":    "logintest@sqlite.example.com",
		"password": "loginpass123",
	}, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("login status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if _, ok := body["access_token"]; !ok {
		t.Error("expected access_token in login response")
	}
}

// TestUnauthorized verifies JWT middleware works on SQLite-backed server.
func TestUnauthorized(t *testing.T) {
	resp := do(t, http.MethodGet, "/api/v1/users", nil, "") // no token
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func do(t *testing.T, method, path string, body any, token string) *http.Response {
	t.Helper()

	var payload string
	if body != nil {
		b, _ := json.Marshal(body)
		payload = string(b)
	}

	req, err := http.NewRequest(method, suite.server.URL+path, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func adminToken(t *testing.T) string {
	t.Helper()
	id := mustParseUUID("00000000-0000-0000-0000-000000000001")
	pair, err := suite.jwtSvc.GenerateTokenPair(id, "admin")
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}
	return pair.AccessToken
}

// mustParseUUID parses a UUID string, panics on error.
func mustParseUUID(s string) [16]byte {
	var id [16]byte
	if len(s) != 36 {
		panic(fmt.Sprintf("mustParseUUID: invalid length: %q", s))
	}
	hex := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	for i, b := 0, 0; b < 32; i, b = i+1, b+2 {
		hi := hexVal(hex[b])
		lo := hexVal(hex[b+1])
		if hi == 255 || lo == 255 {
			panic(fmt.Sprintf("mustParseUUID: invalid hex in %q", s))
		}
		id[i] = hi<<4 | lo
	}
	return id
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 255
}
