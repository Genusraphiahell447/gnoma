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

// Per-provider context-window fallbacks applied when a probe doesn't report a
// concrete num_ctx / n_ctx. The router treats ContextWindow == 0 as "tiny"
// (forcing two-stage tool routing), so leaving 0 in for unprobed models would
// corrupt routing decisions for every local arm.
const (
	defaultOllamaContextSize   = 32768
	defaultLlamaCppContextSize = 8192
)

// DiscoveredModel represents a model found via discovery.
type DiscoveredModel struct {
	ID            string
	Name          string
	Provider      string // "ollama" or "llamacpp"
	Size          int64  // bytes, if available
	SupportsTools bool   // whether the model supports function/tool calling
	ContextSize   int    // context window in tokens (always populated; provider-specific default if probe was inconclusive)
}

// DiscoverOllama polls the local Ollama instance for available models.
// toolCache caches /api/show probe results per model name to avoid N requests
// per discovery cycle. Pass nil to probe every model unconditionally.
// The caller owns the cache and should pass the same map across cycles.
func DiscoverOllama(ctx context.Context, baseURL string, toolCache map[string]bool) ([]DiscoveredModel, error) {
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var data struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	discovered := make([]DiscoveredModel, 0, len(data.Models))
	currentModels := make(map[string]bool, len(data.Models))
	for _, m := range data.Models {
		currentModels[m.Name] = true
		dm := DiscoveredModel{
			ID:       m.Name,
			Name:     m.Name,
			Provider: "ollama",
			Size:     m.Size,
		}

		// Try to probe capabilities if we have a cache or if we want to probe
		if toolCache != nil {
			if supported, ok := toolCache[m.Name]; ok {
				dm.SupportsTools = supported
			} else {
				// Probe once
				supported, contextSize := probeOllamaModel(ctx, baseURL, m.Name)
				toolCache[m.Name] = supported
				dm.SupportsTools = supported
				dm.ContextSize = contextSize
			}
		}

		if dm.ContextSize == 0 {
			dm.ContextSize = defaultOllamaContextSize
		}

		discovered = append(discovered, dm)
	}

	// Prune cache entries for models that have disappeared since the last
	// poll. Without this, the cache grows unbounded and stale entries linger
	// (a reappearing model would replay an out-of-date tool-support verdict).
	for name := range toolCache {
		if !currentModels[name] {
			delete(toolCache, name)
		}
	}
	return discovered, nil
}

