package bash

import (
	"context"
	"testing"
)

func TestParseAliases_BashFormat(t *testing.T) {
	output := `alias gs='git status'
alias ll='ls -la --color=auto'
alias gco='git checkout'
alias ..='cd ..'
`
	m, err := ParseAliases(output)
	if err != nil {
		t.Fatalf("ParseAliases: %v", err)
	}

	if m.Len() != 4 {
		t.Errorf("Len() = %d, want 4", m.Len())
	}

	tests := []struct {
		name, want string
	}{
		{"gs", "git status"},
		{"ll", "ls -la --color=auto"},
		{"gco", "git checkout"},
		{"..", "cd .."},
	}
	for _, tt := range tests {
		got, ok := m.Get(tt.name)
		if !ok {
			t.Errorf("alias %q not found", tt.name)
			continue
		}
		if got != tt.want {
			t.Errorf("alias %q = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestParseAliases_ZshFormat(t *testing.T) {
	// zsh alias -p may omit 'alias ' prefix
	output := `gs='git status'
ll='ls -la'
`
	m, err := ParseAliases(output)
	if err != nil {
		t.Fatalf("ParseAliases: %v", err)
	}

	got, ok := m.Get("gs")
	if !ok || got != "git status" {
		t.Errorf("gs = %q, %v", got, ok)
	}
}

func TestParseAliases_DoubleQuotes(t *testing.T) {
	output := `alias gs="git status"
`
	m, _ := ParseAliases(output)

	got, ok := m.Get("gs")
	if !ok || got != "git status" {
		t.Errorf("gs = %q, %v", got, ok)
	}
}

func TestParseAliases_SkipsDangerousExpansions(t *testing.T) {
	output := `alias safe='ls -la'
alias danger='echo $(whoami)'
alias backtick='echo ` + "`" + `date` + "`" + `'
alias ifshack='IFS=: read a b'
`
	m, _ := ParseAliases(output)

	if _, ok := m.Get("safe"); !ok {
		t.Error("safe alias should be kept")
	}
	if _, ok := m.Get("danger"); ok {
		t.Error("danger alias ($()) should be filtered")
	}
	if _, ok := m.Get("backtick"); ok {
		t.Error("backtick alias should be filtered")
	}
	if _, ok := m.Get("ifshack"); ok {
		t.Error("IFS alias should be filtered")
	}
}

func TestParseAliases_EmptyAndMalformed(t *testing.T) {
	output := `
alias gs='git status'

not a valid line
alias =empty_name
alias noequals
`
	m, _ := ParseAliases(output)

	if m.Len() != 1 {
		t.Errorf("Len() = %d, want 1 (only gs)", m.Len())
	}
}

func TestAliasMap_ExpandCommand(t *testing.T) {
	m := NewAliasMap()
	m.mu.Lock()
	m.aliases["ll"] = "ls -la --color=auto"
	m.aliases["gs"] = "git status"
	m.aliases[".."] = "cd .."
	m.mu.Unlock()

	tests := []struct {
		input string
		want  string
	}{
		// Alias with args
		{"ll /tmp", "ls -la --color=auto /tmp"},
		// Alias without args
		{"gs", "git status"},
		// Alias with trailing whitespace (trimmed)
		{"gs ", "git status"},
		// No alias match — return unchanged
		{"echo hello", "echo hello"},
		// Dotdot alias
		{"..", "cd .."},
		// Empty command
		{"", ""},
		// Only whitespace
		{"  ", "  "},
	}
	for _, tt := range tests {
		got := m.ExpandCommand(tt.input)
		if got != tt.want {
			t.Errorf("ExpandCommand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAliasMap_ExpandCommand_NoAliases(t *testing.T) {
	m := NewAliasMap()
	got := m.ExpandCommand("echo hello")
	if got != "echo hello" {
		t.Errorf("ExpandCommand = %q, want unchanged", got)
	}
}

func TestAliasMap_All(t *testing.T) {
	m := NewAliasMap()
	m.mu.Lock()
	m.aliases["a"] = "b"
	m.aliases["c"] = "d"
	m.mu.Unlock()

	all := m.All()
	if len(all) != 2 {
		t.Errorf("len(All()) = %d, want 2", len(all))
	}
	// Verify it's a copy
	all["x"] = "y"
	if m.Len() != 2 {
		t.Error("All() should return a copy, not a reference")
	}
}

func TestStripQuotes(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"'hello'", "hello"},
		{`"hello"`, "hello"},
		{"hello", "hello"},
		{"'h'", "h"},
		{"''", ""},
		{`""`, ""},
		{"'mismatched\"", "'mismatched\""},
		{"x", "x"},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripQuotes(tt.input)
		if got != tt.want {
			t.Errorf("stripQuotes(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseFishAliases(t *testing.T) {
	output := `alias gs 'git status'
alias ll 'ls -la'
alias gco "git checkout"
`
	m, err := ParseFishAliases(output)
	if err != nil {
		t.Fatalf("ParseFishAliases: %v", err)
	}

	if m.Len() != 3 {
		t.Errorf("Len() = %d, want 3", m.Len())
	}

	got, ok := m.Get("gs")
	if !ok || got != "git status" {
		t.Errorf("gs = %q, %v", got, ok)
	}
	got, ok = m.Get("gco")
	if !ok || got != "git checkout" {
		t.Errorf("gco = %q, %v", got, ok)
	}
}

func TestShellBaseName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"/bin/bash", "bash"},
		{"/usr/bin/zsh", "zsh"},
		{"/usr/local/bin/fish", "fish"},
		{"bash", "bash"},
		{"/bin/sh", "sh"},
	}
	for _, tt := range tests {
		got := shellBaseName(tt.input)
		if got != tt.want {
			t.Errorf("shellBaseName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAliasCommandFor(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{"bash", "alias -p 2>/dev/null; true"},
		{"zsh", "alias 2>/dev/null; true"},
		{"fish", "alias 2>/dev/null; true"},
		{"sh", "alias -p 2>/dev/null; true"},
		{"unknown", "alias 2>/dev/null; true"},
	}
	for _, tt := range tests {
		got := aliasCommandFor(tt.shell)
		if got != tt.want {
			t.Errorf("aliasCommandFor(%q) = %q, want %q", tt.shell, got, tt.want)
		}
	}
}

func TestHarvestAliases_Integration(t *testing.T) {
	// This actually runs the user's shell — skip in CI
	if testing.Short() {
		t.Skip("skipping alias harvest in short mode")
	}

	m, err := HarvestAliases(context.Background())
	if err != nil {
		// Non-fatal: harvesting may fail in some environments
		t.Logf("HarvestAliases: %v (non-fatal)", err)
	}
	t.Logf("Harvested %d aliases", m.Len())
	for name, exp := range m.All() {
		t.Logf("  %s → %s", name, exp)
	}
}

func TestBashTool_WithAliases(t *testing.T) {
	aliases := NewAliasMap()
	aliases.mu.Lock()
	aliases.aliases["ll"] = "ls -la"
	aliases.mu.Unlock()

	b := New(WithAliases(aliases))

	// "ll /tmp" should expand to "ls -la /tmp" and execute
	result, err := b.Execute(context.Background(), []byte(`{"command":"ll /tmp"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should produce output (ls -la /tmp lists files)
	if result.Output == "" {
		t.Error("expected output from expanded alias")
	}
	if result.Metadata["blocked"] == true {
		t.Error("expanded alias should not be blocked")
	}
}
