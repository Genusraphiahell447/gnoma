package provider

import (
	"fmt"
	"sync"
)

// ProviderConfig is the common configuration for any provider.
type ProviderConfig struct {
	Name       string
	APIKey     string
	BaseURL    string         // override for OpenAI-compat endpoints
	Model      string         // default model for this provider
	Options    map[string]any // provider-specific options
	MaxRetries *int           // nil = SDK default; ptr(0) = no retries
}

// Factory creates a Provider from configuration.
type Factory func(cfg ProviderConfig) (Provider, error)

// Registry maps provider names to factory functions.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
	}
}

// Register adds a provider factory. Overwrites if name already exists.
func (r *Registry) Register(name string, f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = f
}

// Create instantiates a provider by name with the given config.
func (r *Registry) Create(name string, cfg ProviderConfig) (Provider, error) {
	r.mu.RLock()
	f, ok := r.factories[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown provider: %q", name)
	}
	cfg.Name = name
	return f(cfg)
}

// Has returns true if a factory is registered for the given name.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[name]
	return ok
}

// Names returns all registered provider names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}
