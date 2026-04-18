// Package typesense provides the axe Typesense search plugin.
//
// Typesense is a fast, self-hostable, typo-tolerant search engine.
// Use it as a drop-in replacement for Elasticsearch at a fraction of the cost.
//
// Usage:
//
//	app.Use(typesense.New(typesense.Config{
//	    Host:   "localhost",
//	    Port:   8108,
//	    APIKey: os.Getenv("TYPESENSE_API_KEY"),
//	}))
//
//	svc := plugin.MustResolve[search.Searcher](app, typesense.ServiceKey)
//	err := svc.Index(ctx, "posts", "123", map[string]any{
//	    "id":    "123",
//	    "title": "Hello World",
//	    "body":  "First post content",
//	})
//
// Layer conformance:
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 5: ServiceKey constant
//   - Layer 6: uses app.Logger — no new connections beyond the dedicated HTTP client
package typesense

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/obs"
	"github.com/axe-cute/axe/pkg/plugin/search"
)

// ServiceKey is the service locator key for [search.Searcher].
const ServiceKey = "search:typesense"

// Prometheus metrics.
var (
	indexedTotal  = obs.NewCounterVec("search_typesense", "indexed_total",
		"Documents indexed.", []string{"collection"})
	deletedTotal  = obs.NewCounterVec("search_typesense", "deleted_total",
		"Documents deleted.", []string{"collection"})
	searchTotal   = obs.NewCounterVec("search_typesense", "searches_total",
		"Search queries executed.", []string{"collection"})
	searchLatency = obs.NewHistogram("search_typesense", "search_duration_seconds",
		"Typesense search query latency.")
)

// ── Config ────────────────────────────────────────────────────────────────────

// Config configures the Typesense plugin.
type Config struct {
	// Host is the Typesense server hostname. Default: "localhost".
	Host string
	// Port is the Typesense server port. Default: 8108.
	Port int
	// APIKey is the Typesense API key. Required.
	APIKey string
	// Protocol is "http" or "https". Default: "http".
	Protocol string
	// Timeout for HTTP requests. Default: 10s.
	Timeout time.Duration
	// HTTPClient allows injecting a custom HTTP client (e.g. for tests).
	// If nil, a default client is created from the other config fields.
	HTTPClient *http.Client
}

func (c *Config) defaults() {
	if c.Host == "" {
		c.Host = "localhost"
	}
	if c.Port == 0 {
		c.Port = 8108
	}
	if c.Protocol == "" {
		c.Protocol = "http"
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
}

func (c *Config) baseURL() string {
	return fmt.Sprintf("%s://%s:%d", c.Protocol, c.Host, c.Port)
}

func (c *Config) validate() error {
	if c.APIKey == "" {
		return errors.New("typesense: APIKey (TYPESENSE_API_KEY) is required")
	}
	return nil
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin is the Typesense search axe plugin.
type Plugin struct {
	cfg    Config
	client *tsClient
	log    *slog.Logger
}

// New creates a Typesense plugin with the given configuration.
// Returns an error if required fields are missing (Layer 4: fail-fast).
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	return &Plugin{
		cfg: cfg,
		client: &tsClient{
			baseURL: cfg.baseURL(),
			apiKey:  cfg.APIKey,
			http:    httpClient,
		},
	}, nil
}

// Name implements [plugin.Plugin].
func (p *Plugin) Name() string { return "search:typesense" }

// MinAxeVersion declares required axe version.
func (p *Plugin) MinAxeVersion() string { return "v1.0.0" }

// Register wires the Typesense plugin into the axe app.
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	p.log = obs.Logger(app, p.Name())

	// Layer 5: provide search.Searcher via typed service locator.
	plugin.Provide[search.Searcher](app, ServiceKey, p)

	p.log.Info("typesense search plugin registered",
		"host", p.cfg.Host,
		"port", p.cfg.Port,
	)
	return nil
}

