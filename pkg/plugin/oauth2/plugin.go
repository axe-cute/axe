// Package oauth2 provides social login for axe via OAuth2.
//
// Supported providers: google, github (extensible via [Provider] interface).
//
// Usage:
//
//	app.Use(oauth2.New(oauth2.Config{
//	    Providers: []oauth2.ProviderConfig{
//	        {Name: "google", ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET")},
//	        {Name: "github", ClientID: os.Getenv("GITHUB_CLIENT_ID"), ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET")},
//	    },
//	    RedirectBase: "https://api.example.com",
//	    OnSuccess: func(ctx context.Context, user *oauth2.UserInfo) (*oauth2.Identity, error) {
//	        // find-or-create user in DB, return internal UUID + role
//	        return &oauth2.Identity{UserID: u.ID.String(), Role: "user", RedirectURL: "https://app.example.com/callback"}, nil
//	    },
//	}))
//
// Auto-registered routes:
//
//	GET /auth/{provider}           → redirect to provider OAuth2 page
//	GET /auth/{provider}/callback  → exchange code, issue JWT, redirect to FE
//
// Layer conformance (Story 8.10):
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 5: ServiceKey for cross-plugin resolution
//   - Layer 6: no self-created DB/Redis connections
package oauth2

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/axe-cute/axe/pkg/jwtauth"
	"github.com/axe-cute/axe/pkg/plugin"
)

// ServiceKey is the typed service locator key for [*Manager].
const ServiceKey = "oauth2"

// oauth2HTTPClient is used for all outbound HTTP calls (token exchange, user info).
// Has a 10-second timeout to prevent goroutine leaks from slow/unresponsive providers.
var oauth2HTTPClient = &http.Client{Timeout: 10 * time.Second}

// ── Provider interface ────────────────────────────────────────────────────────

// Provider abstracts an OAuth2 identity provider.
// Implement this interface to add custom providers (e.g. Apple, Microsoft).
type Provider interface {
	// Name returns the unique slug used in routes (e.g. "google", "github").
	Name() string
	// AuthURL returns the provider's authorization endpoint URL with required params.
	AuthURL(state, redirectURI string) string
	// ExchangeCode exchanges an authorization code for a user profile.
	ExchangeCode(ctx context.Context, code, redirectURI string) (*UserInfo, error)
}

// UserInfo is the normalized user profile returned by any provider.
type UserInfo struct {
	ProviderID string // provider-specific user ID
	Email      string
	Name       string
	AvatarURL  string
	Provider   string // which provider: "google", "github"
}

// ── Config ────────────────────────────────────────────────────────────────────

// ProviderConfig holds OAuth2 credentials for one provider.
type ProviderConfig struct {
	// Name identifies the provider: "google" or "github".
	Name         string
	ClientID     string
	ClientSecret string
	// Scopes overrides the default scopes for this provider.
	// Leave empty for sensible defaults (email, profile).
	Scopes []string
}

// Identity is returned by OnSuccess to map an OAuth2 provider user to the
// application's internal identity. This is the bridge between provider-specific
// IDs (Google sub, GitHub user ID) and your app's UUIDs.
type Identity struct {
	// UserID is the internal UUID from your users table.
	UserID string
	// Role is the user's role (e.g. "user", "admin").
	Role string
	// RedirectURL is where to send the browser after login.
	// The JWT access token will be appended as ?token=xxx.
	RedirectURL string
}

// Config configures the OAuth2 plugin.
type Config struct {
	Providers []ProviderConfig

	// RedirectBase is the base URL of your API server.
	// Callback URLs will be: {RedirectBase}/auth/{provider}/callback
	RedirectBase string

	// OnSuccess is REQUIRED. It maps an OAuth2 user to your app's internal identity.
	// Typically: find-or-create user in DB, return their UUID and role.
	// Return (nil, err) to reject the login.
	OnSuccess func(ctx context.Context, user *UserInfo) (*Identity, error)
}

func (c *Config) validate() error {
	var errs []string
	if len(c.Providers) == 0 {
		errs = append(errs, "at least one provider is required")
	}
	for _, p := range c.Providers {
		if p.Name == "" {
			errs = append(errs, "provider Name is required")
		}
		if p.ClientID == "" {
			errs = append(errs, fmt.Sprintf("provider %q: ClientID is required", p.Name))
		}
		if p.ClientSecret == "" {
			errs = append(errs, fmt.Sprintf("provider %q: ClientSecret is required", p.Name))
		}
		switch p.Name {
		case "google", "github":
			// known providers
		default:
			errs = append(errs, fmt.Sprintf("unknown provider %q — supported: google, github", p.Name))
		}
	}
	if c.RedirectBase == "" {
		errs = append(errs, "RedirectBase is required (e.g. https://api.example.com)")
	}
	if c.OnSuccess == nil {
		errs = append(errs, "OnSuccess is required — maps OAuth2 users to app identity")
	}
	if len(errs) > 0 {
		return errors.New("oauth2: " + strings.Join(errs, "; "))
	}
	return nil
}

