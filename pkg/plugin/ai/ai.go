// Package ai defines the shared AIPlugin interface and common types
// shared by all AI provider implementations (OpenAI, Gemini, Ollama).
//
// This package itself has zero external dependencies — it is the interface layer.
// Import a specific provider for actual AI functionality:
//
//	import "github.com/axe-cute/axe/pkg/plugin/ai/openai"
//
// Example:
//
//	app.Use(openai.New(openai.Config{
//	    APIKey: os.Getenv("OPENAI_API_KEY"),
//	    Model:  "gpt-4o",
//	}))
//
//	svc := plugin.MustResolve[ai.Completer](app, openai.ServiceKey)
//	resp, err := svc.Chat(ctx, ai.ChatRequest{
//	    Messages: []ai.Message{
//	        {Role: "user", Content: "Hello, world!"},
//	    },
//	})
package ai

import "context"

// ── Shared interface ──────────────────────────────────────────────────────────

// Completer is the common AI interface all provider plugins expose.
// Switch providers (OpenAI → Gemini → Ollama) without changing business logic.
type Completer interface {
	// Chat sends a list of messages and returns the model's reply.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// ChatStream sends a list of messages and returns a channel of streamed chunks.
	// The channel is closed when the stream is complete or an error occurs.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
	// Embed converts text to a dense floating-point vector.
	Embed(ctx context.Context, text string) ([]float64, error)
	// Image generates an image from a text prompt and returns a URL.
	Image(ctx context.Context, prompt string) (string, error)
}

// ── Request / response types ──────────────────────────────────────────────────

// Message is a single turn in a chat conversation.
type Message struct {
	// Role is "system", "user", or "assistant".
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest describes a chat completion request.
type ChatRequest struct {
	Messages    []Message `json:"messages"`
	// Model overrides the plugin-level default model for this request.
	Model       string    `json:"model,omitempty"`
	// MaxTokens limits the response length.
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// Temperature controls randomness [0.0, 2.0]. Default: 1.0.
	Temperature float64   `json:"temperature,omitempty"`
	// Stream enables token-by-token streaming via ChatStream().
	Stream      bool      `json:"stream,omitempty"`
}

// ChatResponse is the complete response from a Chat call.
type ChatResponse struct {
	// Content is the assistant's reply text.
	Content string
	// Model is the model that produced this response.
	Model string
	// Usage contains token consumption statistics.
	Usage TokenUsage
}

// ChatChunk is a single streaming token from ChatStream().
type ChatChunk struct {
	// Delta is the incremental text for this chunk.
	Delta string
	// Done is true when this is the final chunk.
	Done  bool
	// Err carries a streaming error (final chunk only).
	Err   error
}

// TokenUsage reports token consumption for billing/monitoring.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
