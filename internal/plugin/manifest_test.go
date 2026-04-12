package plugin

import (
	"testing"
)

func TestParseManifest_Valid(t *testing.T) {
	data := []byte(`{
		"name": "git-tools",
		"version": "1.0.0",
		"description": "Git integration for gnoma",
		"author": "vikingowl",
		"capabilities": {
			"skills": ["skills/*.md"],
			"hooks": [
				{
					"name": "lint-before-commit",
					"event": "pre_tool_use",
					"type": "command",
					"exec": "scripts/lint.sh",
					"tool_pattern": "bash*"
				}
			],
			"mcp_servers": [
				{
					"name": "git",
					"command": "bin/mcp-git",
					"args": ["--verbose"]
				}
			]
		}
	}`)

	m, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	if m.Name != "git-tools" {
		t.Errorf("Name = %q, want %q", m.Name, "git-tools")
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
	}
	if len(m.Capabilities.Skills) != 1 {
		t.Errorf("Skills count = %d, want 1", len(m.Capabilities.Skills))
	}
	if len(m.Capabilities.Hooks) != 1 {
		t.Errorf("Hooks count = %d, want 1", len(m.Capabilities.Hooks))
	}
	if len(m.Capabilities.MCPServers) != 1 {
		t.Errorf("MCPServers count = %d, want 1", len(m.Capabilities.MCPServers))
	}
}

func TestParseManifest_Minimal(t *testing.T) {
	data := []byte(`{"name": "minimal", "version": "0.1.0"}`)
	m, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Name != "minimal" {
		t.Errorf("Name = %q", m.Name)
	}
}

func TestParseManifest_InvalidJSON(t *testing.T) {
	_, err := ParseManifest([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestManifest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		m       Manifest
		wantErr bool
	}{
		{
			name:    "valid",
			m:       Manifest{Name: "my-plugin", Version: "1.0.0"},
			wantErr: false,
		},
		{
			name:    "empty name",
			m:       Manifest{Version: "1.0.0"},
			wantErr: true,
		},
		{
			name:    "invalid name uppercase",
			m:       Manifest{Name: "MyPlugin", Version: "1.0.0"},
			wantErr: true,
		},
		{
			name:    "invalid name starts with number",
			m:       Manifest{Name: "1plugin", Version: "1.0.0"},
			wantErr: true,
		},
		{
			name:    "empty version",
			m:       Manifest{Name: "my-plugin"},
			wantErr: true,
		},
		{
			name:    "invalid version",
			m:       Manifest{Name: "my-plugin", Version: "not-semver"},
			wantErr: true,
		},
		{
			name: "skill glob path traversal",
			m: Manifest{
				Name:    "bad",
				Version: "1.0.0",
				Capabilities: Capabilities{
					Skills: []string{"../../../etc/passwd"},
				},
			},
			wantErr: true,
		},
		{
			name: "skill glob absolute path",
			m: Manifest{
				Name:    "bad",
				Version: "1.0.0",
				Capabilities: Capabilities{
					Skills: []string{"/etc/passwd"},
				},
			},
			wantErr: true,
		},
		{
			name: "hook exec path traversal",
			m: Manifest{
				Name:    "bad",
				Version: "1.0.0",
				Capabilities: Capabilities{
					Hooks: []HookSpec{{
						Name:  "h",
						Event: "pre_tool_use",
						Type:  "command",
						Exec:  "../../../bin/evil",
					}},
				},
			},
			wantErr: true,
		},
		{
			name: "mcp command path traversal",
			m: Manifest{
				Name:    "bad",
				Version: "1.0.0",
				Capabilities: Capabilities{
					MCPServers: []MCPServerSpec{{
						Name:    "evil",
						Command: "../../../bin/evil",
					}},
				},
			},
			wantErr: true,
		},
		{
			name:    "valid name with hyphens and numbers",
			m:       Manifest{Name: "my-plugin-2", Version: "0.1.0"},
			wantErr: false,
		},
		{
			name:    "valid name with underscores",
			m:       Manifest{Name: "my_plugin", Version: "0.1.0"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.m.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidSemver(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"1.0.0", true},
		{"0.1.0", true},
		{"12.34.56", true},
		{"1.0", false},
		{"1", false},
		{"v1.0.0", false},
		{"1.0.0-beta", false}, // strict semver only for v1
		{"", false},
		{"not-a-version", false},
	}

	for _, tt := range tests {
		t.Run(tt.v, func(t *testing.T) {
			if got := validSemver(tt.v); got != tt.want {
				t.Errorf("validSemver(%q) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}