// ── Manager ───────────────────────────────────────────────────────────────────

// Manager holds all registered providers and is exposed via the service locator.
type Manager struct {
	providers map[string]Provider
	cfg       Config
}

func newManager(cfg Config) (*Manager, error) {
	m := &Manager{
		providers: make(map[string]Provider),
		cfg:       cfg,
	}
	for _, pc := range cfg.Providers {
		p, err := newProvider(pc)
		if err != nil {
			return nil, err
		}
		m.providers[pc.Name] = p
	}
	return m, nil
}

func newProvider(cfg ProviderConfig) (Provider, error) {
	scopes := cfg.Scopes
	switch cfg.Name {
	case "google":
		if len(scopes) == 0 {
			scopes = []string{"openid", "email", "profile"}
		}
		return &googleProvider{clientID: cfg.ClientID, clientSecret: cfg.ClientSecret, scopes: scopes}, nil
	case "github":
		if len(scopes) == 0 {
			scopes = []string{"read:user", "user:email"}
		}
		return &githubProvider{clientID: cfg.ClientID, clientSecret: cfg.ClientSecret, scopes: scopes}, nil
	default:
		return nil, fmt.Errorf("oauth2: unknown provider %q", cfg.Name)
	}
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin implements [plugin.Plugin] for OAuth2 social login.
type Plugin struct {
	cfg     Config
	manager *Manager
	log     *slog.Logger
	jwt     *jwtauth.Service
}

// New creates an OAuth2 plugin with the given configuration.
// Returns an error if config is invalid (Layer 4: fail-fast in New).
func New(cfg Config) (*Plugin, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg}, nil
}

// Name returns the plugin identifier.
func (p *Plugin) Name() string { return "oauth2" }

// Register wires OAuth2 routes and provides the Manager via the service locator.
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	p.log = app.Logger.With("plugin", p.Name())

	// Require jwtauth.Service from the app — this is a cross-cutting concern.
	if app.JWT == nil {
		return fmt.Errorf("oauth2: app.JWT is nil — jwtauth.Service is required for token issuance")
	}
	p.jwt = app.JWT

	mgr, err := newManager(p.cfg)
	if err != nil {
		return fmt.Errorf("oauth2: init providers: %w", err)
	}
	p.manager = mgr

	// Register routes under /auth/{provider}
	app.Router.Route("/auth/{provider}", func(r chi.Router) {
		r.Get("/", p.handleLogin)
		r.Get("/callback", p.handleCallback)
	})

	// Layer 5: provide Manager via service locator.
	plugin.Provide[*Manager](app, ServiceKey, mgr)

	names := make([]string, 0, len(mgr.providers))
	for n := range mgr.providers {
		names = append(names, n)
	}
	p.log.Info("oauth2 plugin registered", "providers", names)
	return nil
}

// Shutdown is a no-op — OAuth2 has no persistent connections.
func (p *Plugin) Shutdown(_ context.Context) error { return nil }

// ── Handlers ──────────────────────────────────────────────────────────────────

// handleLogin redirects the user to the provider's OAuth2 consent page.
// Generates a random state token and stores it in a short-lived cookie for CSRF protection.
func (p *Plugin) handleLogin(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")
	provider, ok := p.manager.providers[providerName]
	if !ok {
		http.Error(w, `{"error":"unknown provider"}`, http.StatusBadRequest)
		return
	}

	state, err := generateState()
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Store state in HttpOnly cookie for CSRF protection (15 min TTL).
	// Secure flag is set when behind HTTPS (direct TLS or reverse proxy).
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth2_state",
		Value:    state,
		Path:     "/",
		MaxAge:   900,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})

	redirectURI := p.redirectURI(r, providerName)
	authURL := provider.AuthURL(state, redirectURI)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// handleCallback receives the provider redirect, verifies state, exchanges code for user info,
