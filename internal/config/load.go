package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Load reads and merges config from all layers.
// Order (lowest to highest priority):
// 1. Defaults
// 2. Global config: ~/.config/gnoma/config.toml
// 3. Project config: .gnoma/config.toml
// 4. Environment variables
func Load() (*Config, error) {
	cfg := Defaults()

	// Layer 1: Global config
	globalPath := globalConfigPath()
	if err := loadTOML(&cfg, globalPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading global config %s: %w", globalPath, err)
	}
	// Deep copy global hooks before the project layer.
	// toml.Decode may reuse the backing array, so a plain slice-header copy
	// would alias into whatever the project decode writes.
	// Also reset cfg.Hooks to nil so the project layer starts clean —
	// if the project config is absent, cfg.Hooks stays nil and the append
	// below just returns the global hooks unchanged.
	globalHooks := append([]HookConfig(nil), cfg.Hooks...)
	cfg.Hooks = nil

	// Layer 2: Project config
	projectPath := projectConfigPath()
	if err := loadTOML(&cfg, projectPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading project config %s: %w", projectPath, err)
	}
	// User hooks run first, project hooks after.
	cfg.Hooks = append(globalHooks, cfg.Hooks...)

	// Layer 3: Environment variables
	applyEnv(&cfg)

	return &cfg, nil
}

func loadTOML(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = toml.Decode(string(data), cfg)
	return err
}

// GlobalConfigDir returns the gnoma global config directory (~/.config/gnoma or $XDG_CONFIG_HOME/gnoma).
func GlobalConfigDir() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "gnoma")
}

func globalConfigPath() string {
	return GlobalConfigPath()
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() string {
	return filepath.Join(GlobalConfigDir(), "config.toml")
}

// ProjectRoot walks up from cwd to find the nearest directory containing
// a go.mod, .git, or .gnoma directory. Falls back to cwd if none found.
func ProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		for _, marker := range []string{"go.mod", ".git", ".gnoma"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}

func projectConfigPath() string {
	return filepath.Join(ProjectRoot(), ".gnoma", "config.toml")
}

func applyEnv(cfg *Config) {
	envKeys := map[string]string{
		"mistral":   "MISTRAL_API_KEY",
		"anthropic": "ANTHROPIC_API_KEY",
		"openai":    "OPENAI_API_KEY",
		"google":    "GEMINI_API_KEY",
	}
	// Also check alternative names
	altKeys := map[string][]string{
		"anthropic": {"ANTHROPICS_API_KEY"},
		"google":    {"GOOGLE_API_KEY"},
	}

	for provider, envVar := range envKeys {
		if key := os.Getenv(envVar); key != "" {
			cfg.Provider.APIKeys[provider] = key
			continue
		}
		for _, alt := range altKeys[provider] {
			if key := os.Getenv(alt); key != "" {
				cfg.Provider.APIKeys[provider] = key
				break
			}
		}
	}

	// Resolve ${VAR} references in configured API keys
	for k, v := range cfg.Provider.APIKeys {
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			envName := v[2 : len(v)-1]
			if resolved := os.Getenv(envName); resolved != "" {
				cfg.Provider.APIKeys[k] = resolved
			}
		}
	}

	// Provider override
	if p := os.Getenv("GNOMA_PROVIDER"); p != "" {
		cfg.Provider.Default = p
	}
	if m := os.Getenv("GNOMA_MODEL"); m != "" {
		cfg.Provider.Model = m
	}
}
