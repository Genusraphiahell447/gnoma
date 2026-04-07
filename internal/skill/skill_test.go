package skill

import (
	"testing"
)

func TestParse_FullFrontmatter(t *testing.T) {
	data := []byte(`---
name: my-skill
description: Does something useful
whenToUse: When you need to do something
allowedTools:
  - bash
  - fs.read
paths:
  - ./internal
---
This is the skill body.
`)
	s, err := Parse(data, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Frontmatter.Name != "my-skill" {
		t.Errorf("name = %q, want %q", s.Frontmatter.Name, "my-skill")
	}
	if s.Frontmatter.Description != "Does something useful" {
		t.Errorf("description = %q", s.Frontmatter.Description)
	}
	if len(s.Frontmatter.AllowedTools) != 2 {
		t.Errorf("allowedTools len = %d, want 2", len(s.Frontmatter.AllowedTools))
	}
	if len(s.Frontmatter.Paths) != 1 {
		t.Errorf("paths len = %d, want 1", len(s.Frontmatter.Paths))
	}
	if s.Source != "test" {
		t.Errorf("source = %q, want %q", s.Source, "test")
	}
	want := "This is the skill body.\n"
	if s.Body != want {
		t.Errorf("body = %q, want %q", s.Body, want)
	}
}

func TestParse_MinimalFrontmatter(t *testing.T) {
	data := []byte("---\nname: simple\n---\ndo the thing\n")
	s, err := Parse(data, "bundled")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Frontmatter.Name != "simple" {
		t.Errorf("name = %q", s.Frontmatter.Name)
	}
	if s.Body != "do the thing\n" {
		t.Errorf("body = %q", s.Body)
	}
}

func TestParse_EmptyBody(t *testing.T) {
	data := []byte("---\nname: empty-body\n---\n")
	s, err := Parse(data, "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Body != "" {
		t.Errorf("body = %q, want empty", s.Body)
	}
}

func TestParse_MissingName(t *testing.T) {
	data := []byte("---\ndescription: no name here\n---\nbody\n")
	_, err := Parse(data, "test")
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestParse_NoFrontmatter(t *testing.T) {
	data := []byte("Just a plain markdown file with no frontmatter.\n")
	_, err := Parse(data, "test")
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	data := []byte("---\nname: [unclosed bracket\n---\nbody\n")
	_, err := Parse(data, "test")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParse_InvalidName_Uppercase(t *testing.T) {
	data := []byte("---\nname: MySkill\n---\nbody\n")
	_, err := Parse(data, "test")
	if err == nil {
		t.Error("expected error for uppercase name")
	}
}

func TestParse_InvalidName_Spaces(t *testing.T) {
	data := []byte("---\nname: my skill\n---\nbody\n")
	_, err := Parse(data, "test")
	if err == nil {
		t.Error("expected error for name with spaces")
	}
}

func TestParse_InvalidName_Slash(t *testing.T) {
	data := []byte("---\nname: my/skill\n---\nbody\n")
	_, err := Parse(data, "test")
	if err == nil {
		t.Error("expected error for name with slash")
	}
}

func TestParse_DashesInMarkdownBody(t *testing.T) {
	// --- appearing inside the body must NOT be treated as a second delimiter
	data := []byte("---\nname: with-dashes\n---\nBody text.\n\n---\n\nMore text after a horizontal rule.\n")
	s, err := Parse(data, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Frontmatter.Name != "with-dashes" {
		t.Errorf("name = %q", s.Frontmatter.Name)
	}
	// Body should include the horizontal rule and text after it
	if s.Body == "" {
		t.Error("body should not be empty")
	}
}
