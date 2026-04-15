//go:build integration_mysql

// Package mysqlintegration provides MySQL-specific integration tests for the axe HTTP API.
// Tests run against a real MySQL 8.x database spun up via Testcontainers.
//
// Run with: go test -tags=integration_mysql ./tests/integration/mysql/ -v -timeout 300s
// Or:       make test-integration-mysql
package mysqlintegration

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

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"github.com/axe-cute/axe/ent"
	"github.com/axe-cute/axe/internal/handler"
	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/internal/repository"
	"github.com/axe-cute/axe/internal/service"
	"github.com/axe-cute/axe/pkg/jwtauth"
)

// ── Suite ─────────────────────────────────────────────────────────────────────

var suite *testSuite

type testSuite struct {
	server    *httptest.Server
	jwtSvc    *jwtauth.Service
	container *tcmysql.MySQLContainer
}

// ── TestMain ──────────────────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	ctx := context.Background()

	// 1. Start MySQL container
	mc, err := tcmysql.Run(ctx,
		"mysql:8.0",
		tcmysql.WithDatabase("axe_test"),
		tcmysql.WithUsername("axe"),
		tcmysql.WithPassword("axe_secret"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: start mysql container: %v\n", err)
		os.Exit(1)
	}

	// 2. Build DSN from container
	rawDSN, err := mc.ConnectionString(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: get mysql connection string: %v\n", err)
		os.Exit(1)
	}
	dsn := normalizeDSN(rawDSN)

	// 3. Open DB
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: open mysql db: %v\n", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(5)

	waitForDB(db)

	// 4. Run MySQL migrations
	if err := runMigrations(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: mysql migrations: %v\n", err)
		os.Exit(1)
	}

	// 5. Wire Ent (MySQL dialect)
	drv := entsql.OpenDB(dialect.MySQL, db)
	entClient := ent.NewClient(ent.Driver(drv))

	userRepo := repository.NewUserRepo(entClient)
	userSvc := service.NewUserService(userRepo)
	jwtSvc := jwtauth.New("mysql-integration-test-secret-32b!!", 15*time.Minute, 7*24*time.Hour)

	// 6. Build router (no Redis blocklist — nil is safe)
	r := chi.NewRouter()
	r.Use(chimiddleware.Recoverer)

	authH := handler.NewAuthHandler(userSvc, jwtSvc, nil)
	userH := handler.NewUserHandler(userSvc)

	r.Mount("/api/v1/auth", authH.Routes())
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(jwtSvc, nil))
		r.Mount("/api/v1/users", userH.Routes())
	})

	// 7. Create test HTTP server
	srv := httptest.NewServer(r)

	suite = &testSuite{
		server:    srv,
		jwtSvc:    jwtSvc,
		container: mc,
	}

	// 8. Run tests
	code := m.Run()

	// 9. Teardown
	srv.Close()
	db.Close()
	entClient.Close()
	if err := mc.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: terminate mysql container: %v\n", err)
	}

	os.Exit(code)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestCreateUser verifies user creation against MySQL.
func TestCreateUser(t *testing.T) {
	token := adminToken(t)

	resp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "mysql-user@example.com",
		"name":     "MySQL User",
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
	if body["email"] != "mysql-user@example.com" {
		t.Errorf("email = %v, want mysql-user@example.com", body["email"])
	}
}

// TestCreateUser_Duplicate verifies unique email constraint on MySQL.
func TestCreateUser_Duplicate(t *testing.T) {
	token := adminToken(t)

	// Create user first time
	do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "dup@mysql.example.com",
		"name":     "Dup User",
		"password": "secret123",
	}, token)

	// Second attempt should return 409
	resp := do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "dup@mysql.example.com",
		"name":     "Dup User Again",
		"password": "secret123",
	}, token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

// TestListUsers verifies pagination query works on MySQL.
func TestListUsers(t *testing.T) {
	token := adminToken(t)

	resp := do(t, http.MethodGet, "/api/v1/users?page=1&page_size=10", nil, token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestAuthLogin verifies JWT authentication flow against MySQL.
func TestAuthLogin(t *testing.T) {
	token := adminToken(t)

	// Create a user first
	do(t, http.MethodPost, "/api/v1/users", map[string]any{
		"email":    "logintest@mysql.example.com",
		"name":     "Login Test",
		"password": "loginpass123",
	}, token)

	// Attempt login
	resp := do(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email":    "logintest@mysql.example.com",
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

// TestUnauthorized verifies JWT middleware works on MySQL-backed server.
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

func waitForDB(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for {
		if err := db.PingContext(ctx); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "FATAL: timeout waiting for mysql")
			os.Exit(1)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// normalizeDSN ensures parseTime=true and utf8mb4 are set.
// testcontainers-go/modules/mysql may return a DSN without these.
func normalizeDSN(dsn string) string {
	dsn = strings.TrimPrefix(dsn, "mysql://")
	if !strings.Contains(dsn, "parseTime=") {
		if strings.Contains(dsn, "?") {
			dsn += "&parseTime=true"
		} else {
			dsn += "?parseTime=true"
		}
	}
	if !strings.Contains(dsn, "charset=") {
		dsn += "&charset=utf8mb4"
	}
	if !strings.Contains(dsn, "loc=") {
		dsn += "&loc=UTC"
	}
	return dsn
}

// runMigrations applies all _mysql.sql migration files from db/migrations/.
func runMigrations(ctx context.Context, db *sql.DB) error {
	entries, err := os.ReadDir("../../../db/migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_mysql.sql") {
			continue
		}

		content, err := os.ReadFile("../../../db/migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		// MySQL does not support multi-statement Exec — split by ";"
		stmts := strings.Split(string(content), ";")
		for _, stmt := range stmts {
			stmt = strings.TrimSpace(stmt)
			// Skip blank lines and comment-only blocks
			if stmt == "" {
				continue
			}
			nonComment := false
			for _, line := range strings.Split(stmt, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
					nonComment = true
					break
				}
			}
			if !nonComment {
				continue
			}

			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("execute stmt in %s: %w\nSQL:\n%s", name, err, stmt)
			}
		}
		fmt.Printf("  → applied (mysql): %s\n", name)
	}
	return nil
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
