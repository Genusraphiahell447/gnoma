package subprocess

import (
	"context"
	"errors"
	"slices"
	"sort"
	"testing"
)

// withMockLookPath swaps the package-level lookPath for the duration of a
// test. The caller must not call t.Parallel() on tests that use this — the
// global is shared across the package.
func withMockLookPath(t *testing.T, fn func(name string) (string, error)) {
	t.Helper()
	prev := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = prev })
}

func TestKnownAgents_Defined(t *testing.T) {
	if len(knownAgents) == 0 {
		t.Fatal("knownAgents must not be empty")
	}
	for _, a := range knownAgents {
		if a.Name == "" {
			t.Errorf("agent with empty Name: %+v", a)
		}
		if a.DisplayName == "" {
			t.Errorf("agent %q has empty DisplayName", a.Name)
		}
		if a.Format == "" {
			t.Errorf("agent %q has empty Format", a.Name)
		}
		if a.PromptArgs == nil {
			t.Errorf("agent %q has nil PromptArgs", a.Name)
		}
	}
}

func TestKnownAgents_UniqueNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, a := range knownAgents {
		if seen[a.Name] {
			t.Errorf("duplicate agent name %q", a.Name)
		}
		seen[a.Name] = true
	}
}

func TestKnownAgents_ValidFormats(t *testing.T) {
	valid := map[StreamFormat]bool{
		FormatClaudeStreamJSON: true,
		FormatGeminiStreamJSON: true,
		FormatVibeStreaming:    true,
		FormatCodexStreamJSON:  true,
	}
	for _, a := range knownAgents {
		if !valid[a.Format] {
			t.Errorf("agent %q has unknown format %q", a.Name, a.Format)
		}
	}
}

func TestKnownAgents_PromptArgsIncludePrompt(t *testing.T) {
	const testPrompt = "TESTPROMPT_UNIQUE_SENTINEL"
	for _, a := range knownAgents {
		args := a.PromptArgs(testPrompt)
		found := false
		for _, arg := range args {
			if arg == testPrompt {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("agent %q PromptArgs(%q) does not include the prompt in args: %v", a.Name, testPrompt, args)
		}
	}
}

func TestNewParser_ReturnsParserForKnownFormats(t *testing.T) {
	formats := []StreamFormat{
		FormatClaudeStreamJSON,
		FormatGeminiStreamJSON,
		FormatVibeStreaming,
		FormatCodexStreamJSON,
	}
	for _, f := range formats {
		p := newParser(f, nil)
		if p == nil {
			t.Errorf("newParser(%q) returned nil", f)
		}
	}
}

func TestResolveAgentBinary(t *testing.T) {
	cases := []struct {
		name        string
		canonical   string
		override    string
		found       map[string]string // binary name → path returned by lookPath
		wantPath    string
		wantBinName string
		wantErr     bool
	}{
		{
			name:        "no override: canonical resolves",
			canonical:   "claude",
			override:    "",
			found:       map[string]string{"claude": "/usr/bin/claude"},
			wantPath:    "/usr/bin/claude",
			wantBinName: "claude",
		},
		{
			name:        "override: resolved binary path returned",
			canonical:   "claude",
			override:    "claude-priv",
			found:       map[string]string{"claude": "/usr/bin/claude", "claude-priv": "/home/user/bin/claude-priv"},
			wantPath:    "/home/user/bin/claude-priv",
			wantBinName: "claude-priv",
		},
		{
			name:        "empty override falls back to canonical",
			canonical:   "claude",
			override:    "",
			found:       map[string]string{"claude": "/usr/bin/claude"},
			wantPath:    "/usr/bin/claude",
			wantBinName: "claude",
		},
		{
			name:      "override set but binary not on PATH: error (no silent fallback)",
			canonical: "claude",
			override:  "claude-typo",
			found:     map[string]string{"claude": "/usr/bin/claude"}, // canonical present, override missing
			wantErr:   true,
		},
		{
			name:      "no override and canonical missing: error",
			canonical: "claude",
			override:  "",
			found:     map[string]string{},
			wantErr:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withMockLookPath(t, func(name string) (string, error) {
				if p, ok := tc.found[name]; ok {
					return p, nil
				}
				return "", errors.New("not found")
			})

			path, binName, err := resolveAgentBinary(tc.canonical, tc.override)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path=%q binName=%q", path, binName)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if path != tc.wantPath {
				t.Errorf("path = %q, want %q", path, tc.wantPath)
			}
			if binName != tc.wantBinName {
				t.Errorf("binName = %q, want %q", binName, tc.wantBinName)
			}
		})
	}
}

