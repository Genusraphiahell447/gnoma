package router

import (
	"fmt"
	"strings"
)

// TaskType classifies a task for routing purposes.
type TaskType int

const (
	TaskBoilerplate    TaskType = iota // simple scaffolding, templates
	TaskGeneration                     // new code creation
	TaskRefactor                       // restructuring existing code
	TaskReview                         // code review, analysis
	TaskUnitTest                       // writing tests
	TaskPlanning                       // architecture, design
	TaskOrchestration                  // multi-step coordination
	TaskSecurityReview                 // security-focused analysis
	TaskDebug                          // finding and fixing bugs
	TaskExplain                        // explaining code or concepts
)

func (t TaskType) String() string {
	switch t {
	case TaskBoilerplate:
		return "boilerplate"
	case TaskGeneration:
		return "generation"
	case TaskRefactor:
		return "refactor"
	case TaskReview:
		return "review"
	case TaskUnitTest:
		return "unit_test"
	case TaskPlanning:
		return "planning"
	case TaskOrchestration:
		return "orchestration"
	case TaskSecurityReview:
		return "security_review"
	case TaskDebug:
		return "debug"
	case TaskExplain:
		return "explain"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// Priority indicates task importance for routing decisions.
type Priority int

const (
	PriorityLow      Priority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// Task represents a classified unit of work for routing.
type Task struct {
	Type            TaskType
	Priority        Priority
	EstimatedTokens int
	RequiresTools   bool
	ComplexityScore float64 // 0-1
}

// ValueScore computes a routing value based on priority and type.
func (t Task) ValueScore() float64 {
	base := map[Priority]float64{
		PriorityLow:      0.5,
		PriorityNormal:   1.0,
		PriorityHigh:     2.0,
		PriorityCritical: 5.0,
	}[t.Priority]

	return base * taskTypeMultiplier[t.Type]
}

var taskTypeMultiplier = map[TaskType]float64{
	TaskBoilerplate:    0.6,
	TaskGeneration:     1.0,
	TaskRefactor:       0.9,
	TaskReview:         1.1,
	TaskUnitTest:       0.8,
	TaskPlanning:       1.4,
	TaskOrchestration:  1.5,
	TaskSecurityReview: 2.0,
	TaskDebug:          1.2,
	TaskExplain:        0.7,
}

// QualityThreshold defines minimum acceptable quality for a task type.
type QualityThreshold struct {
	Minimum    float64 // below → output is harmful, never accept
	Acceptable float64 // good enough
	Target     float64 // ideal
}

// DefaultThresholds are calibrated for M4 heuristic scores (range ~0–0.85).
// M9 will replace these with bandit-derived values once quality data accumulates.
var DefaultThresholds = map[TaskType]QualityThreshold{
	TaskBoilerplate:    {0.40, 0.55, 0.70}, // any capable arm works
	TaskGeneration:     {0.45, 0.60, 0.75},
	TaskRefactor:       {0.50, 0.65, 0.78},
	TaskReview:         {0.55, 0.68, 0.80},
	TaskUnitTest:       {0.45, 0.60, 0.75},
	TaskPlanning:       {0.60, 0.72, 0.82},
	TaskOrchestration:  {0.65, 0.75, 0.83},
	TaskSecurityReview: {0.70, 0.78, 0.84}, // requires thinking or large context window
	TaskDebug:          {0.50, 0.65, 0.78},
	TaskExplain:        {0.40, 0.55, 0.72},
}

// ClassifyTask infers a TaskType from the user's prompt using keyword heuristics.
func ClassifyTask(prompt string) Task {
	lower := strings.ToLower(prompt)

	task := Task{
		Priority:      PriorityNormal,
		RequiresTools: true, // assume tools needed by default
	}

	// Check for task type keywords (order matters — more specific first)
	switch {
	case containsAny(lower, "security", "vulnerability", "cve", "owasp", "xss", "injection", "audit security"):
		task.Type = TaskSecurityReview
		task.Priority = PriorityHigh
	case containsAny(lower, "plan", "architect", "design", "strategy", "roadmap"):
		task.Type = TaskPlanning
	case containsAny(lower, "orchestrat", "coordinate", "dispatch", "pipeline"):
		task.Type = TaskOrchestration
		task.Priority = PriorityHigh
	case containsAny(lower, "debug", "fix", "troubleshoot", "not working", "error", "crash", "failing", "bug"):
		task.Type = TaskDebug
	case containsAny(lower, "review", "check", "analyze", "audit", "inspect"):
		task.Type = TaskReview
	case containsAny(lower, "refactor", "restructure", "reorganize", "clean up", "simplify"):
		task.Type = TaskRefactor
	case containsAny(lower, "test", "spec", "coverage", "assert"):
		task.Type = TaskUnitTest
	case containsAny(lower, "explain", "what is", "how does", "describe", "tell me about"):
		task.Type = TaskExplain
		task.RequiresTools = false
	case containsAny(lower, "create", "implement", "build", "add", "write", "generate", "make"):
		task.Type = TaskGeneration
	case containsAny(lower, "scaffold", "boilerplate", "template", "stub", "skeleton"):
		task.Type = TaskBoilerplate
	default:
		task.Type = TaskGeneration // default
	}

	// Estimate complexity from prompt length and keywords
	task.ComplexityScore = estimateComplexity(lower)

	return task
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func estimateComplexity(prompt string) float64 {
	score := 0.0

	// Length contributes to complexity
	words := len(strings.Fields(prompt))
	score += float64(words) / 200.0 // normalize: 200 words = 1.0

	// Complexity keywords
	complexKeywords := []string{"implement", "design", "architect", "system", "integration", "migrate", "optimize"}
	for _, kw := range complexKeywords {
		if strings.Contains(prompt, kw) {
			score += 0.15
		}
	}

	// Simple keywords reduce complexity
	simpleKeywords := []string{"rename", "format", "add field", "change name", "typo", "simple"}
	for _, kw := range simpleKeywords {
		if strings.Contains(prompt, kw) {
			score -= 0.15
		}
	}

	// Clamp to [0, 1]
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}
