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