// stubLookPath returns a lookPath function that resolves any name in the
// given set to a synthetic "/mock/bin/<name>" path. Names absent from the
// set return an error.
func stubLookPath(present ...string) func(string) (string, error) {
	set := make(map[string]bool, len(present))
	for _, n := range present {
		set[n] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/mock/bin/" + name, nil
		}
		return "", errors.New("not found: " + name)
	}
}

func TestDiscoverCLIAgents_NoOverrides_UsesCanonical(t *testing.T) {
	withMockLookPath(t, stubLookPath("claude", "gemini", "vibe"))

	agents := DiscoverCLIAgents(context.Background(), nil)

	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, a.Name)
		if a.OverrideBinary != "" {
			t.Errorf("agent %q has OverrideBinary=%q, want empty", a.Name, a.OverrideBinary)
		}
		if a.Path != "/mock/bin/"+a.Name {
			t.Errorf("agent %q path = %q, want /mock/bin/%s", a.Name, a.Path, a.Name)
		}
	}
	sort.Strings(names)
	want := []string{"claude", "gemini", "vibe"}
	if !slices.Equal(names, want) {
		t.Errorf("agents = %v, want %v", names, want)
	}
}

func TestDiscoverCLIAgents_WithOverride_ResolvesAlias(t *testing.T) {
	// claude → claude-priv (overridden), gemini canonical present, vibe absent.
	withMockLookPath(t, stubLookPath("claude-priv", "gemini"))

	overrides := map[string]string{"claude": "claude-priv"}
	agents := DiscoverCLIAgents(context.Background(), overrides)

	byName := map[string]DiscoveredAgent{}
	for _, a := range agents {
		byName[a.Name] = a
	}

	claude, ok := byName["claude"]
	if !ok {
		t.Fatal("claude agent missing")
	}
	if claude.OverrideBinary != "claude-priv" {
		t.Errorf("claude.OverrideBinary = %q, want claude-priv", claude.OverrideBinary)
	}
	if claude.Path != "/mock/bin/claude-priv" {
		t.Errorf("claude.Path = %q, want /mock/bin/claude-priv", claude.Path)
	}

	gemini, ok := byName["gemini"]
	if !ok {
		t.Fatal("gemini agent missing")
	}
	if gemini.OverrideBinary != "" {
		t.Errorf("gemini.OverrideBinary = %q, want empty", gemini.OverrideBinary)
	}

	if _, present := byName["vibe"]; present {
		t.Error("vibe should not be discovered (not on PATH)")
	}
}

func TestDiscoverCLIAgents_EmptyOverrideValue_FallsBackToCanonical(t *testing.T) {
	withMockLookPath(t, stubLookPath("claude"))

	overrides := map[string]string{"claude": ""}
	agents := DiscoverCLIAgents(context.Background(), overrides)

	if len(agents) == 0 {
		t.Fatal("expected at least the canonical claude")
	}
	for _, a := range agents {
		if a.Name == "claude" {
			if a.OverrideBinary != "" {
				t.Errorf("empty override should yield no OverrideBinary; got %q", a.OverrideBinary)
			}
			if a.Path != "/mock/bin/claude" {
				t.Errorf("Path = %q, want /mock/bin/claude", a.Path)
			}
			return
		}
	}
	t.Fatal("claude agent not in discovered set")
}

func TestDiscoverCLIAgents_OverrideMissingFromPATH_SkipsAgent(t *testing.T) {
	// claude override set to a nonexistent binary; canonical claude IS on PATH.
	// The agent must be skipped (no silent fallback).
	withMockLookPath(t, stubLookPath("claude", "gemini"))

	overrides := map[string]string{"claude": "claude-typo"}
	agents := DiscoverCLIAgents(context.Background(), overrides)

	for _, a := range agents {
		if a.Name == "claude" {
			t.Errorf("claude should be skipped when override binary missing; got %+v", a)
		}
	}
	// gemini should still be discovered.
	foundGemini := false
	for _, a := range agents {
		if a.Name == "gemini" {
			foundGemini = true
		}
	}
	if !foundGemini {
		t.Error("gemini should still be discovered when claude is skipped")
	}
}
