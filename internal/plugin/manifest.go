package plugin

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Manifest describes a plugin package.
type Manifest struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Description  string       `json:"description"`
	Author       string       `json:"author"`
	License      string       `json:"license"`
	GnomaVersion string       `json:"gnoma_version"`
	Capabilities Capabilities `json:"capabilities"`
}

// Capabilities declares what a plugin provides.
type Capabilities struct {
	Skills     []string        `json:"skills"`
	Hooks      []HookSpec      `json:"hooks"`
	MCPServers []MCPServerSpec `json:"mcp_servers"`
}

// HookSpec defines a hook within a plugin manifest.
type HookSpec struct {
	Name        string `json:"name"`
	Event       string `json:"event"`
	Type        string `json:"type"`
	Exec        string `json:"exec"`
	Timeout     string `json:"timeout"`
	FailOpen    bool   `json:"fail_open"`
	ToolPattern string `json:"tool_pattern"`
}

// MCPServerSpec defines an MCP server within a plugin manifest.
type MCPServerSpec struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// ParseManifest parses and validates a plugin.json file.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks manifest fields for correctness and safety.
func (m *Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("%w: name is required", ErrManifestInvalid)
	}
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("%w: name %q must match %s", ErrManifestInvalid, m.Name, namePattern)
	}
	if m.Version == "" {
		return fmt.Errorf("%w: version is required", ErrManifestInvalid)
	}
	if !validSemver(m.Version) {
		return fmt.Errorf("%w: version %q is not valid semver (expected major.minor.patch)", ErrManifestInvalid, m.Version)
	}

	for _, glob := range m.Capabilities.Skills {
		if err := checkSafePath(glob); err != nil {
			return fmt.Errorf("%w: skill glob %q: %v", ErrManifestInvalid, glob, err)
		}
	}

	for _, h := range m.Capabilities.Hooks {
		if h.Exec != "" {
			if err := checkSafePath(h.Exec); err != nil {
				return fmt.Errorf("%w: hook %q exec: %v", ErrManifestInvalid, h.Name, err)
			}
		}
	}

	for _, s := range m.Capabilities.MCPServers {
		if s.Command != "" {
			if err := checkSafePath(s.Command); err != nil {
				return fmt.Errorf("%w: mcp_server %q command: %v", ErrManifestInvalid, s.Name, err)
			}
		}
	}

	return nil
}

// checkSafePath rejects absolute paths and path traversal.
func checkSafePath(p string) error {
	if filepath.IsAbs(p) {
		return fmt.Errorf("%w: absolute path not allowed", ErrPathTraversal)
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("%w: path traversal not allowed", ErrPathTraversal)
	}
	return nil
}

// validSemver checks for strict major.minor.patch format.
func validSemver(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}
