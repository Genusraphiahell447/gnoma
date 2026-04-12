package mistral

import (
	"context"
	"fmt"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	mistralgo "github.com/VikingOwl91/mistral-go-sdk"
	"github.com/VikingOwl91/mistral-go-sdk/model"
)

const defaultModel = "mistral-large-latest"

// Provider implements provider.Provider for the Mistral API.
type Provider struct {
	client *mistralgo.Client
	name   string
	model  string
}

// New creates a Mistral provider from config.
func New(cfg provider.ProviderConfig) (provider.Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("mistral: api key required")
	}

	opts := []mistralgo.Option{}
	if cfg.BaseURL != "" {
		opts = append(opts, mistralgo.WithBaseURL(cfg.BaseURL))
	}

	client := mistralgo.NewClient(cfg.APIKey, opts...)

	m := cfg.Model
	if m == "" {
		m = defaultModel
	}

	return &Provider{
		client: client,
		name:   "mistral",
		model:  m,
	}, nil
}

// Stream initiates a streaming chat completion request.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
	m := req.Model
	if m == "" {
		m = p.model
	}

	cr := translateRequest(req)
	cr.Model = m

	raw, err := p.client.ChatCompleteStream(ctx, cr)
	if err != nil {
		return nil, p.wrapError(err)
	}

	return newMistralStream(raw), nil
}

// Name returns "mistral".
func (p *Provider) Name() string {
	return p.name
}

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string {
	return p.model
}

// Models lists available models from the Mistral API with capability metadata.
func (p *Provider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	resp, err := p.client.ListModels(ctx, &model.ListParams{})
	if err != nil {
		return nil, p.wrapError(err)
	}

	var models []provider.ModelInfo
	for _, m := range resp.Data {
		models = append(models, provider.ModelInfo{
			ID:       m.ID,
			Name:     m.ID,
			Provider: p.name,
			Capabilities: inferCapabilities(m),
		})
	}
	return models, nil
}

// inferCapabilities maps Mistral model metadata to gnoma capabilities.
func inferCapabilities(m model.ModelCard) provider.Capabilities {
	caps := provider.Capabilities{
		ToolUse:       m.Capabilities.FunctionCalling,
		Vision:        m.Capabilities.Vision,
		JSONOutput:    m.Capabilities.CompletionChat, // all chat models support JSON output via ResponseFormat
		ContextWindow: m.MaxContextLength,
		MaxOutput:     8192, // reasonable default
	}
	return caps
}

func (p *Provider) wrapError(err error) error {
	if apiErr, ok := err.(*mistralgo.APIError); ok {
		kind, retryable := provider.ClassifyHTTPError(apiErr.StatusCode, apiErr.Message)
		return &provider.ProviderError{
			Kind:       kind,
			Provider:   p.name,
			StatusCode: apiErr.StatusCode,
			Message:    apiErr.Message,
			Retryable:  retryable,
			Err:        err,
		}
	}
	return &provider.ProviderError{
		Kind:     provider.ErrTransient,
		Provider: p.name,
		Message:  err.Error(),
		Err:      err,
	}
}
