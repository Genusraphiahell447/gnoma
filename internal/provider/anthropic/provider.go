package anthropic

import (
	"context"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultModel = "claude-sonnet-4-20250514"

// Provider implements provider.Provider for the Anthropic API.
type Provider struct {
	client *anthropic.Client
	name   string
	model  string
}

// New creates an Anthropic provider from config.
func New(cfg provider.ProviderConfig) (provider.Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropic: api key required")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	client := anthropic.NewClient(opts...)

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	return &Provider{
		client: &client,
		name:   "anthropic",
		model:  model,
	}, nil
}

// Stream initiates a streaming message request.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	params := translateRequest(req)
	params.Model = anthropic.Model(model)

	if params.MaxTokens == 0 {
		params.MaxTokens = 8192
	}

	raw := p.client.Messages.NewStreaming(ctx, params)

	return newAnthropicStream(raw), nil
}

// Name returns "anthropic".
func (p *Provider) Name() string {
	return p.name
}

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string {
	return p.model
}

// Models returns available Anthropic models with capabilities by querying the API.
func (p *Provider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	pager := p.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	
	var models []provider.ModelInfo
	for pager.Next() {
		m := pager.Current()
		caps := inferAnthropicModelCapabilities(m.ID)
		models = append(models, provider.ModelInfo{
			ID:           m.ID,
			Name:         m.ID,
			Provider:     p.name,
			Capabilities: caps,
		})
	}
	if err := pager.Err(); err != nil {
		// Fallback to hardcoded list if API call fails
		return p.fallbackModels(), nil
	}

	if len(models) == 0 {
		// API returned no models, use fallback
		return p.fallbackModels(), nil
	}

	return models, nil
}

// fallbackModels returns a hardcoded list of known Anthropic models.
func (p *Provider) fallbackModels() []provider.ModelInfo {
	return []provider.ModelInfo{
		{
			ID: "claude-opus-4-20250514", Name: "Claude Opus 4", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				Vision:        true,
				ContextWindow: 200000,
				MaxOutput:     32000,
			},
		},
		{
			ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				Vision:        true,
				ContextWindow: 200000,
				MaxOutput:     16000,
			},
		},
		{
			ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Vision: true,
				ContextWindow: 200000, MaxOutput: 8192,
			},
		},
	}
}

// inferAnthropicModelCapabilities infers capabilities from model ID.
func inferAnthropicModelCapabilities(modelID string) provider.Capabilities {
	// Default capabilities for most modern Claude models
	caps := provider.Capabilities{
		ToolUse:       true,
		JSONOutput:    true,
		Vision:        true,
		ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
		ContextWindow: 200000,
		MaxOutput:     16000,
	}

	// Model-specific overrides
	switch modelID {
	case "claude-opus-4-20250514", "claude-opus-4-20250612":
		caps.MaxOutput = 32000
	case "claude-3-opus-20240229", "claude-3-sonnet-20240229":
		caps.ThinkingModes = []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh}
		caps.ContextWindow = 200000
		caps.MaxOutput = 4096
	case "claude-3-haiku-20240307":
		caps.ThinkingModes = []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh}
		caps.ContextWindow = 200000
		caps.MaxOutput = 4096
	case "claude-2", "claude-2:1", "claude-instant-1":
		caps.ThinkingModes = nil // No extended thinking support
		caps.ContextWindow = 100000
		caps.MaxOutput = 4096
	}

	return caps
}
