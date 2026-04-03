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

// Models returns known Anthropic models with capabilities.
// Anthropic doesn't have a model listing API, so these are hardcoded.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{
		{
			ID: "claude-opus-4-20250514", Name: "Claude Opus 4", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Thinking: true, Vision: true,
				ContextWindow: 200000, MaxOutput: 32000,
			},
		},
		{
			ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Thinking: true, Vision: true,
				ContextWindow: 200000, MaxOutput: 16000,
			},
		},
		{
			ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Vision: true,
				ContextWindow: 200000, MaxOutput: 8192,
			},
		},
	}, nil
}
