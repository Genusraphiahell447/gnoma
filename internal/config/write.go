package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)


// SetProjectConfig writes a single key=value to the project config file (.gnoma/config.toml).
// Only whitelisted keys are supported.
func SetProjectConfig(key, value string) error {
	return setConfig(projectConfigPath(), key, value)
}

// SetGlobalConfig writes a single key=value to the global config file (~/.config/gnoma/config.toml).
// Only whitelisted keys are supported.
func SetGlobalConfig(key, value string) error {
	return setConfig(globalConfigPath(), key, value)
}

func setConfig(path, key, value string) error {
	allowed := map[string]bool{
		"provider.default": true,
		"provider.model":   true,
		"permission.mode":  true,
		"slm.model_url":    true,
		"slm.enabled":      true,
		"slm.data_dir":     true,
	}
	if !allowed[key] {
		return fmt.Errorf("unknown config key %q (supported: %s)", key, strings.Join(allowedKeys(), ", "))
	}

	// Load existing config or start fresh
	var cfg Config
	if data, err := os.ReadFile(path); err == nil {
		toml.Decode(string(data), &cfg) //nolint:errcheck
	}
	if cfg.Provider.APIKeys == nil {
		cfg.Provider.APIKeys = make(map[string]string)
	}
	if cfg.Provider.Endpoints == nil {
		cfg.Provider.Endpoints = make(map[string]string)
	}

	// Apply the change
	switch key {
	case "provider.default":
		cfg.Provider.Default = value
	case "provider.model":
		cfg.Provider.Model = value
	case "permission.mode":
		cfg.Permission.Mode = value
	case "slm.model_url":
		cfg.SLM.ModelURL = value
	case "slm.enabled":
		cfg.SLM.Enabled = value == "true"
	case "slm.data_dir":
		cfg.SLM.DataDir = value
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Write
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	return enc.Encode(cfg)
}

func allowedKeys() []string {
	return []string{
		"provider.default", "provider.model", "permission.mode",
		"slm.model_url", "slm.enabled", "slm.data_dir",
	}
}
