package slm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/provider/openaicompat"
)

// Backend identifies an SLM execution backend.
type Backend string

const (
	BackendAuto         Backend = "auto"
	BackendOllama       Backend = "ollama"
	BackendLlamaCpp     Backend = "llamacpp"
	BackendLlamafile    Backend = "llamafile"
	BackendOpenAICompat Backend = "openaicompat"
	BackendDisabled     Backend = "disabled"
)

// BackendConfig is the subset of config.SLMSection that StartBackend needs.
// Decoupled from the config package so the slm package can be imported from
// anywhere without a dependency cycle.
type BackendConfig struct {
	Backend        Backend
	Model          string
	BaseURL        string
	ModelURL       string
	DataDir        string
	StartupTimeout time.Duration
	// ToolSupport overrides auto-detection for backends we can't probe
	// generically (openaicompat). Ignored when auto-detection succeeds.
	ToolSupport bool
}

// Boot is a started SLM backend, ready to act as a provider.Provider for the
// classifier and as a router arm. Close is always non-nil; for stateless
// backends (Ollama, llamacpp, openaicompat) it's a no-op.
type Boot struct {
	Backend     Backend
	Provider    provider.Provider
	Model       string
	BootTime    time.Duration
	ToolSupport bool // true when the underlying model is known to handle tool calls
	Close       func() error
}

// StartBackend dispatches by cfg.Backend and returns a started SLM. Returns
// (nil, nil) when the chosen backend is "disabled" or when "auto" found no
// available backend — callers stay on the heuristic classifier silently.
// Returns a non-nil error only when the configuration itself is broken
// (unknown backend, missing required field for an explicit choice).
func StartBackend(ctx context.Context, cfg BackendConfig, logger *slog.Logger) (*Boot, error) {
	if logger == nil {
		logger = slog.Default()
	}

	backend := cfg.Backend
	if backend == "" {
		backend = BackendAuto
	}

	switch backend {
	case BackendDisabled:
		return nil, nil
	case BackendOllama:
		return startOllama(ctx, cfg, logger)
	case BackendLlamaCpp:
		return startLlamaCpp(ctx, cfg, logger)
	case BackendLlamafile:
		return startLlamafile(ctx, cfg, logger)
	case BackendOpenAICompat:
		return startOpenAICompat(ctx, cfg, logger)
	case BackendAuto:
		return autoStart(ctx, cfg, logger)
	default:
		return nil, fmt.Errorf("slm: unknown backend %q", backend)
	}
}

// ---- Backend implementations --------------------------------------------

const (
	ollamaDefaultURL   = "http://localhost:11434"
	llamacppDefaultURL = "http://localhost:8080"
)

func startOllama(_ context.Context, cfg BackendConfig, logger *slog.Logger) (*Boot, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = ollamaDefaultURL
	}
	model := cfg.Model
	if model == "" {
		// Try to pick a sensible default model.
		picked, ok := pickSmallestOllamaModel(baseURL)
		if !ok {
			return nil, fmt.Errorf("slm: ollama backend requires [slm] model, and no models were reachable at %s", baseURL)
		}
		model = picked
		logger.Info("slm: auto-picked Ollama model", "model", model, "base_url", baseURL)
	}
	apiURL := baseURL + "/v1"
	begin := time.Now()
	prov, err := openaicompat.NewOllama(provider.ProviderConfig{BaseURL: apiURL})
	if err != nil {
		return nil, fmt.Errorf("slm: ollama provider: %w", err)
	}
	return &Boot{
		Backend:     BackendOllama,
		Provider:    prov,
		Model:       model,
		BootTime:    time.Since(begin),
		ToolSupport: probeOllamaToolSupport(baseURL, model),
		Close:       func() error { return nil },
	}, nil
}

func startLlamaCpp(_ context.Context, cfg BackendConfig, logger *slog.Logger) (*Boot, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = llamacppDefaultURL
	}
	model := cfg.Model
	if model == "" {
		model = "default" // llama.cpp server ignores the model field
	}
	apiURL := baseURL + "/v1"
	begin := time.Now()
	prov, err := openaicompat.NewLlamaCpp(provider.ProviderConfig{BaseURL: apiURL})
	if err != nil {
		return nil, fmt.Errorf("slm: llamacpp provider: %w", err)
	}
	logger.Info("slm: using llama.cpp backend", "base_url", baseURL, "model", model)
	return &Boot{
		Backend:     BackendLlamaCpp,
		Provider:    prov,
		Model:       model,
		BootTime:    time.Since(begin),
		ToolSupport: probeLlamacppToolSupport(baseURL),
		Close:       func() error { return nil },
	}, nil
}

func startOpenAICompat(_ context.Context, cfg BackendConfig, logger *slog.Logger) (*Boot, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("slm: openaicompat backend requires [slm] base_url")
	}
	model := cfg.Model
	if model == "" {
		return nil, fmt.Errorf("slm: openaicompat backend requires [slm] model")
	}
	begin := time.Now()
	prov, err := openaicompat.NewLlamafile(provider.ProviderConfig{BaseURL: baseURL})
	if err != nil {
		return nil, fmt.Errorf("slm: openaicompat provider: %w", err)
	}
	logger.Info("slm: using openai-compatible backend", "base_url", baseURL, "model", model)
	return &Boot{
		Backend:     BackendOpenAICompat,
		Provider:    prov,
		Model:       model,
		BootTime:    time.Since(begin),
		ToolSupport: cfg.ToolSupport, // user-asserted; no generic probe
		Close:       func() error { return nil },
	}, nil
}

