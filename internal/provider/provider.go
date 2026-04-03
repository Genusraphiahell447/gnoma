package provider

import (
	"context"
	"encoding/json"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// Request encapsulates everything needed for a single LLM API call.
type Request struct {
	Model          string
	SystemPrompt   string
	Messages       []message.Message
	Tools          []ToolDefinition
	MaxTokens      int64
	Temperature    *float64
	TopP           *float64
	TopK           *int64
	StopSequences  []string
	Thinking       *ThinkingConfig
	ResponseFormat *ResponseFormat
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

// ResponseFormat controls the output format.
type ResponseFormat struct {
	Type       ResponseFormatType
	JSONSchema *JSONSchema // only used when Type == ResponseJSON
}

type ResponseFormatType string

const (
	ResponseText ResponseFormatType = "text"
	ResponseJSON ResponseFormatType = "json_object"
)

// JSONSchema defines a schema for structured JSON output.
type JSONSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict,omitempty"`
}

// Capabilities describes what a model can do.
type Capabilities struct {
	ToolUse       bool `json:"tool_use"`
	JSONOutput    bool `json:"json_output"`
	Thinking      bool `json:"thinking"`
	Vision        bool `json:"vision"`
	ContextWindow int  `json:"context_window"`
	MaxOutput     int  `json:"max_output"`
}

// ModelInfo describes a model available from a provider.
type ModelInfo struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Provider     string       `json:"provider"`
	Capabilities Capabilities `json:"capabilities"`
}

// SupportsTools returns true if the model supports tool/function calling.
func (m ModelInfo) SupportsTools() bool {
	return m.Capabilities.ToolUse
}

// Provider is the core abstraction over all LLM backends.
type Provider interface {
	// Stream initiates a streaming request and returns an event stream.
	Stream(ctx context.Context, req Request) (stream.Stream, error)

	// Name returns the provider identifier (e.g., "mistral", "anthropic").
	Name() string

	// Models returns available models with their capabilities.
	Models(ctx context.Context) ([]ModelInfo, error)

	// DefaultModel returns the default model ID for this provider.
	DefaultModel() string
}
