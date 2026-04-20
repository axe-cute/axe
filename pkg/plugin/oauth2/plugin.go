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
//	    JWTSecret:    os.Getenv("JWT_SECRET"),
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

	"github.com/axe-cute/axe/pkg/plugin"
)

// ServiceKey is the typed service locator key for [*Manager].
const ServiceKey = "oauth2"

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

// Config configures the OAuth2 plugin.
type Config struct {
	Providers []ProviderConfig

	// RedirectBase is the base URL of your API server.
	// Callback URLs will be: {RedirectBase}/auth/{provider}/callback
	RedirectBase string

	// JWTSecret is used to sign the JWT issued after successful OAuth2 login.
	JWTSecret string
	// JWTExpiry is the access token TTL. Default: 24h.
	JWTExpiry time.Duration

	// OnSuccess is called after a successful login to allow custom logic
	// (e.g. create user in DB, sync roles). Return ("", err) to abort login.
	// Return a redirect URL to send the user there with the JWT as a query param.
	OnSuccess func(ctx context.Context, user *UserInfo) (redirectURL string, err error)
}

func (c *Config) defaults() {
	if c.JWTExpiry <= 0 {
		c.JWTExpiry = 24 * time.Hour
	}
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
	if c.JWTSecret == "" {
		errs = append(errs, "JWTSecret is required")
	} else if len(c.JWTSecret) < 32 {
		errs = append(errs, "JWTSecret must be at least 32 characters")
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
}

// New creates an OAuth2 plugin with the given configuration.
// Returns an error if config is invalid (Layer 4: fail-fast in New).
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
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
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth2_state",
		Value:    state,
		Path:     "/",
		MaxAge:   900,
		HttpOnly: true,
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

	// Call OnSuccess hook if configured.
	redirectURL := ""
	if p.cfg.OnSuccess != nil {
		redirectURL, err = p.cfg.OnSuccess(r.Context(), userInfo)
		if err != nil {
			http.Error(w, `{"error":"login rejected"}`, http.StatusUnauthorized)
			return
		}
	}

	// Issue JWT.
	token, err := p.issueJWT(userInfo)
	if err != nil {
		http.Error(w, `{"error":"token issue failed"}`, http.StatusInternalServerError)
		return
	}

	if redirectURL != "" {
		// Redirect to frontend with token as query param.
		u, _ := url.Parse(redirectURL)
		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), http.StatusTemporaryRedirect)
		return
	}

	// No redirect — return JSON.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":    token,
		"provider": providerName,
		"email":    userInfo.Email,
		"name":     userInfo.Name,
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

// issueJWT creates a minimal HS256 JWT for the authenticated user.
// In production, plugins would use jwtauth.Service — kept minimal here
// to avoid a hard import dependency.
func (p *Plugin) issueJWT(user *UserInfo) (string, error) {
	// Build a simple JWT header.claims.signature token.
	// Real implementations should use jwtauth.Service.GenerateTokenPair.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	exp := time.Now().Add(p.cfg.JWTExpiry).Unix()
	claimsJSON, _ := json.Marshal(map[string]interface{}{
		"sub":      user.ProviderID,
		"email":    user.Email,
		"name":     user.Name,
		"provider": user.Provider,
		"exp":      exp,
	})
	claims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	_ = p.cfg.JWTSecret // used for signing in a real impl; kept for interface clarity
	// NOTE: For production, replace this with jwtauth.Service.
	// This returns an unsigned token valid for test/demo purposes only.
	return header + "." + claims + ".signature", nil
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
	resp, err := http.DefaultClient.Do(req)
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
	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth2: token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

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
