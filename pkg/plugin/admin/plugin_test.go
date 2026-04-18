package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axe-cute/axe/pkg/plugin"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


// ── Test fixtures ─────────────────────────────────────────────────────────────

// simplePlugin has no admin contribution.
type simplePlugin struct{ name string }

func (p *simplePlugin) Name() string                                    { return p.name }
func (p *simplePlugin) Register(_ context.Context, _ *plugin.App) error { return nil }
func (p *simplePlugin) Shutdown(_ context.Context) error                 { return nil }

// contributoPlugin implements Contributor.
type contributorPlugin struct {
	simplePlugin
	contribution Contribution
}

func (p *contributorPlugin) AdminContribution() Contribution { return p.contribution }

// configurablePlugin implements Configurable.
type configurablePlugin struct {
	contributorPlugin
	appliedConfig map[string]any
	applyErr      error
}

func (p *configurablePlugin) AdminConfig() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Key: "model", Label: "AI Model", Type: "select", Options: []string{"gpt-4o", "gpt-3.5"}},
		},
	}
}

func (p *configurablePlugin) ApplyConfig(_ context.Context, cfg map[string]any) error {
	if p.applyErr != nil {
		return p.applyErr
	}
	p.appliedConfig = cfg
	return nil
}

// newAdminApp creates an app pre-populated with test plugins.
func newAdminApp(t *testing.T, plugins ...plugin.Plugin) (*plugin.App, *Plugin) {
	t.Helper()
	app := plugintest.NewMockApp()
	for _, p := range plugins {
		require.NoError(t, app.Use(p))
	}
	// Add admin last.
	adm := New(Config{Path: "/axe-admin"})
	require.NoError(t, app.Use(adm))
	require.NoError(t, app.Start(t.Context()))
	return app, adm
}

// ── Plugin creation ───────────────────────────────────────────────────────────

func TestNew_DefaultPath(t *testing.T) {
	p := New(Config{})
	assert.Equal(t, "admin", p.Name())
}

func TestNew_SetsDefaultPath(t *testing.T) {
	p := New(Config{})
	assert.Equal(t, "/axe-admin", p.cfg.Path)
}

// ── Contributor discovery ─────────────────────────────────────────────────────

func TestRegister_DiscoverContributor(t *testing.T) {
	contrib := &contributorPlugin{
		simplePlugin: simplePlugin{name: "storage"},
		contribution: Contribution{ID: "storage", NavLabel: "Storage", NavIcon: "📦"},
	}
	_, adm := newAdminApp(t, contrib)

	found := false
	adm.mu.RLock()
	for _, e := range adm.entries {
		if e.Plugin.Name() == "storage" && e.Contribution != nil {
			found = true
		}
	}
	adm.mu.RUnlock()
	assert.True(t, found, "storage plugin should be discovered as contributor")
}

func TestRegister_NonContributorStillRegistered(t *testing.T) {
	simple := &simplePlugin{name: "ratelimit"}
	_, adm := newAdminApp(t, simple)

	found := false
	adm.mu.RLock()
	for _, e := range adm.entries {
		if e.Plugin.Name() == "ratelimit" {
			found = true
			assert.Nil(t, e.Contribution, "non-contributor has nil Contribution")
		}
	}
	adm.mu.RUnlock()
	assert.True(t, found)
}

// ── GET /api/plugins ──────────────────────────────────────────────────────────

