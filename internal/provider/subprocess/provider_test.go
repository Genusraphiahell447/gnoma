package subprocess

import (
	"context"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestProvider_NameAndDefaultModel(t *testing.T) {
	agent := DiscoveredAgent{
		CLIAgent: CLIAgent{
			Name:         "testcli",
			DisplayName:  "Test CLI",
			Format:       FormatVibeStreaming,
			Capabilities: provider.Capabilities{ContextWindow: 32000},
		},
		Path:    "/usr/bin/testcli",
		Version: "1.0.0",
	}
	p := New(agent)

	if p.Name() != "subprocess" {
		t.Errorf("Name() = %q, want %q", p.Name(), "subprocess")
	}
	if p.DefaultModel() != "testcli" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "testcli")
	}
}

func TestProvider_Models(t *testing.T) {
	agent := DiscoveredAgent{
		CLIAgent: CLIAgent{
			Name:        "claude",
			DisplayName: "Claude Code",
			Format:      FormatClaudeStreamJSON,
			Capabilities: provider.Capabilities{
				ContextWindow: 200000,
			},
		},
		Path:    "/usr/bin/claude",
		Version: "2.1.0",
	}
	p := New(agent)

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("Models() returned %d models, want 1", len(models))
	}
	m := models[0]
	if m.Provider != "subprocess" {
		t.Errorf("model.Provider = %q, want %q", m.Provider, "subprocess")
	}
	if m.Capabilities.ContextWindow != 200000 {
		t.Errorf("ContextWindow = %d, want 200000", m.Capabilities.ContextWindow)
	}
}

func TestProvider_Stream_MissingBinary(t *testing.T) {
	agent := DiscoveredAgent{
		CLIAgent: CLIAgent{
			Name:        "nonexistentcli",
			DisplayName: "Nonexistent",
			Format:      FormatVibeStreaming,
			PromptArgs:  func(p string) []string { return []string{p} },
		},
		Path: "/nonexistent/cli",
	}
	p := New(agent)

	req := provider.Request{}
	_, err := p.Stream(context.Background(), req)
	if err == nil {
		t.Error("expected error for nonexistent binary, got nil")
	}
}
