package config

import "time"

// Config is the top-level configuration.
type Config struct {
	Provider   ProviderSection   `toml:"provider"`
	Permission PermissionSection `toml:"permission"`
	Tools      ToolsSection      `toml:"tools"`
	RateLimits RateLimitSection  `toml:"rate_limits"`
	Security   SecuritySection   `toml:"security"`
	Session    SessionSection    `toml:"session"`
	SLM        SLMSection        `toml:"slm"`
	Hooks      []HookConfig      `toml:"hooks"`
	MCPServers []MCPServerConfig `toml:"mcp_servers"`
	Plugins    PluginsSection    `toml:"plugins"`
}

// SLMSection configures the optional small language model for task classification
// and low-complexity task execution.
//
// Example config:
//
//	[slm]
//	enabled = true
//	model_url = "https://huggingface.co/mozilla-ai/TinyLlama-1.1B-Chat-v1.0-llamafile/resolve/main/TinyLlama-1.1B-Chat-v1.0.Q5_K_M.llamafile"
//
// Run `gnoma slm setup` to download and verify the model before enabling.
type SLMSection struct {
	Enabled  bool   `toml:"enabled"`
	ModelURL string `toml:"model_url"`
	DataDir  string `toml:"data_dir"` // empty = XDG default (~/.local/share/gnoma/slm)
}

// MCPServerConfig defines an MCP server to start and connect to.
//
// Example:
//
//	[[mcp_servers]]
//	name = "git"
//	command = "mcp-server-git"
//	args = ["--repo", "."]
//	env = { GIT_DIR = ".git" }
//	timeout = "30s"
//	replace_default = { exec = "bash" }  # MCP tool "exec" replaces built-in "bash"
type MCPServerConfig struct {
	Name           string            `toml:"name"`
	Command        string            `toml:"command"`
	Args           []string          `toml:"args"`
	Env            map[string]string `toml:"env"`
	Timeout        string            `toml:"timeout"`
	ReplaceDefault map[string]string `toml:"replace_default"` // MCP tool name → built-in name
}

// PluginsSection controls plugin loading.
//
// Example:
//
//	[plugins]
//	enabled = ["git-tools", "docker-tools"]
//	disabled = ["experimental-plugin"]
type PluginsSection struct {
	Enabled  []string `toml:"enabled"`
	Disabled []string `toml:"disabled"`
}

// HookConfig is a single hook entry from TOML config.
//
// Example:
//
//	[[hooks]]
//	name = "block-dangerous-bash"
//	event = "pre_tool_use"
//	type = "command"
//	exec = "bash-safety-check.sh"
//	tool_pattern = "bash*"
//	timeout = "10s"
//	fail_open = false
type HookConfig struct {
	Name        string `toml:"name"`
	Event       string `toml:"event"`
	Type        string `toml:"type"`
	Exec        string `toml:"exec"`
	Timeout     string `toml:"timeout"`
	FailOpen    bool   `toml:"fail_open"`
	ToolPattern string `toml:"tool_pattern"`
}

type SessionSection struct {
	MaxKeep int `toml:"max_keep"`
}

// SecuritySection configures the secret scanner and firewall.
//
// Example config:
//
//	[security]
//	entropy_threshold = 4.5
//
//	[[security.patterns]]
//	name = "internal_token"
//	regex = "mycompany_[a-zA-Z0-9]{32}"
//	action = "redact"
type SecuritySection struct {
	EntropyThreshold float64         `toml:"entropy_threshold"`
	Patterns         []PatternConfig `toml:"patterns"`
}

type PatternConfig struct {
	Name   string `toml:"name"`
	Regex  string `toml:"regex"`
	Action string `toml:"action"` // "redact" (default), "block", "warn"
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
	Temperature *float64          `toml:"temperature"` // TODO(M8): wire to provider.Request.Temperature
	APIKeys     map[string]string `toml:"api_keys"`
	Endpoints   map[string]string `toml:"endpoints"`
}

type ToolsSection struct {
	BashTimeout Duration `toml:"bash_timeout"`
	MaxFileSize int64    `toml:"max_file_size"` // TODO(M8): wire to fs tool WithMaxFileSize option
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
