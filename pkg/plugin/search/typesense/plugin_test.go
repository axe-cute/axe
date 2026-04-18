package typesense

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/search"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_MissingAPIKey(t *testing.T) {
	_, err := New(Config{Host: "localhost", Port: 8108})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APIKey")
}

func TestNew_ValidConfig(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	require.NoError(t, err)
	assert.Equal(t, "search:typesense", p.Name())
}

func TestNew_Defaults(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	require.NoError(t, err)
	assert.Equal(t, "localhost", p.cfg.Host)
	assert.Equal(t, 8108, p.cfg.Port)
	assert.Equal(t, "http", p.cfg.Protocol)
}

// ── Plugin lifecycle ──────────────────────────────────────────────────────────

func TestRegister_ProvidesSearcher(t *testing.T) {
	srv := newMockTypesenseServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	svc, ok := plugin.Resolve[search.Searcher](app, ServiceKey)
	require.True(t, ok, "Searcher must be resolvable via service locator")
	assert.NotNil(t, svc)
}

func TestServiceKey_IsPrefixed(t *testing.T) {
	assert.Equal(t, "search:typesense", ServiceKey)
}

func TestShutdown_NoError(t *testing.T) {
	p, _ := New(Config{APIKey: "k"})
	require.NoError(t, p.Shutdown(t.Context()))
}

func TestMinAxeVersion_NotEmpty(t *testing.T) {
	p, _ := New(Config{APIKey: "k"})
	assert.NotEmpty(t, p.MinAxeVersion())
}

// ── Index ─────────────────────────────────────────────────────────────────────

func TestIndex_Success(t *testing.T) {
	srv := newMockTypesenseServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	err := p.Index(t.Context(), "posts", "123", map[string]any{
		"title": "Hello World",
		"body":  "Content here",
	})
	require.NoError(t, err)
}

func TestIndex_SetsIDInDocument(t *testing.T) {
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&capturedBody)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	err := p.Index(t.Context(), "posts", "abc", map[string]any{"title": "Test"})
	require.NoError(t, err)

	assert.Equal(t, "abc", capturedBody["id"], "id must be injected into document body")
}

func TestIndex_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"unauthorized"}`)
	}))
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	err := p.Index(t.Context(), "posts", "1", map[string]any{"title": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "typesense")
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_Success(t *testing.T) {
	srv := newMockTypesenseServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	err := p.Delete(t.Context(), "posts", "123")
	require.NoError(t, err)
}

// ── Search ────────────────────────────────────────────────────────────────────

func TestSearch_ReturnsResults(t *testing.T) {
	srv := newMockTypesenseServer(t)
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	results, err := p.Search(t.Context(), search.Query{
		Collection: "posts",
		Q:          "hello",
		QueryBy:    []string{"title", "body"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, results.Total)
	require.Len(t, results.Hits, 1)
	assert.Equal(t, "123", results.Hits[0].ID)
	assert.Equal(t, "Hello World", results.Hits[0].Document["title"])
}

func TestSearch_DefaultsPageAndPerPage(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"found":0,"page":1,"hits":[]}`)
	}))
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	_, err := p.Search(t.Context(), search.Query{
		Collection: "posts",
		QueryBy:    []string{"title"},
		// No Q, Page, PerPage
	})
	require.NoError(t, err)
	assert.Contains(t, capturedQuery, "q=%2A",       "empty Q defaults to *")
	assert.Contains(t, capturedQuery, "page=1",       "default page is 1")
	assert.Contains(t, capturedQuery, "per_page=20",  "default per_page is 20")
}

func TestSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"message":"overloaded"}`)
	}))
	defer srv.Close()

	p := mustNewPlugin(t, srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	_, err := p.Search(t.Context(), search.Query{
		Collection: "posts", Q: "x", QueryBy: []string{"title"},
	})
	require.Error(t, err)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func mustNewPlugin(t *testing.T, baseURL string) *Plugin {
	t.Helper()
	// Inject a custom HTTP client that routes all requests to the mock server.
	p, err := New(Config{
		APIKey:     "test-key",
		HTTPClient: &http.Client{},
	})
	require.NoError(t, err)
	// Override the client's baseURL to point at the mock server.
	p.client.baseURL = baseURL
	return p
}


// newMockTypesenseServer returns an httptest.Server that mimics Typesense responses.
func newMockTypesenseServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && contains(r.URL.Path, "/documents"):
			// Index / upsert
			fmt.Fprint(w, `{"id":"123"}`)
		case r.Method == http.MethodDelete && contains(r.URL.Path, "/documents"):
			// Delete
			fmt.Fprint(w, `{"id":"123"}`)
		case r.Method == http.MethodGet && contains(r.URL.Path, "/search"):
			// Search
			fmt.Fprint(w, `{
				"found": 1,
				"page": 1,
				"hits": [{
					"document": {"id": "123", "title": "Hello World"},
					"text_match_info": {"best_field_score": 0.95},
					"highlights": [{"field": "title", "snippet": "<mark>Hello</mark> World"}]
				}]
			}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"message":"not found: %s"}`, r.URL.Path)
		}
	}))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
