// Package openai provides the axe OpenAI AI plugin.
//
// It implements the [ai.Completer] interface using the OpenAI REST API
// (Chat Completions, Embeddings, Image Generation).
//
// Usage:
//
//	app.Use(openai.New(openai.Config{
//	    APIKey: os.Getenv("OPENAI_API_KEY"),
//	    Model:  "gpt-4o",
//	}))
//
//	svc := plugin.MustResolve[ai.Completer](app, openai.ServiceKey)
//	resp, err := svc.Chat(ctx, ai.ChatRequest{
//	    Messages: []ai.Message{{Role: "user", Content: "Hello!"}},
//	})
//
// Streaming:
//
//	ch, err := svc.ChatStream(ctx, ai.ChatRequest{
//	    Messages: []ai.Message{{Role: "user", Content: "Tell me a story"}},
//	    Stream:   true,
//	})
//	for chunk := range ch {
//	    if chunk.Err != nil { break }
//	    fmt.Print(chunk.Delta)
//	}
//
// Layer conformance:
//   - Layer 1: implements plugin.Plugin
//   - Layer 4: config validated in New()
//   - Layer 5: ServiceKey constant
//   - Layer 6: uses app.Logger — one HTTP client (inherent for external API)
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/ai"
	"github.com/axe-cute/axe/pkg/plugin/obs"
)

// ServiceKey is the service locator key for [ai.Completer].
const ServiceKey = "ai:openai"

// Default model used when Config.Model is not set.
const DefaultModel = "gpt-4o"

// OpenAI API base URL — overridable via Config.BaseURL for tests.
const defaultBaseURL = "https://api.openai.com"

// Prometheus metrics.
var (
	chatTotal     = obs.NewCounterVec("ai_openai", "chat_total",
		"Chat completion requests.", []string{"model", "status"})
	embedTotal    = obs.NewCounterVec("ai_openai", "embed_total",
		"Embedding requests.", []string{"status"})
	imageTotal    = obs.NewCounterVec("ai_openai", "image_total",
		"Image generation requests.", []string{"status"})
	chatLatency   = obs.NewHistogram("ai_openai", "chat_duration_seconds",
		"Chat completion latency.")
	tokenTotal    = obs.NewCounterVec("ai_openai", "tokens_total",
		"Tokens consumed.", []string{"type"}) // type: prompt|completion
)

// ── Config ────────────────────────────────────────────────────────────────────

// Config configures the OpenAI plugin.
type Config struct {
	// APIKey is the OpenAI API key. Required.
	APIKey string
	// Model is the default chat model. Default: "gpt-4o".
	Model string
	// EmbedModel is the model used for embeddings. Default: "text-embedding-3-small".
	EmbedModel string
	// ImageModel is the model used for image generation. Default: "dall-e-3".
	ImageModel string
	// BaseURL overrides the OpenAI API base URL (useful for tests or proxies).
	BaseURL string
	// Timeout for API requests. Default: 60s.
	Timeout time.Duration
	// MaxRetries is the number of retries on rate-limit (429) responses. Default: 3.
	MaxRetries int
	// HTTPClient allows injecting a custom HTTP client (for tests).
	HTTPClient *http.Client
}

func (c *Config) defaults() {
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.EmbedModel == "" {
		c.EmbedModel = "text-embedding-3-small"
	}
	if c.ImageModel == "" {
		c.ImageModel = "dall-e-3"
	}
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURL
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
}

func (c *Config) validate() error {
	if c.APIKey == "" {
		return errors.New("openai: APIKey (OPENAI_API_KEY) is required")
	}
	return nil
}

// ── Plugin ────────────────────────────────────────────────────────────────────

// Plugin is the axe OpenAI plugin.
type Plugin struct {
	cfg    Config
	client *http.Client
	log    *slog.Logger
}

// New creates an OpenAI plugin. Returns an error if required config is missing.
func New(cfg Config) (*Plugin, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	return &Plugin{cfg: cfg, client: httpClient}, nil
}

// Name implements [plugin.Plugin].
func (p *Plugin) Name() string { return "ai:openai" }

// MinAxeVersion declares required axe version.
func (p *Plugin) MinAxeVersion() string { return "v1.0.0" }

// Register provides ai.Completer via the service locator.
func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
	p.log = obs.Logger(app, p.Name())
	plugin.Provide[ai.Completer](app, ServiceKey, p)

	p.log.Info("openai plugin registered",
		"model", p.cfg.Model,
		"embed_model", p.cfg.EmbedModel,
		"image_model", p.cfg.ImageModel,
	)
	return nil
}

// Shutdown is a no-op — HTTP client has no persistent connections.
func (p *Plugin) Shutdown(_ context.Context) error { return nil }

// ── ai.Completer implementation ───────────────────────────────────────────────

// Chat sends a chat completion request to OpenAI.
func (p *Plugin) Chat(ctx context.Context, req ai.ChatRequest) (*ai.ChatResponse, error) {
	start := time.Now()
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}

	msgs := make([]map[string]string, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = map[string]string{"role": m.Role, "content": m.Content}
	}

	body := map[string]any{
		"model":    model,
		"messages": msgs,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Error *openAIError `json:"error,omitempty"`
	}

	if err := p.doWithRetry(ctx, http.MethodPost, "/v1/chat/completions", body, &raw); err != nil {
		chatTotal.WithLabelValues(model, "error").Inc()
		chatLatency.Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("openai: chat: %w", err)
	}
	if raw.Error != nil {
		chatTotal.WithLabelValues(model, "error").Inc()
		return nil, fmt.Errorf("openai: chat: %s", raw.Error.Message)
	}
	if len(raw.Choices) == 0 {
		return nil, errors.New("openai: chat: no choices in response")
	}

	chatLatency.Observe(time.Since(start).Seconds())
	chatTotal.WithLabelValues(model, "success").Inc()
	tokenTotal.WithLabelValues("prompt").Add(float64(raw.Usage.PromptTokens))
	tokenTotal.WithLabelValues("completion").Add(float64(raw.Usage.CompletionTokens))

	return &ai.ChatResponse{
		Content: raw.Choices[0].Message.Content,
		Model:   raw.Model,
		Usage: ai.TokenUsage{
			PromptTokens:     raw.Usage.PromptTokens,
			CompletionTokens: raw.Usage.CompletionTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		},
	}, nil
}