func startLlamafile(ctx context.Context, cfg BackendConfig, logger *slog.Logger) (*Boot, error) {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	mgr := New(Config{DataDir: dataDir, ModelURL: cfg.ModelURL}, logger)
	if !mgr.IsSetUp() {
		return nil, fmt.Errorf("slm: llamafile not set up; run: gnoma slm setup")
	}

	timeout := cfg.StartupTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	bootCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	baseURL, err := mgr.Start(bootCtx)
	if err != nil {
		return nil, fmt.Errorf("slm: start llamafile: %w", err)
	}
	prov, err := openaicompat.NewLlamafile(provider.ProviderConfig{BaseURL: baseURL + "/v1"})
	if err != nil {
		_ = mgr.Stop()
		return nil, fmt.Errorf("slm: llamafile provider: %w", err)
	}
	return &Boot{
		Backend:     BackendLlamafile,
		Provider:    prov,
		Model:       "default",
		BootTime:    mgr.StartupDuration(),
		ToolSupport: probeLlamacppToolSupport(baseURL), // llamafile speaks the llama.cpp server protocol
		Close:       func() error { return mgr.Stop() },
	}, nil
}

// autoStart picks the first available backend in priority order:
//
//  1. Explicit llamafile (ModelURL or DataDir is set, AND the manifest is on
//     disk) — respects users who already ran `gnoma slm setup`.
//  2. Ollama, if reachable with at least one model.
//  3. llama.cpp, if reachable.
//  4. llamafile, if a manifest happens to exist anywhere.
//  5. Nothing → returns (nil, nil); caller stays on heuristic classifier.
func autoStart(ctx context.Context, cfg BackendConfig, logger *slog.Logger) (*Boot, error) {
	// Hint: if the user has llamafile config set, prefer it.
	if cfg.ModelURL != "" {
		mgr := New(Config{
			DataDir:  defaultIfEmpty(cfg.DataDir, DefaultDataDir()),
			ModelURL: cfg.ModelURL,
		}, logger)
		if mgr.IsSetUp() {
			return startLlamafile(ctx, cfg, logger)
		}
	}

	if model, ok := pickSmallestOllamaModel(ollamaDefaultURL); ok {
		c := cfg
		c.Model = model
		boot, err := startOllama(ctx, c, logger)
		if err == nil {
			return boot, nil
		}
		logger.Debug("slm auto: ollama probe found models but provider init failed", "error", err)
	}

	if llamacppReachable(llamacppDefaultURL) {
		boot, err := startLlamaCpp(ctx, cfg, logger)
		if err == nil {
			return boot, nil
		}
	}

	mgr := New(Config{DataDir: DefaultDataDir(), ModelURL: cfg.ModelURL}, logger)
	if mgr.IsSetUp() {
		return startLlamafile(ctx, cfg, logger)
	}

	logger.Info("slm auto: no backend reachable; staying on heuristic classifier")
	return nil, nil
}

// ---- Discovery helpers --------------------------------------------------

// pickSmallestOllamaModel returns the model with the smallest reported size
// from the Ollama /api/tags endpoint. Returns ("", false) when Ollama is not
// reachable or has no models.
func pickSmallestOllamaModel(baseURL string) (string, bool) {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", false
	}
	if len(body.Models) == 0 {
		return "", false
	}
	sort.Slice(body.Models, func(i, j int) bool {
		return body.Models[i].Size < body.Models[j].Size
	})
	return body.Models[0].Name, true
}

func llamacppReachable(baseURL string) bool {
	client := &http.Client{Timeout: 750 * time.Millisecond}
	resp, err := client.Get(baseURL + "/props")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// probeOllamaToolSupport asks Ollama's /api/show whether a given model
// advertises the "tools" capability. Returns false on any error or when
// the capability is missing — conservative: assume no tools when unsure.
func probeOllamaToolSupport(baseURL, model string) bool {
	body, err := json.Marshal(map[string]string{"model": model})
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var r struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return false
	}
	for _, c := range r.Capabilities {
		if c == "tools" {
			return true
		}
	}
	return false
}

// probeLlamacppToolSupport asks llama.cpp's /props endpoint whether the
// chat template advertises tool support. Same convention as Ollama: assume
// no tools when the probe fails.
func probeLlamacppToolSupport(baseURL string) bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(baseURL + "/props")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var r struct {
		ChatTemplateCaps struct {
			SupportsTools     bool `json:"supports_tools"`
			SupportsToolCalls bool `json:"supports_tool_calls"`
		} `json:"chat_template_caps"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return false
	}
	return r.ChatTemplateCaps.SupportsTools && r.ChatTemplateCaps.SupportsToolCalls
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
