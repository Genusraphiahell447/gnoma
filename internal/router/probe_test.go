package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeLlamaCppToolSupport_SupportsTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"chat_template": "...",
			"chat_template_caps": {
				"supports_tools": true,
				"supports_tool_calls": true,
				"supports_parallel_tool_calls": false,
				"supports_system_role": true
			}
		}`))
	}))
	defer srv.Close()

	got := probeLlamaCppToolSupport(context.Background(), srv.URL)
	if !got {
		t.Error("probeLlamaCppToolSupport() = false, want true for model with tool support")
	}
}

func TestProbeLlamaCppToolSupport_NoToolSupport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"chat_template": "...",
			"chat_template_caps": {
				"supports_tools": false,
				"supports_tool_calls": false,
				"supports_system_role": true
			}
		}`))
	}))
	defer srv.Close()

	got := probeLlamaCppToolSupport(context.Background(), srv.URL)
	if got {
		t.Error("probeLlamaCppToolSupport() = true, want false for model without tool support")
	}
}

func TestProbeLlamaCppToolSupport_NoCaps(t *testing.T) {
	// Old llama.cpp version that doesn't return chat_template_caps
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chat_template": "...", "total_slots": 1}`))
	}))
	defer srv.Close()

	got := probeLlamaCppToolSupport(context.Background(), srv.URL)
	if got {
		t.Error("probeLlamaCppToolSupport() = true, want false when chat_template_caps is absent")
	}
}

func TestProbeLlamaCppToolSupport_ServerDown(t *testing.T) {
	got := probeLlamaCppToolSupport(context.Background(), "http://127.0.0.1:1")
	if got {
		t.Error("probeLlamaCppToolSupport() = true, want false when server unreachable")
	}
}

func TestProbeLlamaCppToolSupport_ToolsWithoutToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"chat_template_caps": {
				"supports_tools": true,
				"supports_tool_calls": false
			}
		}`))
	}))
	defer srv.Close()

	got := probeLlamaCppToolSupport(context.Background(), srv.URL)
	if got {
		t.Error("probeLlamaCppToolSupport() = true, want false when supports_tool_calls is false")
	}
}

func TestProbeOllamaToolSupport_HasTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"details": {"family": "qwen2", "parameter_size": "7B"},
			"capabilities": ["completion", "tools"]
		}`))
	}))
	defer srv.Close()

	got := probeOllamaToolSupport(context.Background(), srv.URL, "qwen2.5:7b")
	if !got {
		t.Error("probeOllamaToolSupport() = false, want true for model with tools capability")
	}
}

func TestProbeOllamaToolSupport_NoTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"details": {"family": "phi", "parameter_size": "3B"},
			"capabilities": ["completion"]
		}`))
	}))
	defer srv.Close()

	got := probeOllamaToolSupport(context.Background(), srv.URL, "phi3:3b")
	if got {
		t.Error("probeOllamaToolSupport() = true, want false for model without tools capability")
	}
}

func TestProbeOllamaToolSupport_NoCapsField(t *testing.T) {
	// Old Ollama version without capabilities
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"details": {"family": "llama"}}`))
	}))
	defer srv.Close()

	got := probeOllamaToolSupport(context.Background(), srv.URL, "llama3:8b")
	if got {
		t.Error("probeOllamaToolSupport() = true, want false when capabilities field absent")
	}
}

func TestProbeOllamaToolSupport_ServerDown(t *testing.T) {
	got := probeOllamaToolSupport(context.Background(), "http://127.0.0.1:1", "test")
	if got {
		t.Error("probeOllamaToolSupport() = true, want false when server unreachable")
	}
}
