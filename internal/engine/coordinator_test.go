package engine

import (
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/router"
)

func TestCoordinatorPrompt_RequiredToolsMentioned(t *testing.T) {
	prompt := coordinatorPrompt()
	for _, required := range []string{"spawn_elfs", "list_results", "read_result"} {
		if !strings.Contains(prompt, required) {
			t.Errorf("coordinator prompt must mention %q", required)
		}
	}
}

func TestCoordinatorPrompt_GuidanceContent(t *testing.T) {
	prompt := strings.ToLower(coordinatorPrompt())
	// Prompt must instruct parallel dispatch, serial writes, and synthesis.
	for _, required := range []string{"parallel", "serial", "synthesize"} {
		if !strings.Contains(prompt, required) {
			t.Errorf("coordinator prompt must contain guidance keyword %q", required)
		}
	}
}

func TestShouldInjectCoordinatorPrompt(t *testing.T) {
	cases := []struct {
		prompt string
		want   bool
		note   string
	}{
		// True positives: explicit orchestration intent
		{"orchestrate the migration", true, "explicit orchestrat keyword"},
		{"coordinate the deployment", true, "explicit coordinate keyword"},
		{"dispatch tasks to elfs", true, "explicit dispatch keyword"},
		{"fan out this work to 5 elfs", true, "fan out keyword"},
		{"split this into subtasks", true, "subtask keyword"},
		{"delegate to worker elfs", true, "delegate to keyword"},
		// False positives: operational task types must gate first
		{"fix the bug in main.go", false, "debug gates before orchestration"},
		{"explain this function", false, "explain gates before orchestration"},
		{"write unit tests for auth", false, "test gates before orchestration"},
		{"review the orchestration layer", false, "review gates even with orchestrat substring"},
		{"refactor the pipeline dispatch", false, "refactor gates even with dispatch substring"},
		{"debug the dispatch table", false, "debug gates even with dispatch substring"},
	}
	for _, c := range cases {
		task := router.ClassifyTask(c.prompt)
		got := task.Type == router.TaskOrchestration
		if got != c.want {
			t.Errorf("prompt %q (%s): want orchestration=%v, got %v (type=%s)",
				c.prompt, c.note, c.want, got, task.Type)
		}
	}
}
