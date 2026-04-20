package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/ai"
	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_MissingAPIKey(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APIKey")
}

func TestNew_ValidConfig(t *testing.T) {
	p, err := New(Config{APIKey: "sk-test"})
	require.NoError(t, err)
	assert.Equal(t, "ai:openai", p.Name())
}

func TestNew_Defaults(t *testing.T) {
	p, err := New(Config{APIKey: "x"})
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", p.cfg.Model)
	assert.Equal(t, "https://api.openai.com", p.cfg.BaseURL)
	assert.Equal(t, 60*time.Second, p.cfg.Timeout)
	assert.Equal(t, 3, p.cfg.MaxRetries)
}

// ── Plugin lifecycle ──────────────────────────────────────────────────────────

func TestRegister_ProvidesCompleter(t *testing.T) {
	p := mustNewPlugin("http://mock")
	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))

	svc, ok := plugin.Resolve[ai.Completer](app, ServiceKey)
	require.True(t, ok, "Completer must be resolvable via service locator")
	assert.NotNil(t, svc)
}

func TestMinAxeVersion(t *testing.T) {
	p, _ := New(Config{APIKey: "x"})
	assert.NotEmpty(t, p.MinAxeVersion())
}

func TestShutdown_NoError(t *testing.T) {
	p, _ := New(Config{APIKey: "x"})
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── Chat ──────────────────────────────────────────────────────────────────────

func TestChat_Success(t *testing.T) {
	srv := newMockOpenAIServer(t, http.StatusOK, map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"content": "Hello world!"}},
		},
		"model": "gpt-4o",
		"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	defer srv.Close()

	p := mustNewPlugin(srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	resp, err := p.Chat(t.Context(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Hello world!", resp.Content)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.Equal(t, 10, resp.Usage.PromptTokens)
	assert.Equal(t, 5, resp.Usage.CompletionTokens)
	assert.Equal(t, 15, resp.Usage.TotalTokens)
}

func TestChat_UsesRequestModelIfProvided(t *testing.T) {
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	p := mustNewPlugin(srv.URL) // default model is gpt-4o
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	_, err := p.Chat(t.Context(), ai.ChatRequest{
		Model:    "gpt-3.5-turbo",
		Messages: []ai.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	assert.Equal(t, "gpt-3.5-turbo", capturedBody["model"])
}

func TestChat_APIError(t *testing.T) {
	srv := newMockOpenAIServer(t, http.StatusBadRequest, map[string]any{
		"error": map[string]string{"message": "invalid request"},
	})
	defer srv.Close()

	p := mustNewPlugin(srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	_, err := p.Chat(t.Context(), ai.ChatRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request")
}

// ── Embed ─────────────────────────────────────────────────────────────────────

func TestEmbed_Success(t *testing.T) {
	srv := newMockOpenAIServer(t, http.StatusOK, map[string]any{
		"data": []map[string]any{
			{"embedding": []float64{0.1, 0.2, 0.3}},
		},
	})
	defer srv.Close()

	p := mustNewPlugin(srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	vector, err := p.Embed(t.Context(), "test text")
	require.NoError(t, err)
	assert.Equal(t, []float64{0.1, 0.2, 0.3}, vector)
}

// ── Image ─────────────────────────────────────────────────────────────────────

func TestImage_Success(t *testing.T) {
	srv := newMockOpenAIServer(t, http.StatusOK, map[string]any{
		"data": []map[string]any{
			{"url": "https://example.com/img.png"},
		},
	})
	defer srv.Close()

	p := mustNewPlugin(srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	url, err := p.Image(t.Context(), "a cute cat")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/img.png", url)
}

// ── Retry Logic ───────────────────────────────────────────────────────────────

func TestRetry_SuccessOnSecondAttempt(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests) // First attempt fails with 429
			return
		}
		w.WriteHeader(http.StatusOK) // Second attempt succeeds
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "x", BaseURL: srv.URL, MaxRetries: 3})
	require.NoError(t, err)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	// Use a short timeout so the exponential backoff (1s) doesn't block the test too long.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err = p.Chat(ctx, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "x"}}})
	require.NoError(t, err)
	assert.Equal(t, 2, attempts, "must retry exactly once")
}

func TestRetry_FailsOnMaxRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError) // 500 triggers retry
	}))
	defer srv.Close()

	// MaxRetries = 1 to keep test fast.
	p, err := New(Config{APIKey: "x", BaseURL: srv.URL, MaxRetries: 1})
	require.NoError(t, err)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err = p.Chat(ctx, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "x"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max retries exceeded")
	assert.Equal(t, 2, attempts, "initial request + 1 retry = 2 attempts")
}

// ── Streaming (ChatStream) ────────────────────────────────────────────────────

func TestChatStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Send two data chunks followed by [DONE]
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := mustNewPlugin(srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	ch, err := p.ChatStream(t.Context(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	require.NoError(t, err)

	var chunks []ai.ChatChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	require.Len(t, chunks, 3)
	assert.Equal(t, "Hello ", chunks[0].Delta)
	assert.Equal(t, "world", chunks[1].Delta)
	assert.True(t, chunks[2].Done)
	assert.NoError(t, chunks[2].Err)
}

func TestChatStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := mustNewPlugin(srv.URL)
	require.NoError(t, p.Register(t.Context(), plugintest.NewMockApp()))

	_, err := p.ChatStream(t.Context(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "hi"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// ── Admin Contribution ────────────────────────────────────────────────────────

func TestAdminContribution(t *testing.T) {
	p := mustNewPlugin("http")
	contrib := p.AdminContribution()
	assert.Equal(t, "ai:openai", contrib.ID)
	assert.Equal(t, "AI Assistant", contrib.NavLabel)
	assert.Equal(t, "🤖", contrib.NavIcon)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func mustNewPlugin(baseURL string) *Plugin {
	p, err := New(Config{APIKey: "sk-test", BaseURL: baseURL})
	if err != nil {
		panic(err)
	}
	return p
}

func newMockOpenAIServer(t *testing.T, status int, response map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(response)
	}))
}
