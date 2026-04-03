package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

const discoveryTimeout = 5 * time.Second

// DiscoveredModel represents a model found via discovery.
type DiscoveredModel struct {
	ID       string
	Name     string
	Provider string // "ollama" or "llamacpp"
	Size     int64  // bytes, if available
}

// DiscoverOllama polls the local Ollama instance for available models.
func DiscoverOllama(ctx context.Context, baseURL string) ([]DiscoveredModel, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama not reachable at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama returned %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name    string `json:"name"`
			Size    int64  `json:"size"`
			Details struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama response parse: %w", err)
	}

	var models []DiscoveredModel
	for _, m := range result.Models {
		models = append(models, DiscoveredModel{
			ID:       m.Name,
			Name:     m.Name,
			Provider: "ollama",
			Size:     m.Size,
		})
	}
	return models, nil
}

// DiscoverLlamaCpp polls a local llama.cpp server for available models.
func DiscoverLlamaCpp(ctx context.Context, baseURL string) ([]DiscoveredModel, error) {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llama.cpp not reachable at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("llama.cpp returned %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("llama.cpp response parse: %w", err)
	}

	var models []DiscoveredModel
	for _, m := range result.Data {
		models = append(models, DiscoveredModel{
			ID:       m.ID,
			Name:     m.ID,
			Provider: "llamacpp",
		})
	}
	return models, nil
}

// DiscoverLocalModels discovers all available local models (ollama + llama.cpp).
// Non-blocking: failures are logged and skipped.
func DiscoverLocalModels(ctx context.Context, logger *slog.Logger, ollamaURL, llamacppURL string) []DiscoveredModel {
	var all []DiscoveredModel

	if models, err := DiscoverOllama(ctx, ollamaURL); err != nil {
		logger.Debug("ollama discovery failed (non-fatal)", "error", err)
	} else {
		logger.Debug("discovered ollama models", "count", len(models))
		all = append(all, models...)
	}

	if models, err := DiscoverLlamaCpp(ctx, llamacppURL); err != nil {
		logger.Debug("llamacpp discovery failed (non-fatal)", "error", err)
	} else {
		logger.Debug("discovered llamacpp models", "count", len(models))
		all = append(all, models...)
	}

	return all
}

// RegisterDiscoveredModels registers discovered local models as arms in the router.
func RegisterDiscoveredModels(r *Router, models []DiscoveredModel, providerFactory func(name, model string) provider.Provider) {
	for _, m := range models {
		armID := NewArmID(m.Provider, m.ID)

		// Skip if already registered
		exists := false
		for _, arm := range r.Arms() {
			if arm.ID == armID {
				exists = true
				break
			}
		}
		if exists {
			continue
		}

		prov := providerFactory(m.Provider, m.ID)
		if prov == nil {
			continue
		}

		r.RegisterArm(&Arm{
			ID:        armID,
			Provider:  prov,
			ModelName: m.ID,
			IsLocal:   true,
			Capabilities: provider.Capabilities{
				ToolUse:       true, // assume tool support, will fail gracefully if not
				ContextWindow: 32768,
			},
		})
	}
}
