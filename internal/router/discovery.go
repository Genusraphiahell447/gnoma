package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

const discoveryTimeout = 5 * time.Second

// DiscoveredModel represents a model found via discovery.
type DiscoveredModel struct {
	ID            string
	Name          string
	Provider      string // "ollama" or "llamacpp"
	Size          int64  // bytes, if available
	SupportsTools bool   // whether the model supports function/tool calling
	ContextSize   int    // context window in tokens (0 = unknown, use default)
}

// toolSupportedModelPrefixes lists known model families that support tool/function calling.
// This is a conservative allowlist — unknown models default to no tool support.
var toolSupportedModelPrefixes = []string{
	"mistral", "mixtral", "codestral",
	"llama3", "llama-3",
	"qwen2", "qwen-2", "qwen2.5",
	"command-r",
	"functionary",
	"hermes",
	"firefunction",
	"nexusraven",
	"groq-tool",
}

// inferToolSupport returns true if the model name suggests tool/function calling support.
func inferToolSupport(modelName string) bool {
	lower := strings.ToLower(modelName)
	for _, prefix := range toolSupportedModelPrefixes {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
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
			ID:            m.Name,
			Name:          m.Name,
			Provider:      "ollama",
			Size:          m.Size,
			SupportsTools: inferToolSupport(m.Name),
			ContextSize:   32768, // conservative default; Ollama /api/show can refine this
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
			ID:            m.ID,
			Name:          m.ID,
			Provider:      "llamacpp",
			SupportsTools: inferToolSupport(m.ID),
			ContextSize:   8192, // llama.cpp default; --ctx-size configurable
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

// StartDiscoveryLoop periodically polls for local models and reconciles with the router.
// onReconcile is called when the forced arm identity changes (may be nil).
func StartDiscoveryLoop(ctx context.Context, r *Router, logger *slog.Logger,
	ollamaURL, llamacppURL string,
	providerFactory func(name, model string) provider.Provider,
	interval time.Duration,
	onReconcile func(ArmID),
) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				models := DiscoverLocalModels(ctx, logger, ollamaURL, llamacppURL)
				reconcileArms(r, models, providerFactory, logger, onReconcile)
			}
		}
	}()
}

// reconcileArms adds newly discovered models, removes disappeared ones, and
// reconciles the forced arm when discovery reveals its real model name.
// onReconcile is called (if non-nil) when the forced arm identity changes.
func reconcileArms(r *Router, discovered []DiscoveredModel, providerFactory func(name, model string) provider.Provider, logger *slog.Logger, onReconcile func(ArmID)) {
	discoveredSet := make(map[ArmID]bool, len(discovered))
	for _, m := range discovered {
		discoveredSet[NewArmID(m.Provider, m.ID)] = true
	}

	// Reconcile forced arm if it uses a placeholder "default" model name
	// and discovery found the real model name for the same provider.
	forcedID := r.ForcedArm()
	if forcedID != "" && forcedID.Model() == "default" {
		provName := forcedID.Provider()
		var candidates []DiscoveredModel
		for _, m := range discovered {
			if m.Provider == provName {
				candidates = append(candidates, m)
			}
		}
		if len(candidates) >= 1 {
			chosen := candidates[0]
			newID := NewArmID(provName, chosen.ID)
			if len(candidates) > 1 {
				logger.Warn("multiple models discovered for forced provider, using first",
					"provider", provName, "chosen", chosen.ID, "total", len(candidates))
			}
			logger.Debug("reconciling forced arm identity", "old", forcedID, "new", newID)
			r.reconcileForcedArm(forcedID, newID, chosen.ID)
			if onReconcile != nil {
				onReconcile(newID)
			}
		}
	}

	// Register new models
	RegisterDiscoveredModels(r, discovered, providerFactory)

	// Remove arms whose models have disappeared (only local arms).
	// Never remove the forced arm — the user explicitly chose it.
	currentForced := r.ForcedArm()
	for _, arm := range r.Arms() {
		if !arm.IsLocal {
			continue
		}
		if arm.ID == currentForced {
			continue
		}
		if !discoveredSet[arm.ID] {
			logger.Debug("removing disappeared local arm", "id", arm.ID)
			r.RemoveArm(arm.ID)
		}
	}
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
				// Conservative default: don't assume tool support.
				// Many small local models (phi, tinyllama, etc.) don't support
				// function calling and will produce confused output if selected
				// for tool-requiring tasks. Larger known models (mistral, llama3,
				// qwen2.5-coder) support tools. Callers can update the arm's
				// Capabilities after probing the model template.
				ToolUse:       m.SupportsTools,
				ContextWindow: m.ContextSize,
			},
		})
	}
}