// Shutdown is a no-op for Typesense (HTTP client has no persistent connections).
func (p *Plugin) Shutdown(_ context.Context) error { return nil }

// ── search.Searcher implementation ───────────────────────────────────────────

// Index upserts a document into a Typesense collection.
// The document map must include an "id" field as a string.
func (p *Plugin) Index(ctx context.Context, collection, id string, doc map[string]any) error {
	// Ensure id is set in the document body (Typesense requirement).
	if doc == nil {
		doc = make(map[string]any)
	}
	doc["id"] = id

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("typesense: marshal doc: %w", err)
	}

	path := fmt.Sprintf("/collections/%s/documents", collection)
	// UPSERT via action=upsert query param
	req, err := p.client.newRequest(ctx, http.MethodPost, path+"?action=upsert",
		bytes.NewReader(body))
	if err != nil {
		return err
	}

	if err := p.client.do(req, nil); err != nil {
		return fmt.Errorf("typesense: index %s/%s: %w", collection, id, err)
	}

	indexedTotal.WithLabelValues(collection).Inc()
	p.log.Debug("document indexed", "collection", collection, "id", id)
	return nil
}

// Delete removes a document from a Typesense collection.
func (p *Plugin) Delete(ctx context.Context, collection, id string) error {
	path := fmt.Sprintf("/collections/%s/documents/%s", collection, id)
	req, err := p.client.newRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if err := p.client.do(req, nil); err != nil {
		return fmt.Errorf("typesense: delete %s/%s: %w", collection, id, err)
	}
	deletedTotal.WithLabelValues(collection).Inc()
	return nil
}

// Search performs a full-text search query against a Typesense collection.
func (p *Plugin) Search(ctx context.Context, q search.Query) (*search.Results, error) {
	start := time.Now()

	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PerPage <= 0 {
		q.PerPage = 20
	}
	if q.Q == "" {
		q.Q = "*"
	}

	params := url.Values{
		"q":        {q.Q},
		"query_by": {strings.Join(q.QueryBy, ",")},
		"page":     {strconv.Itoa(q.Page)},
		"per_page": {strconv.Itoa(q.PerPage)},
	}
	if q.FilterBy != "" {
		params.Set("filter_by", q.FilterBy)
	}
	if q.SortBy != "" {
		params.Set("sort_by", q.SortBy)
	}

	path := fmt.Sprintf("/collections/%s/documents/search?%s", q.Collection, params.Encode())
	req, err := p.client.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Found int `json:"found"`
		Page  int `json:"page"`
		Hits  []struct {
			Document   map[string]any `json:"document"`
			TextMatchInfo struct {
				Score float64 `json:"best_field_score"`
			} `json:"text_match_info"`
			Highlights []struct {
				Field   string `json:"field"`
				Snippet string `json:"snippet"`
			} `json:"highlights"`
		} `json:"hits"`
	}

	if err := p.client.do(req, &raw); err != nil {
		searchLatency.Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("typesense: search %s %q: %w", q.Collection, q.Q, err)
	}

	searchLatency.Observe(time.Since(start).Seconds())
	searchTotal.WithLabelValues(q.Collection).Inc()

	hits := make([]search.Hit, len(raw.Hits))
	for i, h := range raw.Hits {
		highlights := make(map[string]string, len(h.Highlights))
		for _, hl := range h.Highlights {
			highlights[hl.Field] = hl.Snippet
		}
		id, _ := h.Document["id"].(string)
		hits[i] = search.Hit{
			ID:         id,
			Score:      h.TextMatchInfo.Score,
			Document:   h.Document,
			Highlights: highlights,
		}
	}

	return &search.Results{
		Total: raw.Found,
		Page:  raw.Page,
		Hits:  hits,
	}, nil
}

// ── Typesense HTTP client ─────────────────────────────────────────────────────

type tsClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func (c *tsClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-TYPESENSE-API-KEY", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *tsClient) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("typesense API error %d: %s", resp.StatusCode, errBody.Message)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
