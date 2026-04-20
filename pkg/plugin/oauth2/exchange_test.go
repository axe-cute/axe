package oauth2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTransport redirects all HTTP requests to a local test server.
type testTransport struct {
	base      http.RoundTripper
	targetURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.targetURL[len("http://"):]
	return t.base.RoundTrip(req)
}

func withTestTransport(t *testing.T, srvURL string) func() {
	t.Helper()
	orig := oauth2HTTPClient
	oauth2HTTPClient = &http.Client{
		Timeout:   10 * time.Second,
		Transport: &testTransport{base: http.DefaultTransport, targetURL: srvURL},
	}
	return func() { oauth2HTTPClient = orig }
}

// ── exchangeToken ────────────────────────────────────────────────────────────

func TestExchangeToken_JSON_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok-123"})
	}))
	defer srv.Close()
	cleanup := withTestTransport(t, srv.URL)
	defer cleanup()

	token, err := exchangeToken(context.Background(), "https://oauth2.googleapis.com/token", nil)
	require.NoError(t, err)
	assert.Equal(t, "tok-123", token)
}

func TestExchangeToken_FormEncoded_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GitHub sometimes returns form-encoded responses.
		w.Write([]byte("access_token=gh-tok&token_type=bearer&scope=user"))
	}))
	defer srv.Close()
	cleanup := withTestTransport(t, srv.URL)
	defer cleanup()

	token, err := exchangeToken(context.Background(), "https://github.com/login/oauth/access_token", nil)
	require.NoError(t, err)
	assert.Equal(t, "gh-tok", token)
}

func TestExchangeToken_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	defer srv.Close()
	cleanup := withTestTransport(t, srv.URL)
	defer cleanup()

	_, err := exchangeToken(context.Background(), "https://oauth2.googleapis.com/token", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_grant")
}

func TestExchangeToken_NoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"scope": "email"})
	}))
	defer srv.Close()
	cleanup := withTestTransport(t, srv.URL)
	defer cleanup()

	_, err := exchangeToken(context.Background(), "https://oauth2.googleapis.com/token", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no access_token")
}

func TestExchangeToken_ConnectionError(t *testing.T) {
	cleanup := withTestTransport(t, "http://127.0.0.1:1")
	defer cleanup()

	_, err := exchangeToken(context.Background(), "https://oauth2.googleapis.com/token", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token exchange")
}

// ── Google ExchangeCode ──────────────────────────────────────────────────────

func TestGoogleProvider_ExchangeCode_Success(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// Token endpoint
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"access_token": "google-tok"})
		} else {
			// Userinfo endpoint
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"id": "123", "email": "user@gmail.com", "name": "Test User", "picture": "http://img.com/a.png",
			})
		}
	}))
	defer srv.Close()
	cleanup := withTestTransport(t, srv.URL)
	defer cleanup()

	g := &googleProvider{clientID: "id", clientSecret: "secret", scopes: []string{"email"}}
	info, err := g.ExchangeCode(context.Background(), "auth-code", "http://localhost/callback")
	require.NoError(t, err)
	assert.Equal(t, "123", info.ProviderID)
	assert.Equal(t, "user@gmail.com", info.Email)
	assert.Equal(t, "Test User", info.Name)
}

func TestGoogleProvider_ExchangeCode_TokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "bad_code"})
	}))
	defer srv.Close()
	cleanup := withTestTransport(t, srv.URL)
	defer cleanup()

	g := &googleProvider{clientID: "id", clientSecret: "secret"}
	_, err := g.ExchangeCode(context.Background(), "bad-code", "http://localhost/callback")
	require.Error(t, err)
}

// ── GitHub ExchangeCode ──────────────────────────────────────────────────────

func TestGithubProvider_ExchangeCode_Success(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"access_token": "gh-tok"})
		} else {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 42, "email": "user@github.com", "name": "GH User", "avatar_url": "http://img.com/b.png", "login": "ghuser",
			})
		}
	}))
	defer srv.Close()
	cleanup := withTestTransport(t, srv.URL)
	defer cleanup()

	g := &githubProvider{clientID: "id", clientSecret: "secret", scopes: []string{"user"}}
	info, err := g.ExchangeCode(context.Background(), "code", "http://localhost/callback")
	require.NoError(t, err)
	assert.Equal(t, "42", info.ProviderID)
	assert.Equal(t, "user@github.com", info.Email)
	assert.Equal(t, "GH User", info.Name)
}

func TestGithubProvider_ExchangeCode_FallbackLogin(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
		} else {
			w.Header().Set("Content-Type", "application/json")
			// Name is empty — should fall back to login.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 99, "email": "u@g.com", "name": "", "login": "loginname",
			})
		}
	}))
	defer srv.Close()
	cleanup := withTestTransport(t, srv.URL)
	defer cleanup()

	g := &githubProvider{clientID: "id", clientSecret: "secret"}
	info, err := g.ExchangeCode(context.Background(), "code", "http://localhost/cb")
	require.NoError(t, err)
	assert.Equal(t, "loginname", info.Name, "should fallback to login when name is empty")
}
