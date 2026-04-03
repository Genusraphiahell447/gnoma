package provider

import (
	"context"
	"encoding/json"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// Request encapsulates everything needed for a single LLM API call.
type Request struct {
	Model         string
	SystemPrompt  string
	Messages      []message.Message
	Tools         []ToolDefinition
	MaxTokens     int64
	Temperature   *float64
	TopP          *float64
	TopK          *int64
	StopSequences []string
	Thinking      *ThinkingConfig
}

// ToolDefinition is the provider-agnostic tool schema.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema passthrough
}

// ThinkingConfig controls extended thinking / reasoning.
type ThinkingConfig struct {
	BudgetTokens int64
}

// Provider is the core abstraction over all LLM backends.
type Provider interface {
	// Stream initiates a streaming request and returns an event stream.
	Stream(ctx context.Context, req Request) (stream.Stream, error)

	// Name returns the provider identifier (e.g., "mistral", "anthropic").
	Name() string
}
