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
	Router     RouterSection     `toml:"router"`
	CLIAgents  CLIAgentsSection  `toml:"cli_agents"`
	Hooks      []HookConfig      `toml:"hooks"`
	MCPServers []MCPServerConfig `toml:"mcp_servers"`
	Plugins    PluginsSection    `toml:"plugins"`
}

// SLMSection configures the optional small language model used for task
// classification and low-complexity task execution.
//
// Backend selects how the SLM is reached:
//   - "auto" / "" — pick the best available local backend at startup
//     (Ollama → llama.cpp → llamafile)
//   - "ollama"       — talk to a local Ollama daemon
//   - "llamacpp"     — talk to a local llama.cpp server
//   - "llamafile"    — gnoma manages the llamafile process itself
//   - "openaicompat" — any OpenAI-compatible URL (LM Studio, vLLM, etc.)
//   - "disabled"     — skip the SLM entirely; classifier stays heuristic
//
// See docs/slm-backends.md for copy-paste presets.
type SLMSection struct {
	Enabled        bool     `toml:"enabled"`
	Backend        string   `toml:"backend"`         // auto | ollama | llamacpp | llamafile | openaicompat | disabled (empty = auto)
	Model          string   `toml:"model"`           // model name (ollama/llamacpp/openaicompat); ignored for llamafile
	BaseURL        string   `toml:"base_url"`        // server URL; defaults per-backend
	ModelURL       string   `toml:"model_url"`       // llamafile-only: where to download the binary from
	DataDir        string   `toml:"data_dir"`        // llamafile-only: where to put it (empty = XDG default)
	StartupTimeout Duration `toml:"startup_timeout"` // llamafile-only: first-launch wait budget; 0 = default 5s
}

// CLIAgentsSection maps canonical CLI agent names to override binary names.
//
// Useful when a user has aliased the canonical binary — e.g. `claude-priv`
// instead of `claude`, or `gemini-work` instead of `gemini` — and wants
// gnoma's auto-discovery to find it.
//
// Example:
//
//	[cli_agents]
//	claude = "claude-priv"   # use claude-priv as the Claude Code binary
//	gemini = "gemini-work"
//	# vibe is unset → falls back to the canonical "vibe" name
//
// An empty value (e.g. `claude = ""`) is treated as "no override" — the
// canonical name is used.
type CLIAgentsSection map[string]string

// RouterSection holds router-level overrides. Most routing decisions are
// driven automatically by arm capabilities and the bandit; this section
// exists for the rare overrides that don't fit elsewhere.
type RouterSection struct {
	// ForceTwoStage forces the two-stage tool-routing path regardless of
	// arm context window. Useful for debugging or for forcing the behavior
	// on a large local model. Defaults to false: two-stage activates
	// automatically on local arms with context window <= 16k.
	ForceTwoStage bool `toml:"force_two_stage"`
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
