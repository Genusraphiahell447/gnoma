package plugin

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// writePlugin creates a plugin directory with a plugin.json manifest.
func writePlugin(t *testing.T, dir, name, version string, caps *Capabilities) {
	t.Helper()
	pluginDir := filepath.Join(dir, name)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := Manifest{Name: name, Version: version}
	if caps != nil {
		m.Capabilities = *caps
	}

	data, _ := marshalManifest(m)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// writePluginWithSkill creates a plugin with a skill file.
func writePluginWithSkill(t *testing.T, dir, pluginName, skillName, skillContent string) {
	t.Helper()
	pluginDir := filepath.Join(dir, pluginName)
	skillsDir := filepath.Join(pluginDir, "skills")
	os.MkdirAll(skillsDir, 0o755)

	m := Manifest{
		Name:    pluginName,
		Version: "1.0.0",
		Capabilities: Capabilities{
			Skills: []string{"skills/*.md"},
		},
	}
	data, _ := marshalManifest(m)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)
	os.WriteFile(filepath.Join(skillsDir, skillName+".md"), []byte(skillContent), 0o644)
}

func marshalManifest(m Manifest) ([]byte, error) {
	return marshalJSON(m)
}

func TestLoader_Discover_Empty(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(testLogger())

	plugins, err := loader.Discover(filepath.Join(dir, "global"), filepath.Join(dir, "project"))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(plugins))
	}
}

func TestLoader_Discover_GlobalPlugin(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	writePlugin(t, globalDir, "git-tools", "1.0.0", nil)

	loader := NewLoader(testLogger())
	plugins, err := loader.Discover(globalDir, filepath.Join(dir, "project"))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Manifest.Name != "git-tools" {
		t.Errorf("Name = %q, want %q", plugins[0].Manifest.Name, "git-tools")
	}
	if plugins[0].Scope != "user" {
		t.Errorf("Scope = %q, want %q", plugins[0].Scope, "user")
	}
}

func TestLoader_Discover_ProjectOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	projectDir := filepath.Join(dir, "project")

	writePlugin(t, globalDir, "shared", "1.0.0", nil)
	writePlugin(t, projectDir, "shared", "2.0.0", nil)

	loader := NewLoader(testLogger())
	plugins, err := loader.Discover(globalDir, projectDir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin (deduplicated), got %d", len(plugins))
	}
	if plugins[0].Manifest.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q (project should override global)", plugins[0].Manifest.Version, "2.0.0")
	}
	if plugins[0].Scope != "project" {
		t.Errorf("Scope = %q, want %q", plugins[0].Scope, "project")
	}
}

func TestLoader_Discover_SkipsInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")

	// Write a valid plugin.
	writePlugin(t, globalDir, "good", "1.0.0", nil)

	// Write an invalid plugin (bad JSON).
	badDir := filepath.Join(globalDir, "bad")
	os.MkdirAll(badDir, 0o755)
	os.WriteFile(filepath.Join(badDir, "plugin.json"), []byte(`{invalid`), 0o644)

	loader := NewLoader(testLogger())
	plugins, err := loader.Discover(globalDir, filepath.Join(dir, "project"))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin (skipping invalid), got %d", len(plugins))
	}
	if plugins[0].Manifest.Name != "good" {
		t.Errorf("Name = %q, want %q", plugins[0].Manifest.Name, "good")
	}
}

func TestLoader_Load_AllEnabled(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	writePluginWithSkill(t, globalDir, "test-plugin", "my-skill", "---\nname: my-skill\n---\nHello")

	loader := NewLoader(testLogger())
	plugins, _ := loader.Discover(globalDir, filepath.Join(dir, "project"))

	enabledSet := map[string]bool{"test-plugin": true}
	result, err := loader.Load(plugins, enabledSet)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(result.Skills) != 1 {
		t.Fatalf("expected 1 skill source, got %d", len(result.Skills))
	}
	if result.Skills[0].Source != "plugin:test-plugin" {
		t.Errorf("Source = %q, want %q", result.Skills[0].Source, "plugin:test-plugin")
	}
}

func TestLoader_Load_DisabledPlugin(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	writePluginWithSkill(t, globalDir, "disabled-plugin", "skill", "---\nname: skill\n---\nHi")

	loader := NewLoader(testLogger())
	plugins, _ := loader.Discover(globalDir, filepath.Join(dir, "project"))

	// Plugin not in enabled set.
	result, err := loader.Load(plugins, map[string]bool{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(result.Skills) != 0 {
		t.Errorf("expected 0 skills for disabled plugin, got %d", len(result.Skills))
	}
}

func TestLoader_Load_HooksConverted(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")

	caps := &Capabilities{
		Hooks: []HookSpec{
			{
				Name:        "check",
				Event:       "pre_tool_use",
				Type:        "command",
				Exec:        "scripts/check.sh",
				ToolPattern: "bash*",
			},
		},
	}
	writePlugin(t, globalDir, "hook-plugin", "1.0.0", caps)

	loader := NewLoader(testLogger())
	plugins, _ := loader.Discover(globalDir, filepath.Join(dir, "project"))

	result, err := loader.Load(plugins, map[string]bool{"hook-plugin": true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(result.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(result.Hooks))
	}
	h := result.Hooks[0]
	if h.Name != "check" {
		t.Errorf("Hook name = %q", h.Name)
	}
	if h.Event != "pre_tool_use" {
		t.Errorf("Hook event = %q", h.Event)
	}
	// Exec should be resolved to absolute path under plugin dir.
	pluginDir := filepath.Join(globalDir, "hook-plugin")
	wantExec := filepath.Join(pluginDir, "scripts/check.sh")
	if h.Exec != wantExec {
		t.Errorf("Hook exec = %q, want %q", h.Exec, wantExec)
	}
}

func TestLoader_Load_MCPServersConverted(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")

	caps := &Capabilities{
		MCPServers: []MCPServerSpec{
			{
				Name:    "git",
				Command: "bin/mcp-git",
				Args:    []string{"--verbose"},
			},
		},
	}
	writePlugin(t, globalDir, "mcp-plugin", "1.0.0", caps)

	loader := NewLoader(testLogger())
	plugins, _ := loader.Discover(globalDir, filepath.Join(dir, "project"))

	result, err := loader.Load(plugins, map[string]bool{"mcp-plugin": true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(result.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(result.MCPServers))
	}
	s := result.MCPServers[0]
	if s.Name != "git" {
		t.Errorf("Name = %q", s.Name)
	}
	// Command should be absolute path.
	pluginDir := filepath.Join(globalDir, "mcp-plugin")
	wantCmd := filepath.Join(pluginDir, "bin/mcp-git")
	if s.Command != wantCmd {
		t.Errorf("Command = %q, want %q", s.Command, wantCmd)
	}
}
