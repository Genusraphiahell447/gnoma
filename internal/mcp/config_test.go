package mcp

import (
	"testing"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/config"
)

func TestParseServerConfigs_Valid(t *testing.T) {
	raw := []config.MCPServerConfig{
		{
			Name:           "git",
			Command:        "mcp-server-git",
			Args:           []string{"--repo", "."},
			Env:            map[string]string{"GIT_DIR": ".git"},
			Timeout:        "10s",
			ReplaceDefault: map[string]string{"exec": "bash"},
		},
		{
			Name:    "docker",
			Command: "mcp-server-docker",
		},
	}

	got, err := ParseServerConfigs(raw)
	if err != nil {
		t.Fatalf("ParseServerConfigs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d configs, want 2", len(got))
	}

	if got[0].Name != "git" {
		t.Errorf("config[0].Name = %q, want %q", got[0].Name, "git")
	}
	if got[0].Timeout != 10*time.Second {
		t.Errorf("config[0].Timeout = %v, want %v", got[0].Timeout, 10*time.Second)
	}
	if got[0].ReplaceDefault["exec"] != "bash" {
		t.Errorf("config[0].ReplaceDefault = %v, want map[exec:bash]", got[0].ReplaceDefault)
	}

	// Second config should get default timeout.
	if got[1].Timeout != defaultTimeout {
		t.Errorf("config[1].Timeout = %v, want default %v", got[1].Timeout, defaultTimeout)
	}
}

func TestParseServerConfigs_Errors(t *testing.T) {
	tests := []struct {
		name string
		raw  []config.MCPServerConfig
	}{
		{
			name: "missing name",
			raw:  []config.MCPServerConfig{{Command: "foo"}},
		},
		{
			name: "missing command",
			raw:  []config.MCPServerConfig{{Name: "foo"}},
		},
		{
			name: "duplicate name",
			raw: []config.MCPServerConfig{
				{Name: "foo", Command: "a"},
				{Name: "foo", Command: "b"},
			},
		},
		{
			name: "bad timeout",
			raw:  []config.MCPServerConfig{{Name: "foo", Command: "bar", Timeout: "not-a-duration"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseServerConfigs(tt.raw)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}
