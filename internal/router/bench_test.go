package router

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

// benchArms creates a set of arms with diverse cost/capability profiles.
func benchArms() []*Arm {
	return []*Arm{
		{
			ID: "anthropic/claude-sonnet", ModelName: "claude-sonnet",
			Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 200000, Thinking: false},
			CostPer1kInput:  0.003, CostPer1kOutput: 0.015,
		},
		{
			ID: "anthropic/claude-opus", ModelName: "claude-opus",
			Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 200000, Thinking: true},
			CostPer1kInput:  0.015, CostPer1kOutput: 0.075,
		},
		{
			ID: "openai/gpt-4o", ModelName: "gpt-4o",
			Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 128000},
			CostPer1kInput:  0.005, CostPer1kOutput: 0.015,
		},
		{
			ID: "ollama/qwen3:8b", ModelName: "qwen3:8b",
			IsLocal:         true,
			Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 32000},
			CostPer1kInput:  0, CostPer1kOutput: 0,
		},
		{
			ID: "mistral/mistral-large", ModelName: "mistral-large",
			Capabilities:    provider.Capabilities{ToolUse: true, ContextWindow: 128000},
			CostPer1kInput:  0.002, CostPer1kOutput: 0.006,
		},
	}
}

// benchTasks returns one task per TaskType at varying complexity.
func benchTasks() []Task {
	return []Task{
		{Type: TaskBoilerplate, Priority: PriorityLow, EstimatedTokens: 500, RequiresTools: true, ComplexityScore: 0.1},
		{Type: TaskGeneration, Priority: PriorityNormal, EstimatedTokens: 2000, RequiresTools: true, ComplexityScore: 0.5},
		{Type: TaskRefactor, Priority: PriorityNormal, EstimatedTokens: 3000, RequiresTools: true, ComplexityScore: 0.6},
		{Type: TaskReview, Priority: PriorityHigh, EstimatedTokens: 4000, RequiresTools: false, ComplexityScore: 0.5},
		{Type: TaskUnitTest, Priority: PriorityNormal, EstimatedTokens: 1500, RequiresTools: true, ComplexityScore: 0.4},
		{Type: TaskPlanning, Priority: PriorityHigh, EstimatedTokens: 5000, RequiresTools: false, ComplexityScore: 0.8},
		{Type: TaskOrchestration, Priority: PriorityCritical, EstimatedTokens: 8000, RequiresTools: true, ComplexityScore: 0.9},
		{Type: TaskSecurityReview, Priority: PriorityCritical, EstimatedTokens: 6000, RequiresTools: true, ComplexityScore: 0.85},
		{Type: TaskDebug, Priority: PriorityNormal, EstimatedTokens: 3000, RequiresTools: true, ComplexityScore: 0.6},
		{Type: TaskExplain, Priority: PriorityLow, EstimatedTokens: 1000, RequiresTools: false, ComplexityScore: 0.2},
	}
}

func BenchmarkSelectBest(b *testing.B) {
	arms := benchArms()
	tasks := benchTasks()
	qt := NewQualityTracker()

	b.ResetTimer()
	for b.Loop() {
		for _, task := range tasks {
			selectBest(qt, arms, task)
		}
	}
}

func BenchmarkFilterFeasible(b *testing.B) {
	arms := benchArms()
	tasks := benchTasks()

	b.ResetTimer()
	for b.Loop() {
		for _, task := range tasks {
			filterFeasible(arms, task)
		}
	}
}

func BenchmarkRouterSelect(b *testing.B) {
	r := New(Config{})
	for _, arm := range benchArms() {
		r.RegisterArm(arm)
	}
	tasks := benchTasks()

	b.ResetTimer()
	for b.Loop() {
		for _, task := range tasks {
			d := r.Select(task)
			if d.Error == nil {
				d.Commit(task.EstimatedTokens)
			}
		}
	}
}

func BenchmarkScoreArm(b *testing.B) {
	arms := benchArms()
	qt := NewQualityTracker()
	task := Task{Type: TaskGeneration, Priority: PriorityNormal, EstimatedTokens: 2000, RequiresTools: true, ComplexityScore: 0.5}

	b.ResetTimer()
	for b.Loop() {
		for _, arm := range arms {
			scoreArm(qt, arm, task)
		}
	}
}

func BenchmarkClassifyTask(b *testing.B) {
	prompts := []string{
		"fix the null pointer in handleRequest",
		"explain how the router selects arms",
		"refactor the authentication middleware to use the new session store",
		"add a new endpoint for user profile updates",
		"review the security of the payment processing flow for OWASP vulnerabilities",
		"write unit tests for the pool tracker",
		"plan the architecture for the plugin system",
		"scaffold a new provider adapter for Cohere",
		"orchestrate a multi-step migration: backup, schema change, data backfill, verify",
		"debug why the TUI freezes when streaming large responses",
	}

	b.ResetTimer()
	for b.Loop() {
		for _, p := range prompts {
			ClassifyTask(p)
		}
	}
}

func BenchmarkRouterSelectWithQuality(b *testing.B) {
	r := New(Config{})
	for _, arm := range benchArms() {
		r.RegisterArm(arm)
	}
	tasks := benchTasks()

	// Seed quality tracker with 20 observations per arm/task combo
	for _, arm := range benchArms() {
		for _, task := range tasks {
			for range 20 {
				r.quality.Record(arm.ID, task.Type, true)
			}
			// Mix in some failures for realism
			for range 3 {
				r.quality.Record(arm.ID, task.Type, false)
			}
		}
	}

	b.ResetTimer()
	for b.Loop() {
		for _, task := range tasks {
			d := r.Select(task)
			if d.Error == nil {
				d.Commit(task.EstimatedTokens)
			}
		}
	}
}
