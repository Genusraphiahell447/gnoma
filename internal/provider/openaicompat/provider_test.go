package openaicompat

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestNewOllama_SetsMaxRetriesToZero(t *testing.T) {
	cfg := provider.ProviderConfig{Model: "test-model"}
	_, err := NewOllama(cfg)
	if err != nil {
		t.Fatalf("NewOllama() error = %v", err)
	}
}

func TestNewLlamaCpp_SetsMaxRetriesToZero(t *testing.T) {
	cfg := provider.ProviderConfig{Model: "test-model"}
	_, err := NewLlamaCpp(cfg)
	if err != nil {
		t.Fatalf("NewLlamaCpp() error = %v", err)
	}
}

func TestNewOllama_Defaults(t *testing.T) {
	cfg := provider.ProviderConfig{}
	p, err := NewOllama(cfg)
	if err != nil {
		t.Fatalf("NewOllama() error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
	if p.DefaultModel() != "qwen3:8b" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "qwen3:8b")
	}
}

func TestNewLlamaCpp_Defaults(t *testing.T) {
	cfg := provider.ProviderConfig{}
	p, err := NewLlamaCpp(cfg)
	if err != nil {
		t.Fatalf("NewLlamaCpp() error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
	if p.DefaultModel() != "default" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "default")
	}
}
