package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/plugin/ai"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
)

// ── Embed ────────────────────────────────────────────────────────────────────

func TestEmbed_FullResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	vec, err := p.Embed(context.Background(), "hello world")
	require.NoError(t, err)
	assert.Equal(t, []float64{0.1, 0.2, 0.3}, vec)
}

func TestEmbed_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "rate limited", "type": "rate_limit"},
		})
	}))
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	_, err := p.Embed(context.Background(), "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
}

func TestEmbed_EmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	_, err := p.Embed(context.Background(), "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no data")
}

// ── Image ────────────────────────────────────────────────────────────────────

func TestImage_FullResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"url": "https://img.openai.com/abc.png"},
			},
		})
	}))
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	url, err := p.Image(context.Background(), "a cat")
	require.NoError(t, err)
	assert.Equal(t, "https://img.openai.com/abc.png", url)
}

func TestImage_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "invalid prompt"},
		})
	}))
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	_, err := p.Image(context.Background(), "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid prompt")
}

func TestImage_EmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	_, err := p.Image(context.Background(), "anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no data")
}

// ── WriteChunksToSSE ─────────────────────────────────────────────────────────

func TestWriteChunksToSSE_Normal(t *testing.T) {
	ch := make(chan ai.ChatChunk, 3)
	ch <- ai.ChatChunk{Delta: "Hello"}
	ch <- ai.ChatChunk{Delta: " world"}
	ch <- ai.ChatChunk{Done: true}
	close(ch)

	var buf bytes.Buffer
	WriteChunksToSSE(&buf, ch)

	output := buf.String()
	assert.Contains(t, output, `data: "Hello"`)
	assert.Contains(t, output, `data: " world"`)
	assert.Contains(t, output, "data: [DONE]")
}

func TestWriteChunksToSSE_Error(t *testing.T) {
	ch := make(chan ai.ChatChunk, 1)
	ch <- ai.ChatChunk{Err: fmt.Errorf("stream failed")}
	close(ch)

	var buf bytes.Buffer
	WriteChunksToSSE(&buf, ch)
	assert.Contains(t, buf.String(), "[ERROR] stream failed")
}

func TestWriteChunksToSSE_WithFlusher(t *testing.T) {
	ch := make(chan ai.ChatChunk, 2)
	ch <- ai.ChatChunk{Delta: "Hi"}
	ch <- ai.ChatChunk{Done: true}
	close(ch)

	rec := httptest.NewRecorder()
	WriteChunksToSSE(rec, ch)
	assert.Contains(t, rec.Body.String(), `data: "Hi"`)
}

// ── Helper ───────────────────────────────────────────────────────────────────

func newTestPlugin(t *testing.T, baseURL string) *Plugin {
	t.Helper()
	p, err := New(Config{
		APIKey:     "test-key",
		BaseURL:    baseURL,
		MaxRetries: 0,
	})
	require.NoError(t, err)
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(context.Background(), app))
	return p
}
