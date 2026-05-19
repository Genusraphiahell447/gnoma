package router

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
)

// probeLlamaCppToolSupport queries the llama.cpp /props endpoint to determine
// if the loaded model supports tool calling. Returns false on any error
// (conservative: unknown = no tools).
func probeLlamaCppToolSupport(ctx context.Context, baseURL string) bool {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/props", nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return false
	}

	var result struct {
		ChatTemplateCaps struct {
			SupportsTools     bool `json:"supports_tools"`
			SupportsToolCalls bool `json:"supports_tool_calls"`
		} `json:"chat_template_caps"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Debug("llamacpp /props decode failed", "error", err)
		return false
	}

	caps := result.ChatTemplateCaps
	supported := caps.SupportsTools && caps.SupportsToolCalls
	slog.Debug("llamacpp tool probe",
		"supports_tools", caps.SupportsTools,
		"supports_tool_calls", caps.SupportsToolCalls,
		"result", supported,
	)
	return supported
}

// probeOllamaToolSupport queries Ollama's /api/show endpoint to determine
// if a specific model supports tool calling. Returns false on any error.
func probeOllamaToolSupport(ctx context.Context, baseURL, modelName string) bool {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]string{"model": modelName})
	if err != nil {
		return false
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return false
	}

	var result struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Debug("ollama /api/show decode failed", "model", modelName, "error", err)
		return false
	}

	supported := slices.Contains(result.Capabilities, "tools")
	slog.Debug("ollama tool probe",
		"model", modelName,
		"capabilities", result.Capabilities,
		"supports_tools", supported,
	)
	return supported
}
