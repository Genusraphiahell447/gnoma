package config

import "time"

// Config is the top-level configuration.
type Config struct {
	Provider   ProviderSection   `toml:"provider"`
	Permission PermissionSection `toml:"permission"`
	Tools      ToolsSection      `toml:"tools"`
	RateLimits RateLimitSection  `toml:"rate_limits"`
}

type PermissionSection struct {
	Mode  string           `toml:"mode"`
	Rules []PermissionRule `toml:"rules"`
}

type PermissionRule struct {
	Tool    string `toml:"tool"`
	Pattern string `toml:"pattern"`
	Action  string `toml:"action"`
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

// RateLimitSection allows overriding default rate limits per provider.
//
// Example config:
//
//	[rate_limits.mistral]
//	tier = "starter"
//	rps = 1
//	spend_cap = 20.0
//
//	[rate_limits.anthropic]
//	tier = "tier2"
//	rpm = 1000
//	itpm = 450000
//	otpm = 90000
type RateLimitSection map[string]RateLimitOverride

type RateLimitOverride struct {
	Tier        string  `toml:"tier"`
	RPS         float64 `toml:"rps"`
	RPM         int     `toml:"rpm"`
	RPD         int     `toml:"rpd"`
	TPM         int     `toml:"tpm"`
	ITPM        int     `toml:"itpm"`
	OTPM        int     `toml:"otpm"`
	TokensMonth int64   `toml:"tokens_month"`
	SpendCap    float64 `toml:"spend_cap"`
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
