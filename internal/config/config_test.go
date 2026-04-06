package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Provider.Default != "mistral" {
		t.Errorf("Provider.Default = %q, want mistral", cfg.Provider.Default)
	}
	if cfg.Provider.MaxTokens != 8192 {
		t.Errorf("Provider.MaxTokens = %d", cfg.Provider.MaxTokens)
	}
	if cfg.Tools.BashTimeout.Duration() != 30*time.Second {
		t.Errorf("Tools.BashTimeout = %v", cfg.Tools.BashTimeout)
	}
}

func TestLoadTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[provider]
default = "anthropic"
model = "claude-sonnet-4"
max_tokens = 16384

[provider.api_keys]
anthropic = "sk-test-123"

[provider.endpoints]
ollama = "http://myhost:11434/v1"

[tools]
bash_timeout = "60s"
max_file_size = 2097152
`
	os.WriteFile(path, []byte(content), 0o644)

	cfg := Defaults()
	if err := loadTOML(&cfg, path); err != nil {
		t.Fatalf("loadTOML: %v", err)
	}

	if cfg.Provider.Default != "anthropic" {
		t.Errorf("Provider.Default = %q", cfg.Provider.Default)
	}
	if cfg.Provider.Model != "claude-sonnet-4" {
		t.Errorf("Provider.Model = %q", cfg.Provider.Model)
	}
	if cfg.Provider.MaxTokens != 16384 {
		t.Errorf("Provider.MaxTokens = %d", cfg.Provider.MaxTokens)
	}
	if cfg.Provider.APIKeys["anthropic"] != "sk-test-123" {
		t.Errorf("APIKeys[anthropic] = %q", cfg.Provider.APIKeys["anthropic"])
	}
	if cfg.Provider.Endpoints["ollama"] != "http://myhost:11434/v1" {
		t.Errorf("Endpoints[ollama] = %q", cfg.Provider.Endpoints["ollama"])
	}
	if cfg.Tools.BashTimeout.Duration() != 60*time.Second {
		t.Errorf("Tools.BashTimeout = %v", cfg.Tools.BashTimeout)
	}
	if cfg.Tools.MaxFileSize != 2097152 {
		t.Errorf("Tools.MaxFileSize = %d", cfg.Tools.MaxFileSize)
	}
}

func TestLoadTOML_FileNotFound(t *testing.T) {
	cfg := Defaults()
	err := loadTOML(&cfg, "/nonexistent/config.toml")
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist, got %v", err)
	}
}

func TestApplyEnv(t *testing.T) {
	cfg := Defaults()

	t.Setenv("MISTRAL_API_KEY", "sk-mistral-test")
	t.Setenv("ANTHROPICS_API_KEY", "sk-anthro-test")
	t.Setenv("GOOGLE_API_KEY", "goog-test")
	t.Setenv("GNOMA_PROVIDER", "openai")
	t.Setenv("GNOMA_MODEL", "gpt-4o-mini")

	applyEnv(&cfg)

	if cfg.Provider.APIKeys["mistral"] != "sk-mistral-test" {
		t.Errorf("APIKeys[mistral] = %q", cfg.Provider.APIKeys["mistral"])
	}
	if cfg.Provider.APIKeys["anthropic"] != "sk-anthro-test" {
		t.Errorf("APIKeys[anthropic] = %q (should pick ANTHROPICS_API_KEY)", cfg.Provider.APIKeys["anthropic"])
	}
	if cfg.Provider.APIKeys["google"] != "goog-test" {
		t.Errorf("APIKeys[google] = %q (should pick GOOGLE_API_KEY)", cfg.Provider.APIKeys["google"])
	}
	if cfg.Provider.Default != "openai" {
		t.Errorf("Provider.Default = %q, want openai (from GNOMA_PROVIDER)", cfg.Provider.Default)
	}
	if cfg.Provider.Model != "gpt-4o-mini" {
		t.Errorf("Provider.Model = %q, want gpt-4o-mini (from GNOMA_MODEL)", cfg.Provider.Model)
	}
}

func TestApplyEnv_EnvVarReference(t *testing.T) {
	cfg := Defaults()
	cfg.Provider.APIKeys["custom"] = "${MY_CUSTOM_KEY}"

	t.Setenv("MY_CUSTOM_KEY", "resolved-value")

	applyEnv(&cfg)

	if cfg.Provider.APIKeys["custom"] != "resolved-value" {
		t.Errorf("APIKeys[custom] = %q, want resolved-value", cfg.Provider.APIKeys["custom"])
	}
}

func TestProjectRoot_GoMod(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "util")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/foo\n"), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(sub)
	defer os.Chdir(origDir)

	got := ProjectRoot()
	if got != root {
		t.Errorf("ProjectRoot() = %q, want %q", got, root)
	}
}

func TestProjectRoot_Git(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	os.MkdirAll(sub, 0o755)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)

	origDir, _ := os.Getwd()
	os.Chdir(sub)
	defer os.Chdir(origDir)

	got := ProjectRoot()
	if got != root {
		t.Errorf("ProjectRoot() = %q, want %q", got, root)
	}
}

func TestProjectRoot_GnomaDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "internal")
	os.MkdirAll(sub, 0o755)
	os.MkdirAll(filepath.Join(root, ".gnoma"), 0o755)

	origDir, _ := os.Getwd()
	os.Chdir(sub)
	defer os.Chdir(origDir)

	got := ProjectRoot()
	if got != root {
		t.Errorf("ProjectRoot() = %q, want %q", got, root)
	}
}

func TestProjectRoot_Fallback(t *testing.T) {
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	got := ProjectRoot()
	if got != dir {
		t.Errorf("ProjectRoot() = %q, want %q (cwd fallback)", got, dir)
	}
}

func TestHookConfig_TOML_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[[hooks]]
name = "log-tools"
event = "post_tool_use"
type = "command"
exec = "tee -a /tmp/tools.log"
timeout = "5s"
fail_open = true
tool_pattern = "bash*"
`), 0o644)

	cfg := Defaults()
	if err := loadTOML(&cfg, path); err != nil {
		t.Fatalf("loadTOML: %v", err)
	}
	if len(cfg.Hooks) != 1 {
		t.Fatalf("len(Hooks) = %d, want 1", len(cfg.Hooks))
	}
	h := cfg.Hooks[0]
	if h.Name != "log-tools" {
		t.Errorf("Name = %q", h.Name)
	}
	if h.Event != "post_tool_use" {
		t.Errorf("Event = %q", h.Event)
	}
	if h.Type != "command" {
		t.Errorf("Type = %q", h.Type)
	}
	if h.Exec != "tee -a /tmp/tools.log" {
		t.Errorf("Exec = %q", h.Exec)
	}
	if h.Timeout != "5s" {
		t.Errorf("Timeout = %q", h.Timeout)
	}
	if !h.FailOpen {
		t.Error("FailOpen should be true")
	}
	if h.ToolPattern != "bash*" {
		t.Errorf("ToolPattern = %q", h.ToolPattern)
	}
}

