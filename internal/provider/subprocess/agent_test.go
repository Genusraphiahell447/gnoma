package subprocess

import (
	"testing"
)

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
	for _, f := range []StreamFormat{FormatClaudeStreamJSON, FormatGeminiStreamJSON, FormatVibeStreaming} {
		p := newParser(f)
		if p == nil {
			t.Errorf("newParser(%q) returned nil", f)
		}
	}
}
