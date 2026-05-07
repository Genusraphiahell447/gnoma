package openai

import (
	"context"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const defaultModel = "gpt-4o"

// Provider implements provider.Provider for the OpenAI API.
type Provider struct {
	client     *oai.Client
	name       string
	model      string
	streamOpts []option.RequestOption // injected per-request (e.g. think:false for Ollama)
}

// New creates an OpenAI provider from config.
func New(cfg provider.ProviderConfig) (provider.Provider, error) {
	return NewWithStreamOptions(cfg, nil)
}

// NewWithStreamOptions creates an OpenAI provider with extra per-request stream options.
// Use this for Ollama/llama.cpp adapters that need non-standard body fields.
func NewWithStreamOptions(cfg provider.ProviderConfig, streamOpts []option.RequestOption) (provider.Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai: api key required")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.MaxRetries != nil {
		opts = append(opts, option.WithMaxRetries(*cfg.MaxRetries))
	}

	client := oai.NewClient(opts...)

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	return &Provider{
		client:     &client,
		name:       "openai",
		model:      model,
		streamOpts: streamOpts,
	}, nil
}

// Stream initiates a streaming chat completion request.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	params := translateRequest(req)
	params.Model = model

	raw := p.client.Chat.Completions.NewStreaming(ctx, params, p.streamOpts...)

	return newOpenAIStream(raw), nil
}

// Name returns "openai".
func (p *Provider) Name() string { return p.name }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.model }

// Models returns available OpenAI models with capabilities by querying the API.
func (p *Provider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	pager := p.client.Models.ListAutoPaging(ctx)
	
	var models []provider.ModelInfo
	for pager.Next() {
		m := pager.Current()
		caps := inferOpenAIModelCapabilities(m.ID)
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

// fallbackModels returns a hardcoded list of known OpenAI models.
func (p *Provider) fallbackModels() []provider.ModelInfo {
	return []provider.ModelInfo{
		{
			ID: "gpt-4o", Name: "GPT-4o", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Vision: true,
				ContextWindow: 128000, MaxOutput: 16384,
			},
		},
		{
			ID: "gpt-4o-mini", Name: "GPT-4o Mini", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Vision: true,
				ContextWindow: 128000, MaxOutput: 16384,
			},
		},
		{
			ID: "o3", Name: "o3", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				ContextWindow: 200000,
				MaxOutput:     100000,
			},
		},
		{
			ID: "o3-mini", Name: "o3 Mini", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				ContextWindow: 200000,
				MaxOutput:     100000,
			},
		},
	}
}

// inferOpenAIModelCapabilities infers capabilities from model ID.
func inferOpenAIModelCapabilities(modelID string) provider.Capabilities {
	// Default capabilities for most modern OpenAI models
	caps := provider.Capabilities{
		ToolUse:       true,
		JSONOutput:    true,
		Vision:        true,
		ContextWindow: 128000,
		MaxOutput:     16384,
	}

	// Model-specific overrides
	switch modelID {
	case "gpt-4o", "gpt-4o-mini":
		caps.ContextWindow = 128000
		caps.MaxOutput = 16384
	case "o3", "o3-mini":
		caps.ThinkingModes = []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh}
		caps.ContextWindow = 200000
		caps.MaxOutput = 100000
	case "gpt-4", "gpt-4-0613", "gpt-4-32k", "gpt-4-32k-0613":
		caps.Vision = false
		caps.ContextWindow = 8192
		caps.MaxOutput = 8192
	case "gpt-3.5-turbo", "gpt-3.5-turbo-0613", "gpt-3.5-turbo-16k", "gpt-3.5-turbo-16k-0613":
		caps.Vision = false
		caps.ToolUse = false
		caps.ContextWindow = 16384
		caps.MaxOutput = 4096
	}

	return caps
}
