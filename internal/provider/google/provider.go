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

// Models returns available Google models with capabilities by querying the API.
func (p *Provider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	var models []provider.ModelInfo
	for model, err := range p.client.Models.All(ctx) {
		if err != nil {
			// Fallback to hardcoded list if API call fails
			return p.fallbackModels(), nil
		}
		
		caps := inferGoogleModelCapabilities(model)
		models = append(models, provider.ModelInfo{
			ID:           model.Name,
			Name:         model.DisplayName,
			Provider:     p.name,
			Capabilities: caps,
		})
	}

	if len(models) == 0 {
		// API returned no models, use fallback
		return p.fallbackModels(), nil
	}

	return models, nil
}

// fallbackModels returns a hardcoded list of known Google models.
func (p *Provider) fallbackModels() []provider.ModelInfo {
	return []provider.ModelInfo{
		{
			ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				Vision:        true,
				ContextWindow: 1048576,
				MaxOutput:     65536,
			},
		},
		{
			ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				Vision:        true,
				ContextWindow: 1048576,
				MaxOutput:     65536,
			},
		},
		{
			ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Vision: true,
				ContextWindow: 1048576, MaxOutput: 8192,
			},
		},
	}
}

// inferGoogleModelCapabilities infers capabilities from the Google Model.
func inferGoogleModelCapabilities(m *genai.Model) provider.Capabilities {
	// Default capabilities for most modern Gemini models
	caps := provider.Capabilities{
		ToolUse:       true,
		JSONOutput:    true,
		Vision:        true,
		ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
		ContextWindow: 1048576,
		MaxOutput:     65536,
	}

	// Model-specific overrides based on model name
	name := m.Name
	switch {
	case name == "gemini-2.5-pro", name == "gemini-2.5-flash":
		caps.ContextWindow = 1048576
		caps.MaxOutput = 65536
	case name == "gemini-2.0-pro", name == "gemini-2.0-flash":
		caps.ContextWindow = 1048576
		caps.MaxOutput = 8192
	case name == "gemini-1.5-pro", name == "gemini-1.5-flash":
		caps.ContextWindow = 1048576
		caps.MaxOutput = 8192
	}

	return caps
}
