package config

import "time"

// Config is the top-level configuration.
type Config struct {
	Provider   ProviderSection   `toml:"provider"`
	Tools      ToolsSection      `toml:"tools"`
}

type ProviderSection struct {
	Default     string            `toml:"default"`
	Model       string            `toml:"model"`
	MaxTokens   int64             `toml:"max_tokens"`
	Temperature *float64          `toml:"temperature"`
	APIKeys     map[string]string `toml:"api_keys"`
	Endpoints   map[string]string `toml:"endpoints"`
}

type ToolsSection struct {
	BashTimeout Duration `toml:"bash_timeout"`
	MaxFileSize int64    `toml:"max_file_size"`
}

// Duration wraps time.Duration for TOML string parsing (e.g. "30s", "5m").
type Duration time.Duration

func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}
