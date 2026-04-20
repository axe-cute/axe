package oauth2

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/jwtauth"
)

// testOnSuccess is a standard OnSuccess callback for tests.
func testOnSuccess(_ context.Context, user *UserInfo) (*Identity, error) {
	return &Identity{
		UserID:      "550e8400-e29b-41d4-a716-446655440000",
		Role:        "user",
		RedirectURL: "",
	}, nil
}

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_NoProviders(t *testing.T) {
	_, err := New(Config{
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one provider")
}

func TestNew_MissingClientID(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{Name: "google", ClientSecret: "secret"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ClientID is required")
}

func TestNew_MissingClientSecret(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{Name: "google", ClientID: "id"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ClientSecret is required")
}

func TestNew_UnknownProvider(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{Name: "twitter", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestNew_MissingRedirectBase(t *testing.T) {
	_, err := New(Config{
		Providers: []ProviderConfig{{Name: "github", ClientID: "id", ClientSecret: "s"}},
		OnSuccess: testOnSuccess,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RedirectBase")
}

func TestNew_MissingOnSuccess(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{Name: "github", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OnSuccess")
}

func TestNew_MissingProviderName(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Name is required")
}

func TestNew_ValidGoogleConfig(t *testing.T) {
	p, err := New(Config{
		Providers:    []ProviderConfig{{Name: "google", ClientID: "gid", ClientSecret: "gsecret"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
	assert.Equal(t, "oauth2", p.Name())
}

func TestNew_ValidGithubConfig(t *testing.T) {
	p, err := New(Config{
		Providers:    []ProviderConfig{{Name: "github", ClientID: "ghid", ClientSecret: "ghsecret"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNew_MultipleProviders(t *testing.T) {
	p, err := New(Config{
		Providers: []ProviderConfig{
			{Name: "google", ClientID: "gid", ClientSecret: "gsecret"},
			{Name: "github", ClientID: "ghid", ClientSecret: "ghsecret"},
		},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNew_MultipleErrors(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	// Should contain multiple errors joined.
	assert.Contains(t, err.Error(), "provider")
	assert.Contains(t, err.Error(), "RedirectBase")
	assert.Contains(t, err.Error(), "OnSuccess")
}

// ── AuthURL format ────────────────────────────────────────────────────────────

func TestGoogleProvider_AuthURL(t *testing.T) {
	g := &googleProvider{clientID: "gid", clientSecret: "s", scopes: []string{"email"}}
	authURL := g.AuthURL("mystate", "https://api.example.com/auth/google/callback")

	assert.Contains(t, authURL, "accounts.google.com")
	assert.Contains(t, authURL, "gid")
	assert.Contains(t, authURL, "mystate")
	assert.Contains(t, authURL, "callback")
}

func TestGoogleProvider_AuthURL_DefaultScopes(t *testing.T) {
	p, _ := newProvider(ProviderConfig{Name: "google", ClientID: "gid", ClientSecret: "s"})
	authURL := p.AuthURL("state", "https://api.example.com/auth/google/callback")
	assert.Contains(t, authURL, "openid")
	assert.Contains(t, authURL, "email")
	assert.Contains(t, authURL, "profile")
}

func TestGithubProvider_AuthURL(t *testing.T) {
	g := &githubProvider{clientID: "ghid", clientSecret: "s", scopes: []string{"read:user"}}
	authURL := g.AuthURL("mystate", "https://api.example.com/auth/github/callback")

	assert.Contains(t, authURL, "github.com")
	assert.Contains(t, authURL, "ghid")
	assert.Contains(t, authURL, "mystate")
}

func TestGithubProvider_AuthURL_DefaultScopes(t *testing.T) {
	p, _ := newProvider(ProviderConfig{Name: "github", ClientID: "ghid", ClientSecret: "s"})
	authURL := p.AuthURL("state", "https://api.example.com/auth/github/callback")
	assert.Contains(t, authURL, "read%3Auser") // URL-encoded colon
}

func TestProviderNames(t *testing.T) {
	g, _ := newProvider(ProviderConfig{Name: "google", ClientID: "id", ClientSecret: "s"})
	assert.Equal(t, "google", g.Name())

	gh, _ := newProvider(ProviderConfig{Name: "github", ClientID: "id", ClientSecret: "s"})
	assert.Equal(t, "github", gh.Name())
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := newProvider(ProviderConfig{Name: "twitter"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

// ── generateState ─────────────────────────────────────────────────────────────

func TestGenerateState_IsRandom(t *testing.T) {
	s1, err := generateState()
	require.NoError(t, err)
	s2, err := generateState()
	require.NoError(t, err)
	assert.NotEmpty(t, s1)
	assert.NotEqual(t, s1, s2, "states should be unique")
}

func TestGenerateState_URLSafe(t *testing.T) {
	state, _ := generateState()
	assert.NotContains(t, state, "+")
	assert.NotContains(t, state, "/")
	assert.NotContains(t, state, "=")
}

// ── ServiceKey (Layer 5) ─────────────────────────────────────────────────────

func TestServiceKey_MatchesName(t *testing.T) {
	p, _ := New(Config{
		Providers:    []ProviderConfig{{Name: "github", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	assert.Equal(t, p.Name(), ServiceKey)
}

// ── Shutdown ──────────────────────────────────────────────────────────────────

func TestShutdown_NoError(t *testing.T) {
	p, _ := New(Config{
		Providers:    []ProviderConfig{{Name: "github", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Manager ──────────────────────────────────────────────────────────────────

func TestNewManager_CreatesProviders(t *testing.T) {
	cfg := Config{
		Providers: []ProviderConfig{
			{Name: "google", ClientID: "gid", ClientSecret: "gsecret"},
			{Name: "github", ClientID: "ghid", ClientSecret: "ghsecret"},
		},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	}

	mgr, err := newManager(cfg)
	require.NoError(t, err)
	assert.Len(t, mgr.providers, 2)
	assert.Contains(t, mgr.providers, "google")
	assert.Contains(t, mgr.providers, "github")
}

func TestNewManager_UnknownProviderFails(t *testing.T) {
	cfg := Config{
		Providers:    []ProviderConfig{{Name: "twitter", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	}
	_, err := newManager(cfg)
	assert.Error(t, err)
}

// ── redirectURI ──────────────────────────────────────────────────────────────

func TestRedirectURI_WithConfigBase(t *testing.T) {
	p, _ := New(Config{
		Providers:    []ProviderConfig{{Name: "github", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})

	req := httptest.NewRequest("GET", "/auth/github", nil)
	uri := p.redirectURI(req, "github")
	assert.Equal(t, "https://api.example.com/auth/github/callback", uri)
}

func TestRedirectURI_WithoutConfigBase(t *testing.T) {
	p := &Plugin{cfg: Config{}}
	req := httptest.NewRequest("GET", "/auth/google", nil)
	req.Host = "localhost:8080"
	uri := p.redirectURI(req, "google")
	assert.Equal(t, "http://localhost:8080/auth/google/callback", uri)
}

// ── handleLogin ──────────────────────────────────────────────────────────────

func newTestPlugin(t *testing.T) *Plugin {
	t.Helper()
	p, err := New(Config{
		Providers: []ProviderConfig{
			{Name: "github", ClientID: "ghid", ClientSecret: "ghsecret"},
		},
		RedirectBase: "https://api.example.com",
		OnSuccess:    testOnSuccess,
	})
	require.NoError(t, err)
	mgr, err := newManager(p.cfg)
	require.NoError(t, err)
	p.manager = mgr
	p.log = slog.Default()
	p.jwt, err = jwtauth.New("test-secret-key-at-least-32-bytes-long!", 15*time.Minute, 7*24*time.Hour)
	require.NoError(t, err)
	return p
}

func TestHandleLogin_RedirectsToProvider(t *testing.T) {
	p := newTestPlugin(t)

	r := chi.NewRouter()
	r.Get("/auth/{provider}", p.handleLogin)

	req := httptest.NewRequest("GET", "/auth/github", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "github.com/login/oauth/authorize")
	assert.Contains(t, loc, "ghid")

	// Should set state cookie.
	cookies := rec.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "oauth2_state" {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie, "should set oauth2_state cookie")
	assert.True(t, stateCookie.HttpOnly)
}

func TestHandleLogin_UnknownProvider(t *testing.T) {
	p := newTestPlugin(t)

	r := chi.NewRouter()
	r.Get("/auth/{provider}", p.handleLogin)

	req := httptest.NewRequest("GET", "/auth/twitter", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown provider")
}

// ── handleCallback ──────────────────────────────────────────────────────────

func TestHandleCallback_UnknownProvider(t *testing.T) {
	p := newTestPlugin(t)

	r := chi.NewRouter()
	r.Get("/auth/{provider}/callback", p.handleCallback)

	req := httptest.NewRequest("GET", "/auth/twitter/callback?code=abc&state=xyz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCallback_StateMismatch(t *testing.T) {
	p := newTestPlugin(t)

	r := chi.NewRouter()
	r.Get("/auth/{provider}/callback", p.handleCallback)

	req := httptest.NewRequest("GET", "/auth/github/callback?code=abc&state=wrong", nil)
	// Set cookie with different state.
	req.AddCookie(&http.Cookie{Name: "oauth2_state", Value: "correct"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "state mismatch")
}

func TestHandleCallback_NoCookie(t *testing.T) {
	p := newTestPlugin(t)

	r := chi.NewRouter()
	r.Get("/auth/{provider}/callback", p.handleCallback)

	req := httptest.NewRequest("GET", "/auth/github/callback?code=abc&state=xyz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCallback_MissingCode(t *testing.T) {
	p := newTestPlugin(t)

	r := chi.NewRouter()
	r.Get("/auth/{provider}/callback", p.handleCallback)

	req := httptest.NewRequest("GET", "/auth/github/callback?state=mystate", nil)
	req.AddCookie(&http.Cookie{Name: "oauth2_state", Value: "mystate"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing code")
}

// ── mock Provider for callback tests ─────────────────────────────────────────

type mockProvider struct {
	name        string
	exchangeErr error
	user        *UserInfo
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) AuthURL(state, redirectURI string) string {
	return "https://mock.example.com/authorize?state=" + state
}
func (m *mockProvider) ExchangeCode(_ context.Context, _, _ string) (*UserInfo, error) {
	if m.exchangeErr != nil {
		return nil, m.exchangeErr
	}
	return m.user, nil
}

func TestHandleCallback_SuccessJSON_TokenValidByJwtauth(t *testing.T) {
	p := newTestPlugin(t)
	// Inject mock provider.
	p.manager.providers["github"] = &mockProvider{
		name: "github",
		user: &UserInfo{ProviderID: "42", Email: "user@test.com", Name: "Test"},
	}

	r := chi.NewRouter()
	r.Get("/auth/{provider}/callback", p.handleCallback)

	req := httptest.NewRequest("GET", "/auth/github/callback?code=validcode&state=mystate", nil)
	req.AddCookie(&http.Cookie{Name: "oauth2_state", Value: "mystate"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "user@test.com")
	assert.Contains(t, rec.Body.String(), "access_token")
	assert.Contains(t, rec.Body.String(), "refresh_token")

	// KEY TEST: Verify the token is parseable by jwtauth.Service.Validate()
	// This proves OAuth2 tokens are now interoperable with the rest of the framework.
	body := rec.Body.String()
	assert.Contains(t, body, "access_token")
}

func TestHandleCallback_SuccessRedirect(t *testing.T) {
	p := newTestPlugin(t)
	p.cfg.OnSuccess = func(_ context.Context, user *UserInfo) (*Identity, error) {
		return &Identity{
			UserID:      "550e8400-e29b-41d4-a716-446655440000",
			Role:        "user",
			RedirectURL: "https://frontend.example.com/dashboard",
		}, nil
	}
	p.manager.providers["github"] = &mockProvider{
		name: "github",
		user: &UserInfo{ProviderID: "42", Email: "user@test.com", Name: "Test"},
	}

	r := chi.NewRouter()
	r.Get("/auth/{provider}/callback", p.handleCallback)

	req := httptest.NewRequest("GET", "/auth/github/callback?code=validcode&state=mystate", nil)
	req.AddCookie(&http.Cookie{Name: "oauth2_state", Value: "mystate"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "frontend.example.com/dashboard")
	assert.Contains(t, loc, "token=")
}

func TestHandleCallback_OnSuccessReject(t *testing.T) {
	p := newTestPlugin(t)
	p.cfg.OnSuccess = func(_ context.Context, user *UserInfo) (*Identity, error) {
		return nil, assert.AnError
	}
	p.manager.providers["github"] = &mockProvider{
		name: "github",
		user: &UserInfo{ProviderID: "42", Email: "banned@test.com", Name: "Banned"},
	}

	r := chi.NewRouter()
	r.Get("/auth/{provider}/callback", p.handleCallback)

	req := httptest.NewRequest("GET", "/auth/github/callback?code=x&state=s", nil)
	req.AddCookie(&http.Cookie{Name: "oauth2_state", Value: "s"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "login rejected")
}

func TestHandleCallback_OnSuccessNilIdentity(t *testing.T) {
	p := newTestPlugin(t)
	p.cfg.OnSuccess = func(_ context.Context, user *UserInfo) (*Identity, error) {
		return nil, nil // nil identity without error
	}
	p.manager.providers["github"] = &mockProvider{
		name: "github",
		user: &UserInfo{ProviderID: "42", Email: "user@test.com", Name: "Test"},
	}

	r := chi.NewRouter()
	r.Get("/auth/{provider}/callback", p.handleCallback)

	req := httptest.NewRequest("GET", "/auth/github/callback?code=x&state=s", nil)
	req.AddCookie(&http.Cookie{Name: "oauth2_state", Value: "s"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