func TestHandleListPlugins_ReturnsAll(t *testing.T) {
	contrib := &contributorPlugin{
		simplePlugin: simplePlugin{name: "email"},
		contribution: Contribution{ID: "email", NavLabel: "Emails"},
	}
	_, adm := newAdminApp(t, contrib)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/axe-admin/api/plugins", nil)
	adm.handleListPlugins(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var rows []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	assert.GreaterOrEqual(t, len(rows), 2, "should at least contain 'email' and 'admin' itself")
}

// ── GET /api/nav ──────────────────────────────────────────────────────────────

func TestHandleNav_OnlyVisibleContributors(t *testing.T) {
	visible := &contributorPlugin{
		simplePlugin: simplePlugin{name: "storage"},
		contribution: Contribution{ID: "storage", NavLabel: "Storage"},
	}
	hidden := &contributorPlugin{
		simplePlugin: simplePlugin{name: "jobs"},
		contribution: Contribution{ID: "jobs", NavLabel: "Jobs"},
	}
	_, adm := newAdminApp(t, visible, hidden)

	// Hide the jobs entry.
	adm.mu.Lock()
	for _, e := range adm.entries {
		if e.Plugin.Name() == "jobs" {
			e.NavVisible = false
		}
	}
	adm.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/axe-admin/api/nav", nil)
	adm.handleNav(w, r)

	var nav []Contribution
	require.NoError(t, json.NewDecoder(w.Body).Decode(&nav))

	ids := make([]string, len(nav))
	for i, c := range nav {
		ids[i] = c.ID
	}
	assert.Contains(t, ids, "storage", "visible contributor must appear in nav")
	assert.NotContains(t, ids, "jobs", "hidden contributor must not appear in nav")
}

// ── PUT /api/plugins/{id}/nav ─────────────────────────────────────────────────

func TestHandleToggleNav_HidesPlugin(t *testing.T) {
	contrib := &contributorPlugin{
		simplePlugin: simplePlugin{name: "storage"},
		contribution: Contribution{ID: "storage", NavLabel: "Storage"},
	}
	_, adm := newAdminApp(t, contrib)

	body := `{"visible": false}`
	// Set chi context for URL param
	r := httptest.NewRequest(http.MethodPut, "/axe-admin/api/plugins/storage/nav",
		strings.NewReader(body))

	// Inject chi URL param manually via chi's context.
	rctx := newChiCtx("id", "storage")
	r = r.WithContext(rctx)

	w := httptest.NewRecorder()
	adm.handleToggleNav(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify visibility was toggled.
	adm.mu.RLock()
	defer adm.mu.RUnlock()
	for _, e := range adm.entries {
		if e.Plugin.Name() == "storage" {
			assert.False(t, e.NavVisible)
		}
	}
}

func TestHandleToggleNav_UnknownPlugin(t *testing.T) {
	_, adm := newAdminApp(t)

	r := httptest.NewRequest(http.MethodPut, "/axe-admin/api/plugins/unknown/nav",
		strings.NewReader(`{"visible":true}`))
	r = r.WithContext(newChiCtx("id", "unknown"))
	w := httptest.NewRecorder()
	adm.handleToggleNav(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── GET /api/plugins/{id}/config-schema ──────────────────────────────────────

func TestHandleConfigSchema_ConfigurablePlugin(t *testing.T) {
	cp := &configurablePlugin{
		contributorPlugin: contributorPlugin{
			simplePlugin: simplePlugin{name: "ai"},
			contribution: Contribution{ID: "ai", NavLabel: "AI"},
		},
	}
	_, adm := newAdminApp(t, cp)

	r := httptest.NewRequest(http.MethodGet, "/axe-admin/api/plugins/ai/config-schema", nil)
	r = r.WithContext(newChiCtx("id", "ai"))
	w := httptest.NewRecorder()
	adm.handleConfigSchema(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var schema ConfigSchema
	require.NoError(t, json.NewDecoder(w.Body).Decode(&schema))
	require.Len(t, schema.Fields, 1)
	assert.Equal(t, "model", schema.Fields[0].Key)
}

func TestHandleConfigSchema_NonConfigurable(t *testing.T) {
	simple := &simplePlugin{name: "ratelimit"}
	_, adm := newAdminApp(t, simple)

	r := httptest.NewRequest(http.MethodGet, "/axe-admin/api/plugins/ratelimit/config-schema", nil)
	r = r.WithContext(newChiCtx("id", "ratelimit"))
	w := httptest.NewRecorder()
	adm.handleConfigSchema(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── PUT /api/plugins/{id}/config ──────────────────────────────────────────────

func TestHandleApplyConfig_Success(t *testing.T) {
	cp := &configurablePlugin{
		contributorPlugin: contributorPlugin{
			simplePlugin: simplePlugin{name: "ai"},
			contribution: Contribution{ID: "ai", NavLabel: "AI"},
		},
	}
	_, adm := newAdminApp(t, cp)

	body := `{"model":"gpt-4o"}`
	r := httptest.NewRequest(http.MethodPut, "/axe-admin/api/plugins/ai/config",
		strings.NewReader(body))
	r = r.WithContext(newChiCtx("id", "ai"))
	w := httptest.NewRecorder()
	adm.handleApplyConfig(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "gpt-4o", cp.appliedConfig["model"])
}

func TestHandleApplyConfig_PluginRejectsConfig(t *testing.T) {
	cp := &configurablePlugin{
		contributorPlugin: contributorPlugin{
			simplePlugin: simplePlugin{name: "ai"},
			contribution: Contribution{ID: "ai"},
		},
		applyErr: &ErrInvalidConfig{Field: "model", Reason: "unsupported value"},
	}
	_, adm := newAdminApp(t, cp)

	r := httptest.NewRequest(http.MethodPut, "/axe-admin/api/plugins/ai/config",
		strings.NewReader(`{"model":"bad-model"}`))
	r = r.WithContext(newChiCtx("id", "ai"))
	w := httptest.NewRecorder()
	adm.handleApplyConfig(w, r)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// ── ErrInvalidConfig ──────────────────────────────────────────────────────────

func TestErrInvalidConfig_ErrorMessage(t *testing.T) {
	err := &ErrInvalidConfig{Field: "api_key", Reason: "cannot be empty"}
	assert.Contains(t, err.Error(), "api_key")
	assert.Contains(t, err.Error(), "cannot be empty")
}

// ── ServiceKey ────────────────────────────────────────────────────────────────

func TestServiceKey_MatchesName(t *testing.T) {
	p := New(Config{})
	assert.Equal(t, p.Name(), ServiceKey)
}

// ── Shutdown ──────────────────────────────────────────────────────────────────

func TestShutdown_NoError(t *testing.T) {
	p := New(Config{})
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── chi URL param helper ──────────────────────────────────────────────────────

// newChiCtx creates a request context with a chi URL param injected using
// chi's real RouteContext, so chi.URLParam() resolves correctly in handler tests.
func newChiCtx(key, value string) context.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return context.WithValue(context.Background(), chi.RouteCtxKey, rctx)
}

