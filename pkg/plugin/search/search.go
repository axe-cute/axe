// Package search defines the shared Searcher interface and common types
// shared by all search provider implementations (Typesense, Elasticsearch, etc.).
//
// This package itself has zero external dependencies — it is the interface layer.
// Import a specific provider for actual search functionality:
//
//	import "github.com/axe-cute/axe/pkg/plugin/search/typesense"
//
// Example:
//
//	app.Use(typesense.New(typesense.Config{
//	    Host:       "localhost",
//	    Port:       8108,
//	    APIKey:     os.Getenv("TYPESENSE_API_KEY"),
//	    Collection: "posts",
//	}))
//
//	svc := plugin.MustResolve[search.Searcher](app, typesense.ServiceKey)
//	results, err := svc.Search(ctx, search.Query{
//	    Collection: "posts",
//	    Q:          "hello world",
//	    QueryBy:    []string{"title", "body"},
//	    Page:       1,
//	    PerPage:    20,
//	})
package search

import "context"

// ── Shared interface ──────────────────────────────────────────────────────────

// Searcher is the common interface all search provider plugins expose
// via the service locator. Switch providers without changing business logic.
type Searcher interface {
	// Index upserts a document into a collection.
	// id is the unique document identifier. doc is the document payload.
	Index(ctx context.Context, collection, id string, doc map[string]any) error

	// Delete removes a document from a collection.
	Delete(ctx context.Context, collection, id string) error

	// Search performs a full-text search query.
	Search(ctx context.Context, q Query) (*Results, error)
}

// ── Query / response types ────────────────────────────────────────────────────

// Query describes a full-text search request.
type Query struct {
	// Collection is the target search collection (a.k.a. index).
	Collection string
	// Q is the search query string. Use "*" to match all documents.
	Q string
	// QueryBy lists the fields to search in. Required.
	QueryBy []string
	// FilterBy is an optional filter expression (provider syntax).
	FilterBy string
	// SortBy is an optional sort expression (e.g. "created_at:desc").
	SortBy string
	// Page is 1-indexed; defaults to 1 if zero.
	Page int
	// PerPage is the number of results per page; defaults to 20 if zero.
	PerPage int
}

// Results holds the results of a Search call.
type Results struct {
	// Total is the total number of matching documents.
	Total int
	// Page is the current page (1-indexed).
	Page int
	// Hits is the list of matched documents.
	Hits []Hit
}

// Hit is a single matched document.
type Hit struct {
	// ID is the document identifier.
	ID string
	// Score is the relevance score (higher = more relevant).
	Score float64
	// Document is the raw document payload.
	Document map[string]any
	// Highlights maps field names to highlighted snippets.
	Highlights map[string]string
}
