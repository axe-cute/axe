package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_InvalidSource(t *testing.T) {
	_, err := New(Config{Source: "cookie"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown source")
}

func TestNew_ValidSources(t *testing.T) {
	sources := []Source{SourceHeader, SourceSubdomain, SourceJWT, SourceAuto, ""}
	for _, src := range sources {
		t.Run(string(src), func(t *testing.T) {
			p, err := New(Config{Source: src})
			require.NoError(t, err)
			assert.NotNil(t, p)
		})
	}
}

// ── Extraction — Header ───────────────────────────────────────────────────────

func TestExtract_Header(t *testing.T) {
	p, _ := New(Config{Source: SourceHeader})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Tenant-ID", "acme")
	assert.Equal(t, "acme", p.extract(r))
}

func TestExtract_Header_Missing(t *testing.T) {
	p, _ := New(Config{Source: SourceHeader})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Equal(t, "", p.extract(r))
}

// ── Extraction — Subdomain ────────────────────────────────────────────────────

func TestExtract_Subdomain(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"acme.example.com", "acme"},
		{"foo.bar.example.com", "foo"},
		{"example.com", ""},     // no subdomain
		{"localhost", ""},        // no subdomain
		{"localhost:8080", ""},   // no subdomain
		{"acme.example.com:443", "acme"},
	}
	p, _ := New(Config{Source: SourceSubdomain})
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Host = tc.host
			assert.Equal(t, tc.want, p.extract(r))
		})
	}
}

// ── Extraction — Auto ─────────────────────────────────────────────────────────

func TestExtract_Auto_HeaderFirst(t *testing.T) {
	p, _ := New(Config{Source: SourceAuto})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "acme.example.com"
	r.Header.Set("X-Tenant-ID", "header-wins")
	assert.Equal(t, "header-wins", p.extract(r))
}

func TestExtract_Auto_FallsToSubdomain(t *testing.T) {
	p, _ := New(Config{Source: SourceAuto})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "acme.example.com"
	// No X-Tenant-ID header
	assert.Equal(t, "acme", p.extract(r))
}

// ── Middleware behaviour ──────────────────────────────────────────────────────

func TestMiddleware_SetsTenantInContext(t *testing.T) {
	p, _ := New(Config{Source: SourceHeader})
	mw := p.middleware()

	var gotTenant string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := FromCtx(r.Context())
		gotTenant = id
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Tenant-ID", "beta")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "beta", gotTenant)
}

func TestMiddleware_Required_Rejects401(t *testing.T) {
	p, _ := New(Config{Source: SourceHeader, Required: true})
	mw := p.middleware()

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// No X-Tenant-ID header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, called, "handler must not be called when tenant is missing and Required=true")
}

func TestMiddleware_NotRequired_PassesThrough(t *testing.T) {
	p, _ := New(Config{Source: SourceHeader, Required: false, DefaultTenant: "public"})
	mw := p.middleware()

	var gotTenant string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant, _ = FromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public", gotTenant)
}

// ── FromCtx / MustFromCtx ────────────────────────────────────────────────────

func TestFromCtx_Empty(t *testing.T) {
	ctx := context.Background()
	_, ok := FromCtx(ctx)
	assert.False(t, ok)
}

func TestFromCtx_WithTenant(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "acme")
	id, ok := FromCtx(ctx)
	assert.True(t, ok)
	assert.Equal(t, "acme", id)
}

func TestMustFromCtx_Panics(t *testing.T) {
	assert.Panics(t, func() {
		MustFromCtx(context.Background())
	})
}

// ── CacheKey ──────────────────────────────────────────────────────────────────

func TestCacheKey_WithTenant(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "acme")
	assert.Equal(t, "acme:user:42", CacheKey(ctx, "user:42"))
}

func TestCacheKey_NoTenant(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "user:42", CacheKey(ctx, "user:42"))
}

// ── Plugin registration ───────────────────────────────────────────────────────

func TestRegister_ProvidesMiddleware(t *testing.T) {
	p, err := New(Config{Source: SourceHeader})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
	// Middleware was provided to service locator.
	// Plugin also registered app.Router middleware — router itself now uses it.
}
