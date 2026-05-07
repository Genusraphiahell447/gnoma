// Package openaicompat provides OpenAI-compatible provider adapters
// for Ollama, llama.cpp, and other servers that implement the OpenAI API.
package openaicompat

import (
	"somegit.dev/Owlibou/gnoma/internal/provider"
	oaiprov "somegit.dev/Owlibou/gnoma/internal/provider/openai"
)

const (
	ollamaDefaultURL   = "http://localhost:11434/v1"
	llamacppDefaultURL = "http://localhost:8080/v1"
)

func intPtr(v int) *int { return &v }

// NewOllama creates a provider for a local Ollama instance.
func NewOllama(cfg provider.ProviderConfig) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = ollamaDefaultURL
	}
	if cfg.APIKey == "" {
		cfg.APIKey = "ollama" // Ollama doesn't require a real key
	}
	if cfg.Model == "" {
		cfg.Model = "qwen3:8b"
	}
	if cfg.MaxRetries == nil {
		cfg.MaxRetries = intPtr(0) // local 500s are deterministic, not transient
	}
	return oaiprov.New(cfg)
}

// NewLlamaCpp creates a provider for a local llama.cpp server.
func NewLlamaCpp(cfg provider.ProviderConfig) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = llamacppDefaultURL
	}
	if cfg.APIKey == "" {
		cfg.APIKey = "llamacpp" // llama.cpp doesn't require a real key
	}
	if cfg.Model == "" {
		cfg.Model = "default"
	}
	if cfg.MaxRetries == nil {
		cfg.MaxRetries = intPtr(0) // local 500s are deterministic, not transient
	}
	return oaiprov.New(cfg)
}

// NewLlamafile creates a provider for a llamafile process.
// BaseURL must include /v1, e.g. "http://127.0.0.1:8080/v1".
func NewLlamafile(cfg provider.ProviderConfig) (provider.Provider, error) {
	if cfg.APIKey == "" {
		cfg.APIKey = "llamafile" // llamafile doesn't require a real key
	}
	if cfg.Model == "" {
		cfg.Model = "default" // llamafile ignores the model field
	}
	if cfg.MaxRetries == nil {
		cfg.MaxRetries = intPtr(0)
	}
	return oaiprov.New(cfg)
}
