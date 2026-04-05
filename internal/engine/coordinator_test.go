package engine

import (
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/router"
)

func TestCoordinatorSystemPrompt_InjectedForOrchestration(t *testing.T) {
	prompt := coordinatorPrompt()
	if !strings.Contains(prompt, "spawn_elfs") {
		t.Error("coordinator prompt must mention spawn_elfs")
	}
	if !strings.Contains(prompt, "list_results") {
		t.Error("coordinator prompt must mention list_results")
	}
}

func TestShouldInjectCoordinatorPrompt(t *testing.T) {
	cases := []struct {
		prompt string
		want   bool
	}{
		{"orchestrate the migration", true},
		{"coordinate the refactor", true},
		{"dispatch tasks to elfs", true},
		{"fix the bug in main.go", false},
		{"explain this function", false},
		{"write unit tests for auth", false},
	}
	for _, c := range cases {
		task := router.ClassifyTask(c.prompt)
		got := task.Type == router.TaskOrchestration
		if got != c.want {
			t.Errorf("prompt %q: want orchestration=%v, got %v (type=%s)", c.prompt, c.want, got, task.Type)
		}
	}
}