// then calls OnSuccess (if set) and issues a JWT.
func (p *Plugin) handleCallback(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")
	provider, ok := p.manager.providers[providerName]
	if !ok {
		http.Error(w, `{"error":"unknown provider"}`, http.StatusBadRequest)
		return
	}

	// CSRF: verify state matches cookie.
	stateCookie, err := r.Cookie("oauth2_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, `{"error":"state mismatch — possible CSRF"}`, http.StatusBadRequest)
		return
	}
	// Consume state cookie.
	http.SetCookie(w, &http.Cookie{Name: "oauth2_state", MaxAge: -1, Path: "/"})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, `{"error":"missing code parameter"}`, http.StatusBadRequest)
		return
	}

	redirectURI := p.redirectURI(r, providerName)
	userInfo, err := provider.ExchangeCode(r.Context(), code, redirectURI)
	if err != nil {
		p.log.Error("oauth2: exchange code failed", "provider", providerName, "error", err)
		http.Error(w, `{"error":"provider exchange failed"}`, http.StatusUnauthorized)
		return
	}
	userInfo.Provider = providerName

	// Call OnSuccess hook — maps OAuth2 user to app identity.
	identity, err := p.cfg.OnSuccess(r.Context(), userInfo)
	if err != nil {
		p.log.Warn("oauth2: OnSuccess rejected login", "email", userInfo.Email, "error", err)
		http.Error(w, `{"error":"login rejected"}`, http.StatusUnauthorized)
		return
	}
	if identity == nil {
		http.Error(w, `{"error":"login rejected — no identity returned"}`, http.StatusUnauthorized)
		return
	}

	// Issue JWT using the shared jwtauth.Service — same token format as the rest of the app.
	userUUID, err := uuid.Parse(identity.UserID)
	if err != nil {
		p.log.Error("oauth2: invalid UserID from OnSuccess", "user_id", identity.UserID, "error", err)
		http.Error(w, `{"error":"internal identity error"}`, http.StatusInternalServerError)
		return
	}
	pair, err := p.jwt.GenerateTokenPair(userUUID, identity.Role)
	if err != nil {
		p.log.Error("oauth2: token generation failed", "error", err)
		http.Error(w, `{"error":"token issue failed"}`, http.StatusInternalServerError)
		return
	}

	if identity.RedirectURL != "" {
		// Redirect to frontend with token as URL fragment (P0-04).
		// Using fragment (#) instead of query param (?) prevents the token
		// from being sent to the server in HTTP Referer headers or server logs.
		// Frontend must read from window.location.hash.
		u, _ := url.Parse(identity.RedirectURL)
		u.Fragment = "token=" + pair.AccessToken
		http.Redirect(w, r, u.String(), http.StatusTemporaryRedirect)
		return
	}

	// No redirect — return JSON.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"provider":      providerName,
		"email":         userInfo.Email,
		"name":          userInfo.Name,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (p *Plugin) redirectURI(r *http.Request, providerName string) string {
	base := p.cfg.RedirectBase
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/auth/" + providerName + "/callback"
}

// generateState returns a 16-byte URL-safe random string for CSRF protection.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ── Google provider ───────────────────────────────────────────────────────────

type googleProvider struct {
	clientID     string
	clientSecret string
	scopes       []string
}

func (g *googleProvider) Name() string { return "google" }

func (g *googleProvider) AuthURL(state, redirectURI string) string {
	params := url.Values{
		"client_id":     {g.clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {strings.Join(g.scopes, " ")},
		"state":         {state},
		"access_type":   {"offline"},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()
}

func (g *googleProvider) ExchangeCode(ctx context.Context, code, redirectURI string) (*UserInfo, error) {
	token, err := exchangeToken(ctx, "https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := oauth2HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &UserInfo{ProviderID: info.ID, Email: info.Email, Name: info.Name, AvatarURL: info.Picture}, nil
}

// ── GitHub provider ───────────────────────────────────────────────────────────

type githubProvider struct {
	clientID     string
	clientSecret string
	scopes       []string
}

func (g *githubProvider) Name() string { return "github" }

func (g *githubProvider) AuthURL(state, redirectURI string) string {
	params := url.Values{
		"client_id":    {g.clientID},
		"redirect_uri": {redirectURI},
		"scope":        {strings.Join(g.scopes, " ")},
		"state":        {state},
	}
	return "https://github.com/login/oauth/authorize?" + params.Encode()
}

func (g *githubProvider) ExchangeCode(ctx context.Context, code, redirectURI string) (*UserInfo, error) {
	token, err := exchangeToken(ctx, "https://github.com/login/oauth/access_token", url.Values{
		"code":          {code},
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := oauth2HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info struct {
		ID        int    `json:"id"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		Login     string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	if info.Name == "" {
		info.Name = info.Login
	}
	return &UserInfo{
		ProviderID: fmt.Sprintf("%d", info.ID),
		Email:      info.Email,
		Name:       info.Name,
		AvatarURL:  info.AvatarURL,
	}, nil
}

// ── exchangeToken shared helper ───────────────────────────────────────────────

func exchangeToken(ctx context.Context, endpoint string, params url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := oauth2HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth2: token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// P2-08: Check HTTP status before parsing — prevents interpreting error pages as valid JSON.
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("oauth2: token exchange HTTP %d: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		// GitHub returns form-encoded on some endpoints.
		vals, parseErr := url.ParseQuery(string(body))
		if parseErr != nil {
			return "", fmt.Errorf("oauth2: parse token response: %w", err)
		}
		if t := vals.Get("access_token"); t != "" {
			return t, nil
		}
	}

	if errMsg, ok := result["error"].(string); ok {
		return "", fmt.Errorf("oauth2: provider error: %s", errMsg)
	}
	if token, ok := result["access_token"].(string); ok {
		return token, nil
	}
	return "", fmt.Errorf("oauth2: no access_token in response")
}
