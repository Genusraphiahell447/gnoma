package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Load reads and merges config from all layers.
//
// Backward-compatible entry point: callers that don't care about
// profiles get the same behaviour they always had. Behind the scenes
// this delegates to LoadWithProfile(""), which engages profile mode
// only when ~/.config/gnoma/profiles/ exists.
//
// Order (lowest to highest priority):
//  1. Defaults
//  2. Global config: ~/.config/gnoma/config.toml
//  3. Selected profile (only if profiles/ exists): profiles/<name>.toml
//  4. Project config: .gnoma/config.toml
//  5. Environment variables
func Load() (*Config, error) {
	cfg, _, err := LoadWithProfile("")
	return cfg, err
}

// LoadBase reads only the defaults + global ~/.config/gnoma/config.toml,
// without consulting profiles, the project config, or env. This is the
// safe-mode loader for diagnostic commands (`gnoma profile list/show`)
// that must work even when a user's profile configuration is broken.
// A missing base config is not an error — defaults are returned.
func LoadBase() (*Config, error) {
	cfg := Defaults()
	globalPath := globalConfigPath()
	if err := loadTOML(&cfg, globalPath); err != nil && !os.IsNotExist(err) {
		return &cfg, err
	}
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
