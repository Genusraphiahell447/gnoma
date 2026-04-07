package skill

import (
	"strings"
	"testing"
)

func skillWithBody(body string) *Skill {
	return &Skill{
		Frontmatter: Frontmatter{Name: "test"},
		Body:        body,
	}
}

func TestRender_ArgsSubstituted(t *testing.T) {
	s := skillWithBody("Please do: {{.Args}}")
	out, err := s.Render(TemplateData{Args: "fix the tests"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "Please do: fix the tests" {
		t.Errorf("output = %q", out)
	}
}

func TestRender_AllVariables(t *testing.T) {
	s := skillWithBody("Cwd={{.Cwd}} Root={{.ProjectRoot}} Args={{.Args}}")
	out, err := s.Render(TemplateData{Args: "a", Cwd: "/tmp", ProjectRoot: "/proj"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "Cwd=/tmp Root=/proj Args=a" {
		t.Errorf("output = %q", out)
	}
}

func TestRender_NoDirectives_ArgsAppended(t *testing.T) {
	s := skillWithBody("Refactor all the things.\n")
	out, err := s.Render(TemplateData{Args: "focus on error handling"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Refactor all the things.") {
		t.Errorf("body missing from output: %q", out)
	}
	if !strings.Contains(out, "focus on error handling") {
		t.Errorf("args missing from output: %q", out)
	}
	// args must follow body with blank line separator
	if !strings.Contains(out, "Refactor all the things.\n\n\nfocus on error handling") &&
		!strings.Contains(out, "Refactor all the things.\n\nfocus on error handling") {
		t.Errorf("unexpected separator in output: %q", out)
	}
}

func TestRender_NoDirectives_NoArgs(t *testing.T) {
	body := "Just the body.\n"
	s := skillWithBody(body)
	out, err := s.Render(TemplateData{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != body {
		t.Errorf("output = %q, want %q", out, body)
	}
}

func TestRender_InvalidTemplate(t *testing.T) {
	s := skillWithBody("{{.Unclosed")
	_, err := s.Render(TemplateData{})
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}

func TestRender_EmptyBody_WithArgs(t *testing.T) {
	s := skillWithBody("")
	out, err := s.Render(TemplateData{Args: "do something"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty body + args → just the args
	if out != "do something" {
		t.Errorf("output = %q, want %q", out, "do something")
	}
}
