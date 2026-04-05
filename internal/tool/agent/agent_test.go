package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/router"
)

func TestParseTaskType_ExplicitHintTakesPrecedence(t *testing.T) {
	// Explicit hints should override prompt classification
	tests := []struct {
		hint   string
		prompt string
		want   router.TaskType
	}{
		{"review", "fix the bug", router.TaskReview},
		{"refactor", "write tests", router.TaskRefactor},
		{"debug", "plan the architecture", router.TaskDebug},
		{"explain", "implement the feature", router.TaskExplain},
		{"planning", "debug the crash", router.TaskPlanning},
		{"generation", "review the code", router.TaskGeneration},
	}
	for _, tt := range tests {
		got := parseTaskType(tt.hint, tt.prompt)
		if got != tt.want {
			t.Errorf("parseTaskType(%q, %q) = %s, want %s", tt.hint, tt.prompt, got, tt.want)
		}
	}
}

func TestParseTaskType_AutoClassifiesWhenNoHint(t *testing.T) {
	// No hint → classify from prompt instead of defaulting to TaskGeneration
	tests := []struct {
		prompt string
		want   router.TaskType
	}{
		{"review this pull request", router.TaskReview},
		{"fix the failing test", router.TaskDebug},
		{"refactor the auth module", router.TaskRefactor},
		{"write unit tests for handler", router.TaskUnitTest},
		{"explain how the router works", router.TaskExplain},
		{"audit security of the API", router.TaskSecurityReview},
		{"plan the migration strategy", router.TaskPlanning},
		{"scaffold a new service", router.TaskBoilerplate},
	}
	for _, tt := range tests {
		got := parseTaskType("", tt.prompt)
		if got != tt.want {
			t.Errorf("parseTaskType(%q) = %s, want %s (auto-classified)", tt.prompt, got, tt.want)
		}
	}
}

// --- Tool interface tests ---

func TestAgentTool_Interface(t *testing.T) {
	tool := New(nil, nil)

	if tool.Name() != "agent" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "agent")
	}
	if tool.Description() == "" {
		t.Error("Description() must be non-empty")
	}
	if !json.Valid(tool.Parameters()) {
		t.Error("Parameters() must be valid JSON")
	}
	if !tool.IsReadOnly() {
		t.Error("IsReadOnly() must be true — agent spawning is non-destructive to parent context")
	}
	if tool.IsDestructive() {
		t.Error("IsDestructive() must be false")
	}
}

func TestBatchTool_Interface(t *testing.T) {
	tool := NewBatch(nil, nil)

	if tool.Name() != "spawn_elfs" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "spawn_elfs")
	}
	if tool.Description() == "" {
		t.Error("Description() must be non-empty")
	}
	if !json.Valid(tool.Parameters()) {
		t.Error("Parameters() must be valid JSON")
	}
	if !tool.IsReadOnly() {
		t.Error("IsReadOnly() must be true")
	}
	if tool.IsDestructive() {
		t.Error("IsDestructive() must be false")
	}
}

func TestListResultsTool_Interface(t *testing.T) {
	tool := NewListResultsTool(nil)

	if tool.Name() != "list_results" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "list_results")
	}
	if !json.Valid(tool.Parameters()) {
		t.Error("Parameters() must be valid JSON")
	}
	if !tool.IsReadOnly() {
		t.Error("IsReadOnly() must be true")
	}
}

func TestReadResultTool_Interface(t *testing.T) {
	tool := NewReadResultTool(nil)

	if tool.Name() != "read_result" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "read_result")
	}
	if !json.Valid(tool.Parameters()) {
		t.Error("Parameters() must be valid JSON")
	}
	if !tool.IsReadOnly() {
		t.Error("IsReadOnly() must be true")
	}
}

// --- Truncation tests ---

func TestTruncateOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		max       int
		truncated bool   // true = expect truncation; false = expect unchanged
		want      string // only checked when !truncated
	}{
		{name: "short text unchanged", input: "hello", max: 2000, want: "hello"},
		{name: "empty string unchanged", input: "", max: 2000, want: ""},
		{name: "exact max unchanged", input: strings.Repeat("x", 2000), max: 2000, want: strings.Repeat("x", 2000)},
		{name: "over max is truncated", input: strings.Repeat("y", 5000), max: 2000, truncated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateOutput(tt.input, tt.max)
			if !tt.truncated {
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
				return
			}
			// Truncated: result must start with first max chars and include notice.
			if !strings.HasPrefix(got, strings.Repeat("y", tt.max)) {
				t.Error("truncated result should start with first max chars of input")
			}
			if !strings.Contains(got, "[truncated") {
				t.Error("truncated result must contain '[truncated' notice")
			}
		})
	}
}
