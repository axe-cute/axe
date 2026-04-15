//go:build integration

// Package integration provides end-to-end tests for the axe HTTP API.
// Tests run against a real PostgreSQL database spun up via Testcontainers.
//
// Run with: go test -tags=integration ./tests/integration/ -v -timeout 300s
// Or:       make test-integration
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/axe-cute/axe/ent"
	"github.com/axe-cute/axe/internal/handler"
	"github.com/axe-cute/axe/internal/handler/middleware"
	"github.com/axe-cute/axe/internal/repository"
	"github.com/axe-cute/axe/internal/service"
	"github.com/axe-cute/axe/pkg/jwtauth"
)

// ── Suite ─────────────────────────────────────────────────────────────────────

// suite holds all shared test state.
var suite *testSuite

type testSuite struct {
	server    *httptest.Server
	jwtSvc    *jwtauth.Service
	container *tcpostgres.PostgresContainer
}

// ── TestMain ──────────────────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	ctx := context.Background()

	// 1. Start PostgreSQL container
	pg, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("axe_test"),
		tcpostgres.WithUsername("axe"),
		tcpostgres.WithPassword("axe_secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: start postgres container: %v\n", err)
		os.Exit(1)
	}

	// 2. Resolve DSN
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: get connection string: %v\n", err)
		os.Exit(1)
	}

	// 3. Open DB
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: open db: %v\n", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(5)

	waitForDB(db)

	// 4. Run migrations
	if err := runMigrations(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: migrations: %v\n", err)
		os.Exit(1)
	}

	// 5. Wire Ent + repositories + services
	drv := entsql.OpenDB(dialect.Postgres, db)
	entClient := ent.NewClient(ent.Driver(drv))

	userRepo := repository.NewUserRepo(entClient)
	userSvc := service.NewUserService(userRepo)

	jwtSvc := jwtauth.New("integration-test-secret-32-bytes!!", 15*time.Minute, 7*24*time.Hour)

	// 6. Build router (no Redis blocklist in integration tests — nil is safe)
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
		container: pg,
	}

	// 8. Run tests
	code := m.Run()

	// 9. Teardown
	srv.Close()
	db.Close()
	entClient.Close()
	if err := pg.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: terminate postgres container: %v\n", err)
	}

	os.Exit(code)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// do sends an HTTP request to the test server and returns the response.
func do(t *testing.T, method, path string, body any, token string) *http.Response {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req, err := http.NewRequest(method, suite.server.URL+path, &buf)
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

// decodeJSON decodes response body into v.
func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// mustBeStatus asserts the response status code.
func mustBeStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Errorf("status = %d, want %d", resp.StatusCode, want)
	}
}

// waitForDB retries db.Ping until the database is ready.
func waitForDB(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		if err := db.PingContext(ctx); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "FATAL: timeout waiting for database")
			os.Exit(1)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// adminToken generates an admin JWT directly from the test JWT service.
// Use this for requests that require authentication (e.g. creating users in tests).
func adminToken(t *testing.T) string {
	t.Helper()
	pair, err := suite.jwtSvc.GenerateTokenPair(
		// Use a fixed UUID for the admin test user so tests are deterministic
		mustParseUUID("00000000-0000-0000-0000-000000000001"),
		"admin",
	)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}
	return pair.AccessToken
}

// mustParseUUID parses a UUID string, panics on error.
func mustParseUUID(s string) [16]byte {
	id, err := parseUUID(s)
	if err != nil {
		panic(fmt.Sprintf("mustParseUUID(%q): %v", s, err))
	}
	return id
}

// parseUUID is a minimal UUID parser to avoid importing uuid in this file top-level.
func parseUUID(s string) ([16]byte, error) {
	var id [16]byte
	if len(s) != 36 {
		return id, fmt.Errorf("invalid UUID length")
	}
	hex := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	for i, b := 0, 0; b < 32; i, b = i+1, b+2 {
		hi := hexVal(hex[b])
		lo := hexVal(hex[b+1])
		if hi == 255 || lo == 255 {
			return id, fmt.Errorf("invalid UUID hex")
		}
		id[i] = hi<<4 | lo
	}
	return id, nil
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



// runMigrations applies all SQL files from db/migrations/ in order.
func runMigrations(ctx context.Context, db *sql.DB) error {
	// Ensure tracking table exists
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	entries, err := os.ReadDir("../../db/migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || len(e.Name()) < 4 || e.Name()[len(e.Name())-4:] != ".sql" {
			continue
		}
		filename := e.Name()

		// Check if already applied
		var count int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE filename = $1`, filename).Scan(&count)
		if count > 0 {
			continue
		}

		content, err := os.ReadFile("../../db/migrations/" + filename)
		if err != nil {
			return fmt.Errorf("read %s: %w", filename, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", filename, err)
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, filename); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		fmt.Printf("  → applied: %s\n", filename)
	}
	return nil
}
