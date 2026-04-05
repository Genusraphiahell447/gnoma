package agent

import (
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
