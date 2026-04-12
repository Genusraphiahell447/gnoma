package plugin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"somegit.dev/Owlibou/gnoma/internal/config"
)

// Plugin is a discovered, parsed plugin.
type Plugin struct {
	Manifest Manifest
	Dir      string // absolute path to plugin directory
	Scope    string // "user" or "project"
}

// SkillSource is a directory + source tag for skill.Registry.LoadDir.
type SkillSource struct {
	Dir    string
	Source string
}

// LoadResult contains the merged capabilities from all loaded plugins.
type LoadResult struct {
	Skills     []SkillSource
	Hooks      []config.HookConfig
	MCPServers []config.MCPServerConfig
}

// Loader discovers and loads plugins from directories.
type Loader struct {
	logger *slog.Logger
}

// NewLoader creates a plugin loader.
func NewLoader(logger *slog.Logger) *Loader {
	return &Loader{logger: logger}
}

// Discover scans global and project plugin directories, returning all valid plugins.
// Project-scoped plugins override same-name global plugins.
func (l *Loader) Discover(globalDir, projectDir string) ([]Plugin, error) {
	byName := make(map[string]Plugin)

	// Global plugins first (user scope).
	l.scanDir(globalDir, "user", byName)

	// Project plugins override global.
	l.scanDir(projectDir, "project", byName)

	plugins := make([]Plugin, 0, len(byName))
	for _, p := range byName {
		plugins = append(plugins, p)
	}
	return plugins, nil
}

// Load processes enabled plugins and extracts their capabilities.
func (l *Loader) Load(plugins []Plugin, enabledSet map[string]bool) (LoadResult, error) {
	var result LoadResult

	for _, p := range plugins {
		if !enabledSet[p.Manifest.Name] {
			l.logger.Debug("plugin disabled, skipping", "name", p.Manifest.Name)
			continue
		}

		l.logger.Debug("loading plugin", "name", p.Manifest.Name, "scope", p.Scope)

		// Skills: resolve glob directories.
		for _, glob := range p.Manifest.Capabilities.Skills {
			// Use the directory portion of the glob as the skill source dir.
			skillDir := filepath.Join(p.Dir, filepath.Dir(glob))
			result.Skills = append(result.Skills, SkillSource{
				Dir:    skillDir,
				Source: fmt.Sprintf("plugin:%s", p.Manifest.Name),
			})
		}

		// Hooks: convert to config.HookConfig with resolved paths.
		for _, h := range p.Manifest.Capabilities.Hooks {
			execPath := h.Exec
			if execPath != "" && !filepath.IsAbs(execPath) {
				execPath = filepath.Join(p.Dir, execPath)
			}
			result.Hooks = append(result.Hooks, config.HookConfig{
				Name:        h.Name,
				Event:       h.Event,
				Type:        h.Type,
				Exec:        execPath,
				Timeout:     h.Timeout,
				FailOpen:    h.FailOpen,
				ToolPattern: h.ToolPattern,
			})
		}

		// MCP servers: convert with resolved command paths.
		for _, s := range p.Manifest.Capabilities.MCPServers {
			cmd := s.Command
			if cmd != "" && !filepath.IsAbs(cmd) {
				cmd = filepath.Join(p.Dir, cmd)
			}
			result.MCPServers = append(result.MCPServers, config.MCPServerConfig{
				Name:    s.Name,
				Command: cmd,
				Args:    s.Args,
				Env:     s.Env,
			})
		}
	}

	return result, nil
}

func (l *Loader) scanDir(dir, scope string, byName map[string]Plugin) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Missing directory is fine (not all users have plugins).
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(pluginDir, "plugin.json")

		data, err := os.ReadFile(manifestPath)
		if err != nil {
			l.logger.Debug("skipping plugin dir (no manifest)", "dir", pluginDir)
			continue
		}

		manifest, err := ParseManifest(data)
		if err != nil {
			l.logger.Warn("skipping plugin (invalid manifest)", "dir", pluginDir, "error", err)
			continue
		}

		byName[manifest.Name] = Plugin{
			Manifest: *manifest,
			Dir:      pluginDir,
			Scope:    scope,
		}
	}
}

// marshalJSON is a thin wrapper for tests.
func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
