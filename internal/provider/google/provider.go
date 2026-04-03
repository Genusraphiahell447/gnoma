package google

import (
	"context"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"

	"google.golang.org/genai"
)

const defaultModel = "gemini-2.5-flash"

// Provider implements provider.Provider for Google's Gemini API.
type Provider struct {
	client *genai.Client
	name   string
	model  string
}

// New creates a Google GenAI provider from config.
func New(cfg provider.ProviderConfig) (provider.Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("google: api key required")
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("google: create client: %w", err)
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	return &Provider{
		client: client,
		name:   "google",
		model:  model,
	}, nil
}

// Stream initiates a streaming content generation request.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	contents := translateContents(req.Messages)
	config := translateConfig(req)

	iter := p.client.Models.GenerateContentStream(ctx, model, contents, config)

	return newGoogleStream(ctx, iter, model), nil
}

// Name returns "google".
func (p *Provider) Name() string { return p.name }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.model }

// Models returns known Google models with capabilities.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{
		{
			ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Thinking: true, Vision: true,
				ContextWindow: 1048576, MaxOutput: 65536,
			},
		},
		{
			ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Thinking: true, Vision: true,
				ContextWindow: 1048576, MaxOutput: 65536,
			},
		},
		{
			ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Vision: true,
				ContextWindow: 1048576, MaxOutput: 8192,
			},
		},
	}, nil
}
