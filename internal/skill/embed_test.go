package skill

import (
	"testing"
)

func TestBundledSkills_NotEmpty(t *testing.T) {
	skills, err := BundledSkills()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) == 0 {
		t.Error("expected at least one bundled skill")
	}
}

func TestBundledSkills_BatchExists(t *testing.T) {
	skills, err := BundledSkills()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var batch *Skill
	for _, s := range skills {
		if s.Frontmatter.Name == "batch" {
			batch = s
			break
		}
	}
	if batch == nil {
		t.Fatal("batch skill not found in bundled skills")
	}
	if batch.Frontmatter.Description == "" {
		t.Error("batch skill missing description")
	}
	if batch.Body == "" {
		t.Error("batch skill has empty body")
	}
	if batch.Source != "bundled" {
		t.Errorf("batch skill source = %q, want %q", batch.Source, "bundled")
	}
}

func TestBundledSkills_AllParseClean(t *testing.T) {
	skills, err := BundledSkills()
	if err != nil {
		t.Fatalf("BundledSkills() error: %v", err)
	}
	for _, s := range skills {
		if s.Frontmatter.Name == "" {
			t.Errorf("bundled skill has empty name: %+v", s)
		}
	}
}

func TestBundledSkills_InitExists(t *testing.T) {
	skills, err := BundledSkills()
	if err != nil {
		t.Fatalf("BundledSkills() error: %v", err)
	}
	var init *Skill
	for _, s := range skills {
		if s.Frontmatter.Name == "init" {
			init = s
			break
		}
	}
	if init == nil {
		t.Fatal("init skill not found in bundled skills")
	}
	if init.Frontmatter.Description == "" {
		t.Error("init skill missing description")
	}
	if init.Body == "" {
		t.Error("init skill has empty body")
	}
}

func TestBundledSkills_InitRender_Local(t *testing.T) {
	skills, err := BundledSkills()
	if err != nil {
		t.Fatalf("BundledSkills() error: %v", err)
	}
	var init *Skill
	for _, s := range skills {
		if s.Frontmatter.Name == "init" {
			init = s
			break
		}
	}
	if init == nil {
		t.Fatal("init skill not found")
	}

	rendered, err := init.Render(TemplateData{
		ProjectRoot: "/tmp/myproject",
		Local:       true,
	})
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	// Local mode should use sequential fs_* tools, not spawn_elfs for orchestration
	if !contains(rendered, "fs_ls") {
		t.Error("local render should contain fs_ls")
	}
	if !contains(rendered, "fs_read") {
		t.Error("local render should contain fs_read")
	}
	if contains(rendered, "Use spawn_elfs") {
		t.Error("local render should NOT instruct to use spawn_elfs")
	}
	if !contains(rendered, "/tmp/myproject") {
		t.Error("local render should contain ProjectRoot")
	}
}

func TestBundledSkills_InitRender_Cloud(t *testing.T) {
	skills, err := BundledSkills()
	if err != nil {
		t.Fatalf("BundledSkills() error: %v", err)
	}
	var init *Skill
	for _, s := range skills {
		if s.Frontmatter.Name == "init" {
			init = s
			break
		}
	}
	if init == nil {
		t.Fatal("init skill not found")
	}

	rendered, err := init.Render(TemplateData{
		ProjectRoot: "/tmp/myproject",
		Local:       false,
	})
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	// Cloud mode should use spawn_elfs
	if !contains(rendered, "spawn_elfs") {
		t.Error("cloud render should contain spawn_elfs")
	}
	if !contains(rendered, "Elf 1") {
		t.Error("cloud render should contain Elf 1")
	}
	if !contains(rendered, "/tmp/myproject") {
		t.Error("cloud render should contain ProjectRoot")
	}
	if !contains(rendered, "creating") {
		t.Error("cloud render (no Args) should say 'creating'")
	}
}

func TestBundledSkills_InitRender_CloudUpdate(t *testing.T) {
	skills, err := BundledSkills()
	if err != nil {
		t.Fatalf("BundledSkills() error: %v", err)
	}
	var init *Skill
	for _, s := range skills {
		if s.Frontmatter.Name == "init" {
			init = s
			break
		}
	}
	if init == nil {
		t.Fatal("init skill not found")
	}

	rendered, err := init.Render(TemplateData{
		ProjectRoot: "/tmp/myproject",
		Args:        "/tmp/myproject/AGENTS.md",
		Local:       false,
	})
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	// Cloud update mode should have Elf 4 for review
	if !contains(rendered, "Elf 4") {
		t.Error("cloud update render should contain Elf 4")
	}
	if !contains(rendered, "updating") {
		t.Error("cloud update render should say 'updating'")
	}
	if !contains(rendered, "/tmp/myproject/AGENTS.md") {
		t.Error("cloud update render should contain existing path")
	}
}

func TestBundledSkills_InitRender_LocalUpdate(t *testing.T) {
	skills, err := BundledSkills()
	if err != nil {
		t.Fatalf("BundledSkills() error: %v", err)
	}
	var init *Skill
	for _, s := range skills {
		if s.Frontmatter.Name == "init" {
			init = s
			break
		}
	}
	if init == nil {
		t.Fatal("init skill not found")
	}

	rendered, err := init.Render(TemplateData{
		ProjectRoot: "/tmp/myproject",
		Args:        "/tmp/myproject/AGENTS.md",
		Local:       true,
	})
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	// Local update should mention existing file
	if !contains(rendered, "existing AGENTS.md") {
		t.Error("local update render should mention existing file")
	}
	if contains(rendered, "Use spawn_elfs") {
		t.Error("local update render should NOT instruct to use spawn_elfs")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
