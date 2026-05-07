package provider

import (
	"context"
	"encoding/json"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/stream"
)

// ToolChoiceMode controls how the model selects tools.
type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceNone     ToolChoiceMode = "none"
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
	ToolChoice     ToolChoiceMode // "" = provider default (auto)
}

// ToolDefinition is the provider-agnostic tool schema.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema passthrough
}

// EffortLevel is the normalized effort/thinking level across providers.
type EffortLevel int

const (
	EffortAuto   EffortLevel = iota // no preference; provider decides
	EffortLow                       // fast / minimal reasoning
	EffortMedium                    // balanced
	EffortHigh                      // maximum reasoning
)

func (e EffortLevel) String() string {
	switch e {
	case EffortLow:
		return "low"
	case EffortMedium:
		return "medium"
	case EffortHigh:
		return "high"
	default:
		return "auto"
	}
}

// ThinkingConfig controls extended thinking / reasoning.
type ThinkingConfig struct {
	BudgetTokens int64       // explicit token budget (0 = derive from Level)
	Level        EffortLevel // normalized effort; used when BudgetTokens == 0
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
	ToolUse       bool          `json:"tool_use"`
	JSONOutput    bool          `json:"json_output"`
	ThinkingModes []EffortLevel `json:"thinking_modes,omitempty"` // nil = no thinking support
	Vision        bool          `json:"vision"`
	ContextWindow int           `json:"context_window"`
	MaxOutput     int           `json:"max_output"`
}

// SupportsThinking returns true if the model supports any extended reasoning mode.
func (c Capabilities) SupportsThinking() bool {
	return len(c.ThinkingModes) > 0
}

// SupportsEffort returns true if the model supports the given effort level.
// EffortAuto always returns true (it means "no constraint").
func (c Capabilities) SupportsEffort(level EffortLevel) bool {
	if level == EffortAuto {
		return true
	}
	for _, m := range c.ThinkingModes {
		if m == level {
			return true
		}
	}
	return false
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