func probeOllamaModel(ctx context.Context, baseURL, model string) (bool, int) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/show", strings.NewReader(fmt.Sprintf(`{"name":"%s"}`, model)))
	if err != nil {
		return false, 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return false, 0
	}
	var data struct {
		Template   string `json:"template"`
		Parameters string `json:"parameters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return false, 0
	}

	// Heuristic for tool support: many modern models that support tools
	// have "call" or "tool" or "json" in their template or system prompt
	// logic. More specifically, Ollama's own tool-calling models often
	// include specific jinja templates.
	supported := strings.Contains(data.Template, ".Tool") ||
		strings.Contains(data.Template, "tools") ||
		strings.Contains(data.Template, "json")

	// Context size heuristic from parameters
	contextSize := 0
	if strings.Contains(data.Parameters, "num_ctx") {
		// Ollama parameters are often a block of text: "num_ctx 4096\nstop <|end|>"
		lines := strings.Split(data.Parameters, "\n")
		for _, l := range lines {
			if strings.HasPrefix(l, "num_ctx") {
				fmt.Sscanf(l, "num_ctx %d", &contextSize)
				break
			}
		}
	}

	return supported, contextSize
}

// DiscoverLlamaCPP enumerates models served by a llama.cpp server.
//
// llama-server exposes /v1/models (OpenAI-compatible) — single-model
// deployments return one entry with the actual model ID; multi-model proxies
// (llama-swap, custom routers in front of llama-server) return many. We
// enumerate that list and share the context window read from /props across
// the entries, since llama-server is one process per context.
func DiscoverLlamaCPP(ctx context.Context, baseURL string) ([]DiscoveredModel, error) {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	ids, err := fetchLlamaCppModelIDs(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	// /props is best-effort — if it fails or omits n_ctx, fall back to the
	// documented default rather than aborting discovery.
	ctxSize := fetchLlamaCppContextSize(ctx, baseURL)
	if ctxSize == 0 {
		ctxSize = defaultLlamaCppContextSize
	}

	models := make([]DiscoveredModel, 0, len(ids))
	for _, id := range ids {
		models = append(models, DiscoveredModel{
			ID:            id,
			Name:          id,
			Provider:      "llamacpp",
			ContextSize:   ctxSize,
			SupportsTools: true, // assume true for modern llama.cpp
		})
	}
	return models, nil
}

func fetchLlamaCppModelIDs(ctx context.Context, baseURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llama.cpp not reachable at %s: %w", baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("llama.cpp returned status %d on /v1/models", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("llama.cpp /v1/models parse: %w", err)
	}
	if len(body.Data) == 0 {
		return nil, fmt.Errorf("llama.cpp /v1/models returned no entries")
	}
	ids := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID == "" {
			continue
		}
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("llama.cpp /v1/models returned only empty IDs")
	}
	return ids, nil
}

// fetchLlamaCppContextSize returns the configured n_ctx from /props, or 0 if
// the endpoint is unavailable / malformed. Caller applies a default.
func fetchLlamaCppContextSize(ctx context.Context, baseURL string) int {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/props", nil)
	if err != nil {
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return 0
	}
	var data struct {
		DefaultGenerationSettings struct {
			N_Ctx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0
	}
	return data.DefaultGenerationSettings.N_Ctx
}

// DiscoverLocalModels polls all known local providers.
func DiscoverLocalModels(ctx context.Context, logger *slog.Logger, ollamaURL, llamacppURL string, ollamaToolCache map[string]bool) []DiscoveredModel {
	var all []DiscoveredModel

	if models, err := DiscoverOllama(ctx, ollamaURL, ollamaToolCache); err != nil {
		logger.Debug("ollama discovery skipped", "error", err)
	} else {
		all = append(all, models...)
	}

	if models, err := DiscoverLlamaCPP(ctx, llamacppURL); err != nil {
		logger.Debug("llama.cpp discovery skipped", "error", err)
	} else {
		all = append(all, models...)
	}

	return all
}

// StartDiscoveryLoop periodically polls for local models and reconciles with the router.
// onReconcile is called when the forced arm identity changes (may be nil).
func StartDiscoveryLoop(ctx context.Context, r *Router, logger *slog.Logger,
	ollamaURL, llamacppURL string,
	providerFactory func(name, model string) SecureProvider,
	interval time.Duration,
	onReconcile func(ArmID),
) {
	go func() {
		ollamaToolCache := make(map[string]bool)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				models := DiscoverLocalModels(ctx, logger, ollamaURL, llamacppURL, ollamaToolCache)
				reconcileArms(r, models, providerFactory, logger, onReconcile)
			}
		}
	}()
}

// reconcileArms adds newly discovered models, removes disappeared ones, and
// reconciles the forced arm when discovery reveals its real model name.
// onReconcile is called (if non-nil) when the forced arm identity changes.
func reconcileArms(r *Router, discovered []DiscoveredModel, providerFactory func(name, model string) SecureProvider, logger *slog.Logger, onReconcile func(ArmID)) {
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
func RegisterDiscoveredModels(r *Router, models []DiscoveredModel, providerFactory func(name, model string) SecureProvider) {
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
				// Many small local models (phi, etc.) don't support
				// function calling and will produce confused output if selected
				// for tool-requiring tasks. Larger known models (mistral, llama3,
				// qwen2.5-coder, tiny3.5) support tools. Callers can update the arm's
				// Capabilities after probing the model template.
				ToolUse:       m.SupportsTools,
				ContextWindow: m.ContextSize,
			},
		})
	}
}
