package plugin

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// PluginInfo is a lightweight summary for listing plugins.
type PluginInfo struct {
	Name    string
	Version string
	Scope   string
	Dir     string
}

// Manager handles plugin install/uninstall lifecycle.
type Manager struct {
	globalDir  string
	projectDir string
	logger     *slog.Logger
}

// NewManager creates a plugin manager.
func NewManager(globalDir, projectDir string, logger *slog.Logger) *Manager {
	return &Manager{
		globalDir:  globalDir,
		projectDir: projectDir,
		logger:     logger,
	}
}

// Install copies a plugin from src to the target scope directory.
func (m *Manager) Install(src, scope string) error {
	manifestPath := filepath.Join(src, "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrManifestNotFound, err)
	}

	manifest, err := ParseManifest(data)
	if err != nil {
		return err
	}

	targetDir := m.scopeDir(scope)
	destDir := filepath.Join(targetDir, manifest.Name)

	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("%w: %q already exists at %s", ErrAlreadyInstalled, manifest.Name, destDir)
	}

	if err := copyDir(src, destDir); err != nil {
		return fmt.Errorf("plugin install %q: %w", manifest.Name, err)
	}

	m.logger.Info("plugin installed", "name", manifest.Name, "version", manifest.Version, "scope", scope)
	return nil
}

// Uninstall removes a plugin directory.
func (m *Manager) Uninstall(name, scope string) error {
	targetDir := filepath.Join(m.scopeDir(scope), name)

	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return fmt.Errorf("%w: %q not found in %s scope", ErrNotFound, name, scope)
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("plugin uninstall %q: %w", name, err)
	}

	m.logger.Info("plugin uninstalled", "name", name, "scope", scope)
	return nil
}

// List returns info about all installed plugins across both scopes.
func (m *Manager) List() ([]PluginInfo, error) {
	var infos []PluginInfo
	m.listDir(m.globalDir, "user", &infos)
	m.listDir(m.projectDir, "project", &infos)
	return infos, nil
}

func (m *Manager) scopeDir(scope string) string {
	if scope == "project" {
		return m.projectDir
	}
	return m.globalDir
}

func (m *Manager) listDir(dir, scope string, infos *[]PluginInfo) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(filepath.Join(pluginDir, "plugin.json"))
		if err != nil {
			continue
		}

		manifest, err := ParseManifest(data)
		if err != nil {
			continue
		}

		*infos = append(*infos, PluginInfo{
			Name:    manifest.Name,
			Version: manifest.Version,
			Scope:   scope,
			Dir:     pluginDir,
		})
	}
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return os.WriteFile(targetPath, data, info.Mode())
	})
}