// ChatStream sends a streaming chat request and returns a channel of chunks.
// Each chunk contains a delta token. The final chunk has Done=true or Err set.
func (p *Plugin) ChatStream(ctx context.Context, req ai.ChatRequest) (<-chan ai.ChatChunk, error) {
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}

	msgs := make([]map[string]string, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = map[string]string{"role": m.Role, "content": m.Content}
	}

	body := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   true,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: chat stream: %w", err)
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("openai: chat stream: HTTP %d", resp.StatusCode)
	}

	ch := make(chan ai.ChatChunk, 16)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- ai.ChatChunk{Done: true}
				return
			}

			var event struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if len(event.Choices) > 0 {
				ch <- ai.ChatChunk{Delta: event.Choices[0].Delta.Content}
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- ai.ChatChunk{Err: fmt.Errorf("openai: stream scan: %w", err)}
		}
	}()

	return ch, nil
}

// Embed converts text to a dense vector representation.
func (p *Plugin) Embed(ctx context.Context, text string) ([]float64, error) {
	body := map[string]any{
		"model": p.cfg.EmbedModel,
		"input": text,
	}

	var raw struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Error *openAIError `json:"error,omitempty"`
	}

	if err := p.doWithRetry(ctx, http.MethodPost, "/v1/embeddings", body, &raw); err != nil {
		embedTotal.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("openai: embed: %w", err)
	}
	if raw.Error != nil {
		embedTotal.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("openai: embed: %s", raw.Error.Message)
	}
	if len(raw.Data) == 0 {
		return nil, errors.New("openai: embed: no data in response")
	}

	embedTotal.WithLabelValues("success").Inc()
	return raw.Data[0].Embedding, nil
}

// Image generates an image from a text prompt and returns the URL.
func (p *Plugin) Image(ctx context.Context, prompt string) (string, error) {
	body := map[string]any{
		"model":  p.cfg.ImageModel,
		"prompt": prompt,
		"n":      1,
		"size":   "1024x1024",
	}

	var raw struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
		Error *openAIError `json:"error,omitempty"`
	}

	if err := p.doWithRetry(ctx, http.MethodPost, "/v1/images/generations", body, &raw); err != nil {
		imageTotal.WithLabelValues("error").Inc()
		return "", fmt.Errorf("openai: image: %w", err)
	}
	if raw.Error != nil {
		imageTotal.WithLabelValues("error").Inc()
		return "", fmt.Errorf("openai: image: %s", raw.Error.Message)
	}
	if len(raw.Data) == 0 {
		return "", errors.New("openai: image: no data in response")
	}

	imageTotal.WithLabelValues("success").Inc()
	return raw.Data[0].URL, nil
}

// ── HTTP with retry + exponential backoff ─────────────────────────────────────

func (p *Plugin) doWithRetry(ctx context.Context, method, path string, body any, out any) error {
	for attempt := 0; attempt <= p.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, ...
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, method,
			p.cfg.BaseURL+path, bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

		resp, err := p.client.Do(req)
		if err != nil {
			if attempt == p.cfg.MaxRetries {
				return fmt.Errorf("http: %w", err)
			}
			continue // retry on transport error
		}

		// Retry on 429 rate limit or 5xx transient errors.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt < p.cfg.MaxRetries {
				p.log.Warn("openai: retrying request",
					"attempt", attempt+1, "status", resp.StatusCode)
				continue
			}
			return fmt.Errorf("openai: max retries exceeded (status %d)", resp.StatusCode)
		}

		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		return err
	}
	return errors.New("openai: max retries exceeded")
}

// ── Sentry error type ──────────────────────────────────────────────────────────

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"` // can be string or int
}

// AdminContribution makes OpenAI visible in the axe admin dashboard.
func (p *Plugin) AdminContribution() adminContribution {
	return adminContribution{
		ID:       "ai:openai",
		NavLabel: "AI Assistant",
		NavIcon:  "🤖",
	}
}

// adminContribution is a local copy to avoid an import cycle with the admin package.
type adminContribution struct {
	ID       string
	NavLabel string
	NavIcon  string
}

// Streaming helper for SSE — exported for use in HTTP handlers.

// WriteChunksToSSE reads from the ChatStream channel and writes SSE events
// to the ResponseWriter. Call this from a route handler:
//
//	ch, err := svc.ChatStream(ctx, req)
//	// ...
//	openai.WriteChunksToSSE(w, ch)
func WriteChunksToSSE(w io.Writer, ch <-chan ai.ChatChunk) {
	for chunk := range ch {
		if chunk.Err != nil {
			fmt.Fprintf(w, "data: [ERROR] %s\n\n", chunk.Err.Error())
			return
		}
		if chunk.Done {
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		// JSON-encode the delta to safe-encode newlines inside tokens.
		encoded, _ := json.Marshal(chunk.Delta)
		fmt.Fprintf(w, "data: %s\n\n", encoded)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}
