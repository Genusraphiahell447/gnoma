package mcp

import (
	"fmt"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/config"
)

const defaultTimeout = 30 * time.Second

// ServerConfig is the validated, parsed form of config.MCPServerConfig.
type ServerConfig struct {
	Name           string
	Command        string
	Args           []string
	Env            map[string]string
	Timeout        time.Duration
	ReplaceDefault map[string]string // MCP tool name → built-in name to replace
}

// ParseServerConfigs validates and converts raw config entries.
func ParseServerConfigs(raw []config.MCPServerConfig) ([]ServerConfig, error) {
	seen := make(map[string]bool, len(raw))
	result := make([]ServerConfig, 0, len(raw))

	for i, r := range raw {
		if r.Name == "" {
			return nil, fmt.Errorf("mcp_servers[%d]: name is required", i)
		}
		if seen[r.Name] {
			return nil, fmt.Errorf("mcp_servers: duplicate name %q", r.Name)
		}
		seen[r.Name] = true

		if r.Command == "" {
			return nil, fmt.Errorf("mcp_servers[%d] %q: command is required", i, r.Name)
		}

		timeout := defaultTimeout
		if r.Timeout != "" {
			var err error
			timeout, err = time.ParseDuration(r.Timeout)
			if err != nil {
				return nil, fmt.Errorf("mcp_servers[%d] %q: invalid timeout %q: %w", i, r.Name, r.Timeout, err)
			}
		}

		result = append(result, ServerConfig{
			Name:           r.Name,
			Command:        r.Command,
			Args:           r.Args,
			Env:            r.Env,
			Timeout:        timeout,
			ReplaceDefault: r.ReplaceDefault,
		})
	}

	return result, nil
}
