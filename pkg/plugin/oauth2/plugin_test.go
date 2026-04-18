package oauth2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_NoProviders(t *testing.T) {
	_, err := New(Config{
		RedirectBase: "https://api.example.com",
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one provider")
}

func TestNew_MissingClientID(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{Name: "google", ClientSecret: "secret"}},
		RedirectBase: "https://api.example.com",
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ClientID is required")
}

func TestNew_MissingClientSecret(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{Name: "google", ClientID: "id"}},
		RedirectBase: "https://api.example.com",
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ClientSecret is required")
}

func TestNew_UnknownProvider(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{Name: "twitter", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestNew_MissingRedirectBase(t *testing.T) {
	_, err := New(Config{
		Providers: []ProviderConfig{{Name: "github", ClientID: "id", ClientSecret: "s"}},
		JWTSecret: "super-secret-key-min-32-bytes-ok!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RedirectBase")
}

func TestNew_ShortJWTSecret(t *testing.T) {
	_, err := New(Config{
		Providers:    []ProviderConfig{{Name: "github", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		JWTSecret:    "short",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 characters")
}

func TestNew_ValidGoogleConfig(t *testing.T) {
	p, err := New(Config{
		Providers:    []ProviderConfig{{Name: "google", ClientID: "gid", ClientSecret: "gsecret"}},
		RedirectBase: "https://api.example.com",
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
	assert.Equal(t, "oauth2", p.Name())
}

func TestNew_ValidGithubConfig(t *testing.T) {
	p, err := New(Config{
		Providers:    []ProviderConfig{{Name: "github", ClientID: "ghid", ClientSecret: "ghsecret"}},
		RedirectBase: "https://api.example.com",
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
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
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
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

func TestGithubProvider_AuthURL(t *testing.T) {
	g := &githubProvider{clientID: "ghid", clientSecret: "s", scopes: []string{"read:user"}}
	authURL := g.AuthURL("mystate", "https://api.example.com/auth/github/callback")

	assert.Contains(t, authURL, "github.com")
	assert.Contains(t, authURL, "ghid")
	assert.Contains(t, authURL, "mystate")
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
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
	})
	assert.Equal(t, p.Name(), ServiceKey)
}

// ── Shutdown ──────────────────────────────────────────────────────────────────

func TestShutdown_NoError(t *testing.T) {
	p, _ := New(Config{
		Providers:    []ProviderConfig{{Name: "github", ClientID: "id", ClientSecret: "s"}},
		RedirectBase: "https://api.example.com",
		JWTSecret:    "super-secret-key-min-32-bytes-ok!",
	})
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Default JWTExpiry ─────────────────────────────────────────────────────────

func TestConfig_DefaultJWTExpiry(t *testing.T) {
	cfg := Config{JWTExpiry: 0}
	cfg.defaults()
	assert.Equal(t, 24*60*60, int(cfg.JWTExpiry.Seconds()))
}
