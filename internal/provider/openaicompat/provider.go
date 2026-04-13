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
