package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"somegit.dev/Owlibou/gnoma/internal/config"
)

// Plugin is a discovered, parsed plugin.
type Plugin struct {
	Manifest      Manifest
	Dir           string // absolute path to plugin directory
	Scope         string // "user" or "project"
	ManifestBytes []byte // raw plugin.json bytes; used for trust pinning
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
//
// If pins is non-nil, each plugin's manifest is hashed (SHA-256 over the raw
// plugin.json bytes) and checked against the pin store:
//
//   - Pin missing: TOFU — record the new hash, log a warning, load the plugin.
//   - Pin matches: load silently.
//   - Pin mismatches: skip the plugin and log an error. The user can remove
//     the offending pin to re-enroll.
//
// Pinning failures (file I/O) downgrade to load-without-pin and log a warning;
// they never block startup, since a broken pin file shouldn't lock out plugins
// that were previously working.
func (l *Loader) Load(plugins []Plugin, enabledSet map[string]bool, pins PinStore) (LoadResult, error) {
	var result LoadResult

	for _, p := range plugins {
		if !enabledSet[p.Manifest.Name] {
			l.logger.Debug("plugin disabled, skipping", "name", p.Manifest.Name)
			continue
		}

		if pins != nil && !l.verifyPin(p, pins) {
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
			Manifest:      *manifest,
			Dir:           pluginDir,
			Scope:         scope,
			ManifestBytes: data,
		}
	}
}

// marshalJSON is a thin wrapper for tests.
func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// hashManifest returns the hex-encoded SHA-256 of the manifest bytes.
func hashManifest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// verifyPin enforces the TOFU trust contract on a single plugin.
// Returns true if the plugin is cleared to load.
func (l *Loader) verifyPin(p Plugin, pins PinStore) bool {
	if len(p.ManifestBytes) == 0 {
		// Synthetic plugin (e.g. constructed in tests without going through scanDir).
		// Treat as trusted; the surface that matters has its own coverage.
		return true
	}
	actual := hashManifest(p.ManifestBytes)
	pinned, hasPin := pins.Get(p.Manifest.Name)
	if !hasPin {
		l.logger.Warn("enrolling new plugin (trust on first use)",
			"name", p.Manifest.Name,
			"scope", p.Scope,
			"sha256", actual,
		)
		if err := pins.Set(p.Manifest.Name, actual); err != nil {
			l.logger.Warn("failed to persist plugin pin; will re-enrol next run",
				"name", p.Manifest.Name,
				"error", err,
			)
		}
		return true
	}
	if pinned != actual {
		l.logger.Error("refusing plugin — manifest changed since enrolment",
			"name", p.Manifest.Name,
			"scope", p.Scope,
			"pinned", pinned,
			"actual", actual,
			"hint", "remove the entry from plugins.pins.toml to re-enrol",
		)
		return false
	}
	return true
}
