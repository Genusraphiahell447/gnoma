package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_Install(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	projectDir := filepath.Join(dir, "project")
	_ = os.MkdirAll(globalDir, 0o755)
	_ = os.MkdirAll(projectDir, 0o755)

	// Create a source plugin directory.
	srcDir := filepath.Join(dir, "src", "my-plugin")
	_ = os.MkdirAll(srcDir, 0o755)
	m := Manifest{Name: "my-plugin", Version: "1.0.0", Description: "Test plugin"}
	data, _ := marshalJSON(m)
	_ = os.WriteFile(filepath.Join(srcDir, "plugin.json"), data, 0o644)

	mgr := NewManager(globalDir, projectDir, testLogger())

	// Install to user scope.
	if err := mgr.Install(srcDir, "user"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify the plugin was copied.
	installed := filepath.Join(globalDir, "my-plugin", "plugin.json")
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("installed manifest not found: %v", err)
	}
}

func TestManager_Install_ProjectScope(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	projectDir := filepath.Join(dir, "project")
	_ = os.MkdirAll(globalDir, 0o755)
	_ = os.MkdirAll(projectDir, 0o755)

	srcDir := filepath.Join(dir, "src", "proj-plugin")
	_ = os.MkdirAll(srcDir, 0o755)
	m := Manifest{Name: "proj-plugin", Version: "1.0.0"}
	data, _ := marshalJSON(m)
	_ = os.WriteFile(filepath.Join(srcDir, "plugin.json"), data, 0o644)

	mgr := NewManager(globalDir, projectDir, testLogger())

	if err := mgr.Install(srcDir, "project"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed := filepath.Join(projectDir, "proj-plugin", "plugin.json")
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("installed manifest not found: %v", err)
	}
}

func TestManager_Install_AlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	_ = os.MkdirAll(globalDir, 0o755)

	srcDir := filepath.Join(dir, "src", "dup")
	_ = os.MkdirAll(srcDir, 0o755)
	m := Manifest{Name: "dup", Version: "1.0.0"}
	data, _ := marshalJSON(m)
	_ = os.WriteFile(filepath.Join(srcDir, "plugin.json"), data, 0o644)

	mgr := NewManager(globalDir, filepath.Join(dir, "project"), testLogger())

	// First install.
	_ = mgr.Install(srcDir, "user")

	// Second install should fail.
	err := mgr.Install(srcDir, "user")
	if err == nil {
		t.Error("expected ErrAlreadyInstalled")
	}
}

func TestManager_Install_NoManifest(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	_ = os.MkdirAll(globalDir, 0o755)

	srcDir := filepath.Join(dir, "src", "empty")
	_ = os.MkdirAll(srcDir, 0o755)

	mgr := NewManager(globalDir, filepath.Join(dir, "project"), testLogger())
	err := mgr.Install(srcDir, "user")
	if err == nil {
		t.Error("expected error for missing manifest")
	}
}

func TestManager_Uninstall(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	_ = os.MkdirAll(globalDir, 0o755)

	// Pre-install a plugin.
	pluginDir := filepath.Join(globalDir, "to-remove")
	_ = os.MkdirAll(pluginDir, 0o755)
	m := Manifest{Name: "to-remove", Version: "1.0.0"}
	data, _ := marshalJSON(m)
	_ = os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)

	mgr := NewManager(globalDir, filepath.Join(dir, "project"), testLogger())

	if err := mgr.Uninstall("to-remove", "user"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Error("plugin directory should be removed")
	}
}

func TestManager_Uninstall_NotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "global"), filepath.Join(dir, "project"), testLogger())

	err := mgr.Uninstall("nonexistent", "user")
	if err == nil {
		t.Error("expected ErrNotFound")
	}
}

func TestManager_List(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	projectDir := filepath.Join(dir, "project")

	writePlugin(t, globalDir, "global-plugin", "1.0.0", nil)
	writePlugin(t, projectDir, "project-plugin", "2.0.0", nil)

	mgr := NewManager(globalDir, projectDir, testLogger())

	infos, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(infos) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(infos))
	}

	// Check that both scopes are represented.
	scopes := map[string]bool{}
	for _, info := range infos {
		scopes[info.Scope] = true
	}
	if !scopes["user"] || !scopes["project"] {
		t.Errorf("expected both scopes, got %v", scopes)
	}
}
