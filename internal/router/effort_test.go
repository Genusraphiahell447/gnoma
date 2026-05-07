package router

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestInferEffort(t *testing.T) {
	tests := []struct {
		name        string
		taskType    TaskType
		complexity  float64
		wantAtLeast provider.EffortLevel // minimum expected effort
	}{
		{"simple boilerplate", TaskBoilerplate, 0.1, provider.EffortAuto},
		{"explain", TaskExplain, 0.2, provider.EffortAuto},
		{"medium debug", TaskDebug, 0.4, provider.EffortLow},
		{"complex refactor", TaskRefactor, 0.8, provider.EffortMedium},
		{"security review", TaskSecurityReview, 0.5, provider.EffortHigh},
		{"planning", TaskPlanning, 0.6, provider.EffortMedium},
		{"orchestration", TaskOrchestration, 0.7, provider.EffortHigh},
		{"high complexity generation", TaskGeneration, 0.9, provider.EffortMedium},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := Task{Type: tc.taskType, ComplexityScore: tc.complexity}
			got := inferEffort(task)
			if got < tc.wantAtLeast {
				t.Errorf("inferEffort(%s, complexity=%.1f) = %s, want >= %s",
					tc.taskType, tc.complexity, got, tc.wantAtLeast)
			}
		})
	}
}

func TestFilterFeasible_RequiredEffort(t *testing.T) {
	noThinking := &Arm{
		ID: NewArmID("openai", "gpt-4o"),
		Capabilities: provider.Capabilities{
			ToolUse: true,
		},
	}
	withThinking := &Arm{
		ID: NewArmID("anthropic", "claude-sonnet-4"),
		Capabilities: provider.Capabilities{
			ToolUse:       true,
			ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
		},
	}
	arms := []*Arm{noThinking, withThinking}

	t.Run("EffortAuto: both arms feasible", func(t *testing.T) {
		task := Task{Type: TaskGeneration, RequiredEffort: provider.EffortAuto}
		got := filterFeasible(arms, task)
		if len(got) != 2 {
			t.Errorf("got %d arms, want 2", len(got))
		}
	})

	t.Run("EffortHigh: only thinking arm feasible", func(t *testing.T) {
		task := Task{Type: TaskSecurityReview, RequiredEffort: provider.EffortHigh}
		got := filterFeasible(arms, task)
		if len(got) != 1 || got[0].ID != withThinking.ID {
			t.Errorf("got %v, want only %s", got, withThinking.ID)
		}
	})

	t.Run("EffortLow: only thinking arm feasible", func(t *testing.T) {
		task := Task{Type: TaskDebug, RequiredEffort: provider.EffortLow}
		got := filterFeasible(arms, task)
		if len(got) != 1 || got[0].ID != withThinking.ID {
			t.Errorf("got %v, want only %s", got, withThinking.ID)
		}
	})
}

func TestClassifyTask_SetsRequiredEffort(t *testing.T) {
	tests := []struct {
		prompt     string
		wantEffort provider.EffortLevel
	}{
		{"implement a simple hello world", provider.EffortAuto},
		{"audit this code for security vulnerabilities", provider.EffortHigh},
		{"plan the architecture for our new microservices system", provider.EffortMedium},
	}
	for _, tc := range tests {
		t.Run(tc.prompt[:20], func(t *testing.T) {
			got := ClassifyTask(tc.prompt)
			if got.RequiredEffort < tc.wantEffort {
				t.Errorf("ClassifyTask(%q).RequiredEffort = %s, want >= %s",
					tc.prompt, got.RequiredEffort, tc.wantEffort)
			}
		})
	}
}
