package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkillFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writing skill file: %v", err)
	}
}

func TestRegistry_LoadDir_SingleFile(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "myskill.md", "---\nname: myskill\ndescription: test skill\n---\ndo the thing\n")

	reg := NewRegistry()
	if err := reg.LoadDir(dir, "user"); err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}

	sk := reg.Get("myskill")
	if sk == nil {
		t.Fatal("skill not found after LoadDir")
	}
	if sk.Source != "user" {
		t.Errorf("source = %q, want %q", sk.Source, "user")
	}
}

func TestRegistry_LoadDir_MissingDir_NoError(t *testing.T) {
	reg := NewRegistry()
	err := reg.LoadDir("/nonexistent/path/that/does/not/exist", "user")
	if err != nil {
		t.Errorf("LoadDir on missing dir should not error, got: %v", err)
	}
}

func TestRegistry_LoadDir_SkipsNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "skill.md", "---\nname: good\n---\nbody\n")
	writeSkillFile(t, dir, "README.txt", "not a skill")
	writeSkillFile(t, dir, "config.toml", "[section]\nkey = 1")

	reg := NewRegistry()
	if err := reg.LoadDir(dir, "user"); err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(reg.Names()) != 1 {
		t.Errorf("expected 1 skill, got %d: %v", len(reg.Names()), reg.Names())
	}
}

func TestRegistry_OverridePrecedence(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeSkillFile(t, dir1, "shared.md", "---\nname: shared\ndescription: from dir1\n---\nbody1\n")
	writeSkillFile(t, dir2, "shared.md", "---\nname: shared\ndescription: from dir2\n---\nbody2\n")

	reg := NewRegistry()
	_ = reg.LoadDir(dir1, "user")
	_ = reg.LoadDir(dir2, "project")

	sk := reg.Get("shared")
	if sk == nil {
		t.Fatal("skill not found")
	}
	if sk.Frontmatter.Description != "from dir2" {
		t.Errorf("later load should override: got description %q", sk.Frontmatter.Description)
	}
	if sk.Source != "project" {
		t.Errorf("source = %q, want project", sk.Source)
	}
}

func TestRegistry_GetUnknown_ReturnsNil(t *testing.T) {
	reg := NewRegistry()
	if reg.Get("nonexistent") != nil {
		t.Error("expected nil for unknown skill")
	}
}

func TestRegistry_Names_Sorted(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "zebra.md", "---\nname: zebra\n---\nbody\n")
	writeSkillFile(t, dir, "alpha.md", "---\nname: alpha\n---\nbody\n")
	writeSkillFile(t, dir, "middle.md", "---\nname: middle\n---\nbody\n")

	reg := NewRegistry()
	_ = reg.LoadDir(dir, "test")

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "middle" || names[2] != "zebra" {
		t.Errorf("names not sorted: %v", names)
	}
}

func TestRegistry_LoadBundled(t *testing.T) {
	reg := NewRegistry()
	if err := reg.LoadBundled(); err != nil {
		t.Fatalf("LoadBundled error: %v", err)
	}
	if reg.Get("batch") == nil {
		t.Error("batch skill not found after LoadBundled")
	}
}

func TestRegistry_All_ReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "a.md", "---\nname: aaa\n---\nbody\n")

	reg := NewRegistry()
	_ = reg.LoadDir(dir, "test")

	all := reg.All()
	if len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}
}