func TestHookConfig_MergeOrder(t *testing.T) {
	globalDir := t.TempDir()
	gnomaDir := filepath.Join(globalDir, "gnoma")
	os.MkdirAll(gnomaDir, 0o755)
	os.WriteFile(filepath.Join(gnomaDir, "config.toml"), []byte(`
[[hooks]]
name = "global-hook"
event = "pre_tool_use"
type = "command"
exec = "echo global"
`), 0o644)

	projectDir := t.TempDir()
	pGnomaDir := filepath.Join(projectDir, ".gnoma")
	os.MkdirAll(pGnomaDir, 0o755)
	os.WriteFile(filepath.Join(pGnomaDir, "config.toml"), []byte(`
[[hooks]]
name = "project-hook"
event = "post_tool_use"
type = "command"
exec = "echo project"
`), 0o644)

	t.Setenv("XDG_CONFIG_HOME", globalDir)
	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Hooks) != 2 {
		t.Fatalf("len(Hooks) = %d, want 2", len(cfg.Hooks))
	}
	// global hook first
	if cfg.Hooks[0].Name != "global-hook" {
		t.Errorf("Hooks[0].Name = %q, want global-hook", cfg.Hooks[0].Name)
	}
	if cfg.Hooks[1].Name != "project-hook" {
		t.Errorf("Hooks[1].Name = %q, want project-hook", cfg.Hooks[1].Name)
	}
}

func TestHookConfig_ProjectOnly(t *testing.T) {
	// No global hooks, project defines one.
	projectDir := t.TempDir()
	pGnomaDir := filepath.Join(projectDir, ".gnoma")
	os.MkdirAll(pGnomaDir, 0o755)
	os.WriteFile(filepath.Join(pGnomaDir, "config.toml"), []byte(`
[[hooks]]
name = "project-only"
event = "stop"
type = "command"
exec = "echo done"
`), 0o644)

	emptyGlobalDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", emptyGlobalDir)
	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Hooks) != 1 || cfg.Hooks[0].Name != "project-only" {
		t.Errorf("Hooks = %v, want [project-only]", cfg.Hooks)
	}
}

func TestHookConfig_GlobalOnly(t *testing.T) {
	// Global defines a hook, no project config.
	globalDir := t.TempDir()
	gnomaDir := filepath.Join(globalDir, "gnoma")
	os.MkdirAll(gnomaDir, 0o755)
	os.WriteFile(filepath.Join(gnomaDir, "config.toml"), []byte(`
[[hooks]]
name = "global-only"
event = "session_start"
type = "command"
exec = "echo start"
`), 0o644)

	projectDir := t.TempDir() // no .gnoma dir
	t.Setenv("XDG_CONFIG_HOME", globalDir)
	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Hooks) != 1 || cfg.Hooks[0].Name != "global-only" {
		t.Errorf("Hooks = %v, want [global-only]", cfg.Hooks)
	}
}

func TestLayeredLoad(t *testing.T) {
	// Set up global config
	globalDir := t.TempDir()
	gnomaDir := filepath.Join(globalDir, "gnoma")
	os.MkdirAll(gnomaDir, 0o755)
	os.WriteFile(filepath.Join(gnomaDir, "config.toml"), []byte(`
[provider]
default = "anthropic"
max_tokens = 4096
`), 0o644)

	// Set up project config that overrides
	projectDir := t.TempDir()
	pGnomaDir := filepath.Join(projectDir, ".gnoma")
	os.MkdirAll(pGnomaDir, 0o755)
	os.WriteFile(filepath.Join(pGnomaDir, "config.toml"), []byte(`
[provider]
model = "claude-haiku"
`), 0o644)

	// Override XDG_CONFIG_HOME and working directory
	t.Setenv("XDG_CONFIG_HOME", globalDir)
	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Global: default = anthropic
	if cfg.Provider.Default != "anthropic" {
		t.Errorf("Default = %q, want anthropic (from global)", cfg.Provider.Default)
	}
	// Project: model = claude-haiku
	if cfg.Provider.Model != "claude-haiku" {
		t.Errorf("Model = %q, want claude-haiku (from project)", cfg.Provider.Model)
	}
	// Global: max_tokens = 4096
	if cfg.Provider.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096 (from global)", cfg.Provider.MaxTokens)
	}
}
